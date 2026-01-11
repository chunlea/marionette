package scrcpy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chunlea/marionette/pkg/streaming/android"
)

// StreamReader reads and demuxes scrcpy video/audio streams.
// scrcpy uses a custom protocol with device metadata header followed by
// video/audio packets.
type StreamReader struct {
	reader io.Reader
	config *streamConfig

	// Video state
	videoParser *H264Parser
	videoConfig *android.VideoConfig

	// Audio state (if enabled)
	audioConfig *android.AudioConfig

	// Callbacks
	sink android.VideoSink

	// Statistics
	stats     *android.StreamStats
	statsMu   sync.RWMutex
	startTime time.Time

	// Control
	closed  int32
	closeCh chan struct{}
}

// streamConfig holds parsed stream configuration.
type streamConfig struct {
	deviceName string
	width      int
	height     int
	codec      string
}

// NewStreamReader creates a new stream reader.
func NewStreamReader(reader io.Reader, sink android.VideoSink) *StreamReader {
	return &StreamReader{
		reader:      reader,
		videoParser: NewH264Parser(),
		sink:        sink,
		stats: &android.StreamStats{
			UpdatedAt: time.Now(),
		},
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
	}
}

// Start begins reading and parsing the stream.
// This blocks until the stream ends or Close is called.
func (s *StreamReader) Start() error {
	// Read device metadata header
	if err := s.readHeader(); err != nil {
		return fmt.Errorf("failed to read stream header: %w", err)
	}

	// Main read loop
	return s.readLoop()
}

// Close stops the stream reader.
func (s *StreamReader) Close() error {
	if atomic.CompareAndSwapInt32(&s.closed, 0, 1) {
		close(s.closeCh)
	}
	return nil
}

// VideoConfig returns the video configuration, or nil if not yet available.
func (s *StreamReader) VideoConfig() *android.VideoConfig {
	return s.videoConfig
}

// AudioConfig returns the audio configuration, or nil if audio is disabled.
func (s *StreamReader) AudioConfig() *android.AudioConfig {
	return s.audioConfig
}

// Stats returns current streaming statistics.
func (s *StreamReader) Stats() *android.StreamStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()

	// Calculate average FPS
	elapsed := time.Since(s.startTime).Seconds()
	if elapsed > 0 {
		s.stats.AverageFPS = float64(s.stats.FramesSent) / elapsed
	}

	statsCopy := *s.stats
	return &statsCopy
}

// readHeader reads and parses the scrcpy metadata header.
// Format (scrcpy 2.0+):
// - Device name: 64 bytes (null-padded)
// - Codec: 4 bytes (ASCII)
// - Initial width: 4 bytes (big endian)
// - Initial height: 4 bytes (big endian)
func (s *StreamReader) readHeader() error {
	// Device name (64 bytes)
	deviceName := make([]byte, 64)
	if _, err := io.ReadFull(s.reader, deviceName); err != nil {
		return fmt.Errorf("failed to read device name: %w", err)
	}

	// Find null terminator
	nullIdx := bytes.IndexByte(deviceName, 0)
	if nullIdx > 0 {
		deviceName = deviceName[:nullIdx]
	}

	// Codec (4 bytes) - e.g., "h264", "h265"
	codecBytes := make([]byte, 4)
	if _, err := io.ReadFull(s.reader, codecBytes); err != nil {
		return fmt.Errorf("failed to read codec: %w", err)
	}
	codec := string(bytes.TrimRight(codecBytes, "\x00"))

	// Width (4 bytes, big endian)
	var width uint32
	if err := binary.Read(s.reader, binary.BigEndian, &width); err != nil {
		return fmt.Errorf("failed to read width: %w", err)
	}

	// Height (4 bytes, big endian)
	var height uint32
	if err := binary.Read(s.reader, binary.BigEndian, &height); err != nil {
		return fmt.Errorf("failed to read height: %w", err)
	}

	s.config = &streamConfig{
		deviceName: string(deviceName),
		width:      int(width),
		height:     int(height),
		codec:      codec,
	}

	s.videoConfig = &android.VideoConfig{
		Width:  int(width),
		Height: int(height),
		Codec:  codec,
	}

	return nil
}

// readLoop continuously reads packets from the stream.
func (s *StreamReader) readLoop() error {
	// Buffer for reading packets
	// scrcpy sends raw H.264 NAL units with Annex B start codes
	buf := make([]byte, 64*1024) // 64KB read buffer
	configSent := false

	for {
		select {
		case <-s.closeCh:
			return nil
		default:
		}

		n, err := s.reader.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if s.sink != nil {
					s.sink.OnClose()
				}
				return nil
			}
			if s.sink != nil {
				s.sink.OnError(err)
			}
			return err
		}

		if n == 0 {
			continue
		}

		// Parse H.264 NAL units
		s.videoParser.Write(buf[:n])
		units, err := s.videoParser.Parse()
		if err != nil {
			if s.sink != nil {
				s.sink.OnError(err)
			}
			continue
		}

		for _, unit := range units {
			// Check for SPS/PPS to send config
			if !configSent && s.videoParser.HasConfig() {
				if s.sink != nil {
					// Create AVC config record
					config, err := CreateAVCDecoderConfigurationRecord(
						s.videoParser.SPS(),
						s.videoParser.PPS(),
					)
					if err == nil {
						// Also try to extract dimensions from SPS
						w, h, err := ParseSPSDimensions(s.videoParser.SPS())
						if err == nil && w > 0 && h > 0 {
							s.videoConfig.Width = w
							s.videoConfig.Height = h
						}
						s.videoConfig.SPS = s.videoParser.SPS()
						s.videoConfig.PPS = s.videoParser.PPS()

						if err := s.sink.OnVideoConfig(
							s.videoConfig.Width,
							s.videoConfig.Height,
							s.videoConfig.Codec,
							config,
						); err != nil {
							// Log error but continue
						}
					}
				}
				configSent = true
			}

			// Send video data
			if s.sink != nil {
				// Convert to Annex B format for transport
				annexB := NALUnitToAnnexB(unit.Data)
				if err := s.sink.OnVideoData(annexB); err != nil {
					// Log error but continue
				}
			}

			// Update stats
			s.statsMu.Lock()
			s.stats.FramesSent++
			s.stats.BytesSent += int64(len(unit.Data))
			s.stats.UpdatedAt = time.Now()
			s.statsMu.Unlock()
		}
	}
}

// PacketType identifies the type of scrcpy packet (for v2.0+ with audio).
type PacketType uint8

const (
	// PacketTypeVideo is a video packet.
	PacketTypeVideo PacketType = 0
	// PacketTypeAudio is an audio packet.
	PacketTypeAudio PacketType = 1
	// PacketTypeConfig is a configuration packet.
	PacketTypeConfig PacketType = 2
)

// StreamReaderV2 reads scrcpy v2.0+ streams with video and audio support.
// The v2.0 protocol adds a packet type byte before each data chunk.
type StreamReaderV2 struct {
	*StreamReader
	audioEnabled bool
}

// NewStreamReaderV2 creates a new stream reader for scrcpy v2.0+.
func NewStreamReaderV2(reader io.Reader, sink android.VideoSink, audioEnabled bool) *StreamReaderV2 {
	return &StreamReaderV2{
		StreamReader: NewStreamReader(reader, sink),
		audioEnabled: audioEnabled,
	}
}

// Start begins reading and parsing the v2.0+ stream.
func (s *StreamReaderV2) Start() error {
	// Read device metadata header (same as v1)
	if err := s.readHeader(); err != nil {
		return fmt.Errorf("failed to read stream header: %w", err)
	}

	// If audio is enabled, read audio config
	if s.audioEnabled {
		if err := s.readAudioHeader(); err != nil {
			return fmt.Errorf("failed to read audio header: %w", err)
		}
	}

	// Main read loop with packet type handling
	return s.readLoopV2()
}

// readAudioHeader reads audio configuration for v2.0+ streams.
// Format:
// - Audio codec: 4 bytes (ASCII)
// - Sample rate: 4 bytes (big endian)
// - Channels: 1 byte
func (s *StreamReaderV2) readAudioHeader() error {
	// Audio codec (4 bytes)
	codecBytes := make([]byte, 4)
	if _, err := io.ReadFull(s.reader, codecBytes); err != nil {
		return fmt.Errorf("failed to read audio codec: %w", err)
	}
	codec := string(bytes.TrimRight(codecBytes, "\x00"))

	// Sample rate (4 bytes, big endian)
	var sampleRate uint32
	if err := binary.Read(s.reader, binary.BigEndian, &sampleRate); err != nil {
		return fmt.Errorf("failed to read sample rate: %w", err)
	}

	// Channels (1 byte)
	var channels uint8
	if err := binary.Read(s.reader, binary.BigEndian, &channels); err != nil {
		return fmt.Errorf("failed to read channels: %w", err)
	}

	s.audioConfig = &android.AudioConfig{
		SampleRate: int(sampleRate),
		Channels:   int(channels),
		Codec:      codec,
	}

	// Notify sink of audio config
	if s.sink != nil {
		if err := s.sink.OnAudioConfig(
			s.audioConfig.SampleRate,
			s.audioConfig.Channels,
			s.audioConfig.Codec,
			nil, // No extra config needed for opus/aac
		); err != nil {
			// Log error but continue
		}
	}

	return nil
}

// readLoopV2 reads packets with type prefix (v2.0+ format).
func (s *StreamReaderV2) readLoopV2() error {
	configSent := false

	for {
		select {
		case <-s.closeCh:
			return nil
		default:
		}

		// Read packet header: type (1 byte) + length (4 bytes)
		header := make([]byte, 5)
		if _, err := io.ReadFull(s.reader, header); err != nil {
			if errors.Is(err, io.EOF) {
				if s.sink != nil {
					s.sink.OnClose()
				}
				return nil
			}
			if s.sink != nil {
				s.sink.OnError(err)
			}
			return err
		}

		packetType := PacketType(header[0])
		packetLen := binary.BigEndian.Uint32(header[1:5])

		// Read packet data
		data := make([]byte, packetLen)
		if _, err := io.ReadFull(s.reader, data); err != nil {
			if s.sink != nil {
				s.sink.OnError(err)
			}
			return err
		}

		switch packetType {
		case PacketTypeVideo:
			// Parse and forward video data
			s.videoParser.Write(data)
			units, err := s.videoParser.Parse()
			if err != nil {
				continue
			}

			for _, unit := range units {
				// Check for SPS/PPS to send config
				if !configSent && s.videoParser.HasConfig() {
					if s.sink != nil {
						config, err := CreateAVCDecoderConfigurationRecord(
							s.videoParser.SPS(),
							s.videoParser.PPS(),
						)
						if err == nil {
							w, h, err := ParseSPSDimensions(s.videoParser.SPS())
							if err == nil && w > 0 && h > 0 {
								s.videoConfig.Width = w
								s.videoConfig.Height = h
							}
							s.videoConfig.SPS = s.videoParser.SPS()
							s.videoConfig.PPS = s.videoParser.PPS()

							_ = s.sink.OnVideoConfig(
								s.videoConfig.Width,
								s.videoConfig.Height,
								s.videoConfig.Codec,
								config,
							)
						}
					}
					configSent = true
				}

				// Send video data
				if s.sink != nil {
					annexB := NALUnitToAnnexB(unit.Data)
					_ = s.sink.OnVideoData(annexB)
				}

				// Update stats
				s.statsMu.Lock()
				s.stats.FramesSent++
				s.stats.BytesSent += int64(len(unit.Data))
				s.stats.UpdatedAt = time.Now()
				s.statsMu.Unlock()
			}

		case PacketTypeAudio:
			// Forward audio data
			if s.sink != nil {
				_ = s.sink.OnAudioData(data)
			}

		case PacketTypeConfig:
			// Handle config updates if needed
		}
	}
}

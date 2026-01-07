package webrtc

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming/android"
	"github.com/chunlea/marionette/pkg/streaming/android/scrcpy"
)

// Relay implements android.VideoSink and forwards video/audio data to WebRTC peers.
// It handles H.264 to RTP packetization and manages multiple peer connections.
type Relay struct {
	config Config
	logger *zap.Logger

	// Video state
	videoCodec  string
	videoWidth  int
	videoHeight int
	videoConfig []byte
	h264Parser  *scrcpy.H264Parser

	// Audio state
	audioCodec      string
	audioSampleRate int
	audioChannels   int
	audioConfig     []byte

	// Connected peers
	peers   map[string]*Peer
	peersMu sync.RWMutex

	// RTP state
	videoSeq   uint16
	videoTS    uint32
	audioSeq   uint16
	audioTS    uint32
	rtpMu      sync.Mutex
	lastVideoT time.Time
	lastAudioT time.Time

	// Statistics
	videoFrames   uint64
	videoBytes    uint64
	audioFrames   uint64
	audioBytes    uint64
	droppedFrames uint64

	// Lifecycle
	closed  int32
	closeCh chan struct{}
}

// NewRelay creates a new WebRTC relay.
func NewRelay(config Config) *Relay {
	config = config.WithDefaults()

	return &Relay{
		config:     config,
		logger:     config.Logger.Named("relay"),
		h264Parser: scrcpy.NewH264Parser(),
		peers:      make(map[string]*Peer),
		closeCh:    make(chan struct{}),
		lastVideoT: time.Now(),
		lastAudioT: time.Now(),
	}
}

// AddPeer adds a peer to receive video/audio data.
func (r *Relay) AddPeer(peer *Peer) error {
	if atomic.LoadInt32(&r.closed) == 1 {
		return errors.New("relay is closed")
	}

	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	if _, exists := r.peers[peer.ID()]; exists {
		return errors.New("peer already added")
	}

	// Add video track
	if r.videoCodec != "" {
		_, err := peer.AddVideoTrack(r.videoCodec)
		if err != nil {
			return err
		}
	}

	// Add audio track
	if r.audioCodec != "" {
		_, err := peer.AddAudioTrack(r.audioCodec)
		if err != nil {
			return err
		}
	}

	r.peers[peer.ID()] = peer

	r.logger.Info("peer added",
		zap.String("peer_id", peer.ID()),
		zap.Int("total_peers", len(r.peers)),
	)

	return nil
}

// RemovePeer removes a peer from the relay.
func (r *Relay) RemovePeer(peerID string) {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	if peer, exists := r.peers[peerID]; exists {
		delete(r.peers, peerID)
		_ = peer.Close()

		r.logger.Info("peer removed",
			zap.String("peer_id", peerID),
			zap.Int("total_peers", len(r.peers)),
		)
	}
}

// GetPeer returns a peer by ID.
func (r *Relay) GetPeer(peerID string) *Peer {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()
	return r.peers[peerID]
}

// PeerCount returns the number of connected peers.
func (r *Relay) PeerCount() int {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()
	return len(r.peers)
}

// Stats returns relay statistics.
func (r *Relay) Stats() RelayStats {
	return RelayStats{
		VideoFrames:   atomic.LoadUint64(&r.videoFrames),
		VideoBytes:    atomic.LoadUint64(&r.videoBytes),
		AudioFrames:   atomic.LoadUint64(&r.audioFrames),
		AudioBytes:    atomic.LoadUint64(&r.audioBytes),
		DroppedFrames: atomic.LoadUint64(&r.droppedFrames),
		PeerCount:     r.PeerCount(),
	}
}

// RelayStats contains relay statistics.
type RelayStats struct {
	VideoFrames   uint64
	VideoBytes    uint64
	AudioFrames   uint64
	AudioBytes    uint64
	DroppedFrames uint64
	PeerCount     int
}

// VideoConfig returns the current video configuration.
func (r *Relay) VideoConfig() (width, height int, codec string, config []byte) {
	return r.videoWidth, r.videoHeight, r.videoCodec, r.videoConfig
}

// AudioConfig returns the current audio configuration.
func (r *Relay) AudioConfig() (sampleRate, channels int, codec string, config []byte) {
	return r.audioSampleRate, r.audioChannels, r.audioCodec, r.audioConfig
}

// Close closes the relay and all peer connections.
func (r *Relay) Close() error {
	if !atomic.CompareAndSwapInt32(&r.closed, 0, 1) {
		return nil
	}

	close(r.closeCh)

	r.peersMu.Lock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		peers = append(peers, peer)
	}
	r.peers = make(map[string]*Peer)
	r.peersMu.Unlock()

	for _, peer := range peers {
		_ = peer.Close()
	}

	r.logger.Info("relay closed")
	return nil
}

// Done returns a channel that's closed when the relay is closed.
func (r *Relay) Done() <-chan struct{} {
	return r.closeCh
}

// OnVideoData implements android.VideoSink.
// It receives raw H.264 NAL units in Annex B format.
func (r *Relay) OnVideoData(data []byte) error {
	if atomic.LoadInt32(&r.closed) == 1 {
		return errors.New("relay is closed")
	}

	if len(data) == 0 {
		return nil
	}

	// Parse NAL units
	r.h264Parser.Write(data)
	units, err := r.h264Parser.Parse()
	if err != nil {
		r.logger.Warn("failed to parse H.264 data", zap.Error(err))
		return nil
	}

	// Send each NAL unit as RTP packets
	for _, unit := range units {
		if err := r.sendVideoNAL(unit.Data); err != nil {
			atomic.AddUint64(&r.droppedFrames, 1)
		}
	}

	atomic.AddUint64(&r.videoFrames, 1)
	atomic.AddUint64(&r.videoBytes, uint64(len(data)))

	return nil
}

// OnVideoConfig implements android.VideoSink.
func (r *Relay) OnVideoConfig(width, height int, codec string, config []byte) error {
	r.videoWidth = width
	r.videoHeight = height
	r.videoCodec = codec
	r.videoConfig = config

	r.logger.Info("video config received",
		zap.Int("width", width),
		zap.Int("height", height),
		zap.String("codec", codec),
	)

	// Add video tracks to existing peers that don't have them
	r.peersMu.Lock()
	for _, peer := range r.peers {
		if peer.VideoTrack() == nil {
			if _, err := peer.AddVideoTrack(codec); err != nil {
				r.logger.Warn("failed to add video track to peer",
					zap.String("peer_id", peer.ID()),
					zap.Error(err),
				)
			}
		}
	}
	r.peersMu.Unlock()

	return nil
}

// OnAudioData implements android.VideoSink.
func (r *Relay) OnAudioData(data []byte) error {
	if atomic.LoadInt32(&r.closed) == 1 {
		return errors.New("relay is closed")
	}

	if len(data) == 0 {
		return nil
	}

	if err := r.sendAudioData(data); err != nil {
		return err
	}

	atomic.AddUint64(&r.audioFrames, 1)
	atomic.AddUint64(&r.audioBytes, uint64(len(data)))

	return nil
}

// OnAudioConfig implements android.VideoSink.
func (r *Relay) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	r.audioSampleRate = sampleRate
	r.audioChannels = channels
	r.audioCodec = codec
	r.audioConfig = config

	r.logger.Info("audio config received",
		zap.Int("sample_rate", sampleRate),
		zap.Int("channels", channels),
		zap.String("codec", codec),
	)

	// Add audio tracks to existing peers that don't have them
	r.peersMu.Lock()
	for _, peer := range r.peers {
		if peer.AudioTrack() == nil {
			if _, err := peer.AddAudioTrack(codec); err != nil {
				r.logger.Warn("failed to add audio track to peer",
					zap.String("peer_id", peer.ID()),
					zap.Error(err),
				)
			}
		}
	}
	r.peersMu.Unlock()

	return nil
}

// OnError implements android.VideoSink.
func (r *Relay) OnError(err error) {
	r.logger.Error("stream error", zap.Error(err))
}

// OnClose implements android.VideoSink.
func (r *Relay) OnClose() {
	r.logger.Info("stream closed")
	_ = r.Close()
}

// sendVideoNAL sends a video NAL unit to all peers.
func (r *Relay) sendVideoNAL(nalu []byte) error {
	if len(nalu) == 0 {
		return nil
	}

	r.peersMu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		if peer.State() == PeerStateConnected && peer.VideoTrack() != nil {
			peers = append(peers, peer)
		}
	}
	r.peersMu.RUnlock()

	if len(peers) == 0 {
		return nil
	}

	// Calculate timestamp increment
	now := time.Now()
	r.rtpMu.Lock()
	elapsed := now.Sub(r.lastVideoT)
	r.lastVideoT = now
	r.rtpMu.Unlock()

	// Calculate timestamp increment (90kHz clock)
	timestampIncrement := uint32(elapsed.Seconds() * float64(r.config.VideoClockRate))
	if timestampIncrement == 0 {
		timestampIncrement = 3000 // ~33ms at 30fps
	}

	// Packetize NAL unit
	packets := r.packetizeH264NAL(nalu, timestampIncrement)

	// Send to all connected peers
	for _, peer := range peers {
		track := peer.VideoTrack()
		if track == nil {
			continue
		}

		for _, pkt := range packets {
			if err := track.WriteRTP(pkt); err != nil {
				r.logger.Debug("failed to write video RTP",
					zap.String("peer_id", peer.ID()),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

// packetizeH264NAL packetizes an H.264 NAL unit into RTP packets.
func (r *Relay) packetizeH264NAL(nalu []byte, tsIncrement uint32) []*rtp.Packet {
	r.rtpMu.Lock()
	seq := r.videoSeq
	ts := r.videoTS
	r.videoTS += tsIncrement
	r.rtpMu.Unlock()

	mtu := r.config.VideoMTU
	var packets []*rtp.Packet

	// If NAL fits in one packet, send as single NAL unit
	if len(nalu) <= mtu {
		r.rtpMu.Lock()
		r.videoSeq++
		r.rtpMu.Unlock()

		packets = append(packets, &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96, // Dynamic PT for H.264
				SequenceNumber: seq,
				Timestamp:      ts,
				Marker:         true, // Single NAL = end of frame
			},
			Payload: nalu,
		})
		return packets
	}

	// FU-A fragmentation for large NAL units
	nalHeader := nalu[0]
	nalType := nalHeader & 0x1F
	nalNRI := nalHeader & 0x60

	// Calculate fragment size (accounting for FU header)
	fragmentSize := mtu - 2 // FU indicator + FU header
	payload := nalu[1:]     // Skip NAL header

	for i := 0; i < len(payload); i += fragmentSize {
		end := i + fragmentSize
		if end > len(payload) {
			end = len(payload)
		}

		isStart := i == 0
		isEnd := end == len(payload)

		// FU indicator: same NRI as original, type = 28 (FU-A)
		fuIndicator := nalNRI | 28

		// FU header: S/E bits + original NAL type
		fuHeader := nalType
		if isStart {
			fuHeader |= 0x80 // Start bit
		}
		if isEnd {
			fuHeader |= 0x40 // End bit
		}

		fragment := make([]byte, 2+end-i)
		fragment[0] = fuIndicator
		fragment[1] = fuHeader
		copy(fragment[2:], payload[i:end])

		r.rtpMu.Lock()
		currentSeq := r.videoSeq
		r.videoSeq++
		r.rtpMu.Unlock()

		packets = append(packets, &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    96,
				SequenceNumber: currentSeq,
				Timestamp:      ts,
				Marker:         isEnd, // Marker on last fragment
			},
			Payload: fragment,
		})
	}

	return packets
}

// sendAudioData sends audio data to all peers.
func (r *Relay) sendAudioData(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	r.peersMu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		if peer.State() == PeerStateConnected && peer.AudioTrack() != nil {
			peers = append(peers, peer)
		}
	}
	r.peersMu.RUnlock()

	if len(peers) == 0 {
		return nil
	}

	// Calculate timestamp increment
	now := time.Now()
	r.rtpMu.Lock()
	elapsed := now.Sub(r.lastAudioT)
	r.lastAudioT = now
	seq := r.audioSeq
	r.audioSeq++
	ts := r.audioTS
	// Opus typically uses 20ms frames at 48kHz = 960 samples
	timestampIncrement := uint32(elapsed.Seconds() * float64(r.config.AudioClockRate))
	if timestampIncrement == 0 {
		timestampIncrement = 960 // 20ms at 48kHz
	}
	r.audioTS += timestampIncrement
	r.rtpMu.Unlock()

	// Create RTP packet
	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    111, // Dynamic PT for Opus
			SequenceNumber: seq,
			Timestamp:      ts,
			Marker:         true,
		},
		Payload: data,
	}

	// Send to all connected peers
	for _, peer := range peers {
		track := peer.AudioTrack()
		if track == nil {
			continue
		}

		if err := track.WriteRTP(packet); err != nil {
			r.logger.Debug("failed to write audio RTP",
				zap.String("peer_id", peer.ID()),
				zap.Error(err),
			)
		}
	}

	return nil
}

// Ensure Relay implements android.VideoSink.
var _ android.VideoSink = (*Relay)(nil)

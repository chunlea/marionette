package streaming

import (
	"context"
	"sync"
)

// VideoSink defines an interface for receiving video/audio data from a source.
// This decouples the video source (e.g., scrcpy, Selkies) from the consumer
// (e.g., WebRTC relay, file writer).
type VideoSink interface {
	// OnVideoData is called when video data is received.
	// data contains H.264 NAL units or other codec-specific data.
	OnVideoData(data []byte) error

	// OnVideoConfig is called when video configuration is received.
	// This includes resolution, codec, and codec-specific configuration
	// (e.g., SPS/PPS for H.264).
	OnVideoConfig(width, height int, codec string, config []byte) error

	// OnAudioData is called when audio data is received.
	OnAudioData(data []byte) error

	// OnAudioConfig is called when audio configuration is received.
	// This includes sample rate, channels, codec, and codec-specific configuration.
	OnAudioConfig(sampleRate, channels int, codec string, config []byte) error

	// OnError is called when an error occurs in the source.
	OnError(err error)

	// OnClose is called when the source is closed.
	OnClose()
}

// VideoSource defines an interface for a video/audio source.
type VideoSource interface {
	// SetSink sets the sink that will receive video/audio data.
	SetSink(sink VideoSink)

	// Start starts the video source.
	Start(ctx context.Context) error

	// Stop stops the video source.
	Stop() error

	// VideoConfig returns the current video configuration.
	// Returns zero values if not yet available.
	VideoConfig() (width, height int, codec string)

	// AudioConfig returns the current audio configuration.
	// Returns zero values if not yet available.
	AudioConfig() (sampleRate, channels int, codec string)
}

// MultiSink is a VideoSink that forwards to multiple sinks.
type MultiSink struct {
	mu    sync.RWMutex
	sinks []VideoSink
}

// NewMultiSink creates a new MultiSink.
func NewMultiSink() *MultiSink {
	return &MultiSink{
		sinks: make([]VideoSink, 0),
	}
}

// Add adds a sink to the multi-sink.
func (m *MultiSink) Add(sink VideoSink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sinks = append(m.sinks, sink)
}

// Remove removes a sink from the multi-sink.
func (m *MultiSink) Remove(sink VideoSink) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.sinks {
		if s == sink {
			m.sinks = append(m.sinks[:i], m.sinks[i+1:]...)
			return
		}
	}
}

// Count returns the number of sinks.
func (m *MultiSink) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sinks)
}

// OnVideoData forwards video data to all sinks.
func (m *MultiSink) OnVideoData(data []byte) error {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		if err := sink.OnVideoData(data); err != nil {
			// Log error but continue to other sinks
			continue
		}
	}
	return nil
}

// OnVideoConfig forwards video config to all sinks.
func (m *MultiSink) OnVideoConfig(width, height int, codec string, config []byte) error {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		if err := sink.OnVideoConfig(width, height, codec, config); err != nil {
			continue
		}
	}
	return nil
}

// OnAudioData forwards audio data to all sinks.
func (m *MultiSink) OnAudioData(data []byte) error {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		if err := sink.OnAudioData(data); err != nil {
			continue
		}
	}
	return nil
}

// OnAudioConfig forwards audio config to all sinks.
func (m *MultiSink) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		if err := sink.OnAudioConfig(sampleRate, channels, codec, config); err != nil {
			continue
		}
	}
	return nil
}

// OnError forwards error to all sinks.
func (m *MultiSink) OnError(err error) {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		sink.OnError(err)
	}
}

// OnClose forwards close to all sinks.
func (m *MultiSink) OnClose() {
	m.mu.RLock()
	sinks := make([]VideoSink, len(m.sinks))
	copy(sinks, m.sinks)
	m.mu.RUnlock()

	for _, sink := range sinks {
		sink.OnClose()
	}
}

// Ensure MultiSink implements VideoSink.
var _ VideoSink = (*MultiSink)(nil)

// NopSink is a VideoSink that discards all data.
type NopSink struct{}

// OnVideoData discards the data.
func (NopSink) OnVideoData([]byte) error { return nil }

// OnVideoConfig discards the config.
func (NopSink) OnVideoConfig(int, int, string, []byte) error { return nil }

// OnAudioData discards the data.
func (NopSink) OnAudioData([]byte) error { return nil }

// OnAudioConfig discards the config.
func (NopSink) OnAudioConfig(int, int, string, []byte) error { return nil }

// OnError discards the error.
func (NopSink) OnError(error) {}

// OnClose does nothing.
func (NopSink) OnClose() {}

// Ensure NopSink implements VideoSink.
var _ VideoSink = NopSink{}

package streaming

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingSink records all calls for verification.
type recordingSink struct {
	mu           sync.Mutex
	videoData    [][]byte
	videoConfigs []struct {
		width, height int
		codec         string
		config        []byte
	}
	audioData    [][]byte
	audioConfigs []struct {
		sampleRate, channels int
		codec                string
		config               []byte
	}
	errors []error
	closed bool
}

func newRecordingSink() *recordingSink {
	return &recordingSink{}
}

func (r *recordingSink) OnVideoData(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	r.videoData = append(r.videoData, dataCopy)
	return nil
}

func (r *recordingSink) OnVideoConfig(width, height int, codec string, config []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	configCopy := make([]byte, len(config))
	copy(configCopy, config)
	r.videoConfigs = append(r.videoConfigs, struct {
		width, height int
		codec         string
		config        []byte
	}{width, height, codec, configCopy})
	return nil
}

func (r *recordingSink) OnAudioData(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	r.audioData = append(r.audioData, dataCopy)
	return nil
}

func (r *recordingSink) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	configCopy := make([]byte, len(config))
	copy(configCopy, config)
	r.audioConfigs = append(r.audioConfigs, struct {
		sampleRate, channels int
		codec                string
		config               []byte
	}{sampleRate, channels, codec, configCopy})
	return nil
}

func (r *recordingSink) OnError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, err)
}

func (r *recordingSink) OnClose() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

func (r *recordingSink) VideoDataCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.videoData)
}

func (r *recordingSink) AudioDataCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.audioData)
}

func (r *recordingSink) IsClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// errorSink always returns an error.
type errorSink struct {
	err error
}

func (e *errorSink) OnVideoData([]byte) error                      { return e.err }
func (e *errorSink) OnVideoConfig(int, int, string, []byte) error  { return e.err }
func (e *errorSink) OnAudioData([]byte) error                      { return e.err }
func (e *errorSink) OnAudioConfig(int, int, string, []byte) error  { return e.err }
func (e *errorSink) OnError(error)                                 {}
func (e *errorSink) OnClose()                                      {}

var _ VideoSink = (*recordingSink)(nil)
var _ VideoSink = (*errorSink)(nil)

func TestMultiSink_New(t *testing.T) {
	ms := NewMultiSink()
	require.NotNil(t, ms)
	assert.Equal(t, 0, ms.Count())
}

func TestMultiSink_AddRemove(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	// Add sinks
	ms.Add(sink1)
	assert.Equal(t, 1, ms.Count())

	ms.Add(sink2)
	assert.Equal(t, 2, ms.Count())

	// Remove sink
	ms.Remove(sink1)
	assert.Equal(t, 1, ms.Count())

	// Remove non-existent sink (should be no-op)
	ms.Remove(sink1)
	assert.Equal(t, 1, ms.Count())

	// Remove last sink
	ms.Remove(sink2)
	assert.Equal(t, 0, ms.Count())
}

func TestMultiSink_OnVideoData(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	ms.Add(sink1)
	ms.Add(sink2)

	// Send video data
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x67}
	err := ms.OnVideoData(data)
	require.NoError(t, err)

	// Both sinks should receive the data
	assert.Equal(t, 1, sink1.VideoDataCount())
	assert.Equal(t, 1, sink2.VideoDataCount())

	// Send more data
	err = ms.OnVideoData([]byte{0x00, 0x00, 0x00, 0x01, 0x68})
	require.NoError(t, err)

	assert.Equal(t, 2, sink1.VideoDataCount())
	assert.Equal(t, 2, sink2.VideoDataCount())
}

func TestMultiSink_OnVideoData_WithErrorSink(t *testing.T) {
	ms := NewMultiSink()

	goodSink := newRecordingSink()
	errSink := &errorSink{err: errors.New("test error")}

	ms.Add(errSink)
	ms.Add(goodSink)

	// Send video data - should continue despite error
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x67}
	err := ms.OnVideoData(data)
	require.NoError(t, err) // MultiSink returns nil even if some sinks error

	// Good sink should still receive data
	assert.Equal(t, 1, goodSink.VideoDataCount())
}

func TestMultiSink_OnVideoConfig(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	ms.Add(sink1)
	ms.Add(sink2)

	// Send video config
	config := []byte{0x01, 0x02, 0x03}
	err := ms.OnVideoConfig(1920, 1080, "H264", config)
	require.NoError(t, err)

	// Both sinks should receive the config
	sink1.mu.Lock()
	require.Len(t, sink1.videoConfigs, 1)
	assert.Equal(t, 1920, sink1.videoConfigs[0].width)
	assert.Equal(t, 1080, sink1.videoConfigs[0].height)
	assert.Equal(t, "H264", sink1.videoConfigs[0].codec)
	sink1.mu.Unlock()

	sink2.mu.Lock()
	require.Len(t, sink2.videoConfigs, 1)
	assert.Equal(t, 1920, sink2.videoConfigs[0].width)
	sink2.mu.Unlock()
}

func TestMultiSink_OnAudioData(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	ms.Add(sink1)
	ms.Add(sink2)

	// Send audio data
	data := []byte{0xAA, 0xBB, 0xCC}
	err := ms.OnAudioData(data)
	require.NoError(t, err)

	// Both sinks should receive the data
	assert.Equal(t, 1, sink1.AudioDataCount())
	assert.Equal(t, 1, sink2.AudioDataCount())
}

func TestMultiSink_OnAudioConfig(t *testing.T) {
	ms := NewMultiSink()

	sink := newRecordingSink()
	ms.Add(sink)

	// Send audio config
	config := []byte{0x01}
	err := ms.OnAudioConfig(48000, 2, "opus", config)
	require.NoError(t, err)

	sink.mu.Lock()
	require.Len(t, sink.audioConfigs, 1)
	assert.Equal(t, 48000, sink.audioConfigs[0].sampleRate)
	assert.Equal(t, 2, sink.audioConfigs[0].channels)
	assert.Equal(t, "opus", sink.audioConfigs[0].codec)
	sink.mu.Unlock()
}

func TestMultiSink_OnError(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	ms.Add(sink1)
	ms.Add(sink2)

	// Send error
	testErr := errors.New("test error")
	ms.OnError(testErr)

	// Both sinks should receive the error
	sink1.mu.Lock()
	require.Len(t, sink1.errors, 1)
	assert.Equal(t, testErr, sink1.errors[0])
	sink1.mu.Unlock()

	sink2.mu.Lock()
	require.Len(t, sink2.errors, 1)
	assert.Equal(t, testErr, sink2.errors[0])
	sink2.mu.Unlock()
}

func TestMultiSink_OnClose(t *testing.T) {
	ms := NewMultiSink()

	sink1 := newRecordingSink()
	sink2 := newRecordingSink()

	ms.Add(sink1)
	ms.Add(sink2)

	// Close
	ms.OnClose()

	// Both sinks should be closed
	assert.True(t, sink1.IsClosed())
	assert.True(t, sink2.IsClosed())
}

func TestMultiSink_Concurrent(t *testing.T) {
	ms := NewMultiSink()

	var counter atomic.Int64
	for i := 0; i < 10; i++ {
		sink := newRecordingSink()
		ms.Add(sink)
	}

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ms.OnVideoData([]byte{0x00})
			if err == nil {
				counter.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(100), counter.Load())
}

func TestNopSink(t *testing.T) {
	sink := NopSink{}

	// All methods should succeed and do nothing
	assert.NoError(t, sink.OnVideoData([]byte{0x00}))
	assert.NoError(t, sink.OnVideoConfig(1920, 1080, "H264", []byte{0x01}))
	assert.NoError(t, sink.OnAudioData([]byte{0xAA}))
	assert.NoError(t, sink.OnAudioConfig(48000, 2, "opus", []byte{0x01}))

	// These don't return anything, just verify they don't panic
	sink.OnError(errors.New("test"))
	sink.OnClose()
}

// mockVideoSource is a mock implementation of VideoSource.
type mockVideoSource struct {
	sink        VideoSink
	started     bool
	stopped     bool
	videoWidth  int
	videoHeight int
	videoCodec  string
	audioRate   int
	audioChan   int
	audioCodec  string
}

func (m *mockVideoSource) SetSink(sink VideoSink) {
	m.sink = sink
}

func (m *mockVideoSource) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *mockVideoSource) Stop() error {
	m.stopped = true
	return nil
}

func (m *mockVideoSource) VideoConfig() (int, int, string) {
	return m.videoWidth, m.videoHeight, m.videoCodec
}

func (m *mockVideoSource) AudioConfig() (int, int, string) {
	return m.audioRate, m.audioChan, m.audioCodec
}

var _ VideoSource = (*mockVideoSource)(nil)

func TestVideoSource_Interface(t *testing.T) {
	source := &mockVideoSource{
		videoWidth:  1920,
		videoHeight: 1080,
		videoCodec:  "H264",
		audioRate:   48000,
		audioChan:   2,
		audioCodec:  "opus",
	}

	sink := newRecordingSink()
	source.SetSink(sink)
	assert.Equal(t, sink, source.sink)

	err := source.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, source.started)

	w, h, vc := source.VideoConfig()
	assert.Equal(t, 1920, w)
	assert.Equal(t, 1080, h)
	assert.Equal(t, "H264", vc)

	sr, ch, ac := source.AudioConfig()
	assert.Equal(t, 48000, sr)
	assert.Equal(t, 2, ch)
	assert.Equal(t, "opus", ac)

	err = source.Stop()
	require.NoError(t, err)
	assert.True(t, source.stopped)
}

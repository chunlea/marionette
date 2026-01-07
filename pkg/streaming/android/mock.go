package android

import (
	"context"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
)

// MockProvider is a mock implementation of Provider for testing.
type MockProvider struct {
	mu sync.RWMutex

	// Devices is the list of devices to return from ListDevices.
	Devices []Device

	// Streams is the map of active streams.
	Streams map[string]*StreamInfo

	// Errors configures error responses.
	Errors MockProviderErrors

	// Callbacks for verification.
	OnListDevices func(ctx context.Context) error
	OnGetDevice   func(ctx context.Context, serial string) error
	OnStartStream func(ctx context.Context, opts StreamOptions) error
	OnStopStream  func(ctx context.Context, streamID string) error
	OnSendInput   func(ctx context.Context, serial string, event InputEvent) error
	OnClose       func() error

	closed bool
}

// MockProviderErrors configures which operations should fail.
type MockProviderErrors struct {
	ListDevices error
	GetDevice   error
	StartStream error
	StopStream  error
	GetStream   error
	ListStreams error
	SendInput   error
}

// NewMockProvider creates a new mock provider.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		Streams: make(map[string]*StreamInfo),
	}
}

// AddDevice adds a device to the mock provider.
func (m *MockProvider) AddDevice(device Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Devices = append(m.Devices, device)
}

// SetDevices sets the list of devices.
func (m *MockProvider) SetDevices(devices []Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Devices = devices
}

// ListDevices returns the configured devices.
func (m *MockProvider) ListDevices(ctx context.Context) ([]Device, error) {
	if m.OnListDevices != nil {
		if err := m.OnListDevices(ctx); err != nil {
			return nil, err
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.Errors.ListDevices != nil {
		return nil, m.Errors.ListDevices
	}

	if m.closed {
		return nil, ErrProviderClosed
	}

	// Return a copy to prevent modifications
	devices := make([]Device, len(m.Devices))
	copy(devices, m.Devices)
	return devices, nil
}

// GetDevice returns a specific device by serial.
func (m *MockProvider) GetDevice(ctx context.Context, serial string) (*Device, error) {
	if m.OnGetDevice != nil {
		if err := m.OnGetDevice(ctx, serial); err != nil {
			return nil, err
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.Errors.GetDevice != nil {
		return nil, m.Errors.GetDevice
	}

	if m.closed {
		return nil, ErrProviderClosed
	}

	for _, d := range m.Devices {
		if d.Serial == serial {
			return &d, nil
		}
	}
	return nil, &DeviceNotFoundError{Serial: serial}
}

// StartStream starts a mock stream.
func (m *MockProvider) StartStream(ctx context.Context, opts StreamOptions) (*StreamInfo, error) {
	if m.OnStartStream != nil {
		if err := m.OnStartStream(ctx, opts); err != nil {
			return nil, err
		}
	}

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Errors.StartStream != nil {
		return nil, m.Errors.StartStream
	}

	if m.closed {
		return nil, ErrProviderClosed
	}

	// Check if device exists
	var device *Device
	for _, d := range m.Devices {
		if d.Serial == opts.DeviceSerial {
			device = &d
			break
		}
	}
	if device == nil {
		return nil, &DeviceNotFoundError{Serial: opts.DeviceSerial}
	}

	// Check if stream already running
	for _, s := range m.Streams {
		if s.Device.Serial == opts.DeviceSerial && s.State.IsActive() {
			return nil, ErrStreamAlreadyRunning
		}
	}

	// Apply defaults
	opts = opts.WithDefaults()

	// Create stream
	now := time.Now()
	info := &StreamInfo{
		ID:        id.AndroidStream(),
		Device:    device,
		State:     StreamStateRunning,
		Options:   &opts,
		LocalPort: 27183, // Default scrcpy port
		Width:     1080,
		Height:    1920,
		StartedAt: &now,
	}

	m.Streams[info.ID] = info
	return info, nil
}

// StopStream stops a mock stream.
func (m *MockProvider) StopStream(ctx context.Context, streamID string) error {
	if m.OnStopStream != nil {
		if err := m.OnStopStream(ctx, streamID); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Errors.StopStream != nil {
		return m.Errors.StopStream
	}

	if m.closed {
		return ErrProviderClosed
	}

	info, ok := m.Streams[streamID]
	if !ok {
		return ErrStreamNotFound
	}

	info.State = StreamStateStopped
	delete(m.Streams, streamID)
	return nil
}

// GetStream returns information about a stream.
func (m *MockProvider) GetStream(ctx context.Context, streamID string) (*StreamInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.Errors.GetStream != nil {
		return nil, m.Errors.GetStream
	}

	if m.closed {
		return nil, ErrProviderClosed
	}

	info, ok := m.Streams[streamID]
	if !ok {
		return nil, ErrStreamNotFound
	}
	return info, nil
}

// ListStreams returns all active streams.
func (m *MockProvider) ListStreams(ctx context.Context) ([]*StreamInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.Errors.ListStreams != nil {
		return nil, m.Errors.ListStreams
	}

	if m.closed {
		return nil, ErrProviderClosed
	}

	streams := make([]*StreamInfo, 0, len(m.Streams))
	for _, s := range m.Streams {
		streams = append(streams, s)
	}
	return streams, nil
}

// SendInput sends an input event.
func (m *MockProvider) SendInput(ctx context.Context, serial string, event InputEvent) error {
	if m.OnSendInput != nil {
		if err := m.OnSendInput(ctx, serial, event); err != nil {
			return err
		}
	}

	if err := event.Validate(); err != nil {
		return err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.Errors.SendInput != nil {
		return m.Errors.SendInput
	}

	if m.closed {
		return ErrProviderClosed
	}

	// Check if device exists
	var found bool
	for _, d := range m.Devices {
		if d.Serial == serial {
			found = true
			break
		}
	}
	if !found {
		return &DeviceNotFoundError{Serial: serial}
	}

	return nil
}

// Close closes the mock provider.
func (m *MockProvider) Close() error {
	if m.OnClose != nil {
		if err := m.OnClose(); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	m.Streams = make(map[string]*StreamInfo)
	return nil
}

// IsClosed returns whether the provider is closed.
func (m *MockProvider) IsClosed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.closed
}

// Reset resets the mock provider to initial state.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Devices = nil
	m.Streams = make(map[string]*StreamInfo)
	m.Errors = MockProviderErrors{}
	m.closed = false
}

// Verify MockProvider implements Provider
var _ Provider = (*MockProvider)(nil)

// MockVideoSink is a mock implementation of VideoSink for testing.
type MockVideoSink struct {
	mu sync.Mutex

	VideoDataCalls   [][]byte
	VideoConfigCalls []MockVideoConfigCall
	AudioDataCalls   [][]byte
	AudioConfigCalls []MockAudioConfigCall
	ErrorCalls       []error
	CloseCalled      bool

	VideoDataFunc   func(data []byte) error
	VideoConfigFunc func(width, height int, codec string, config []byte) error
	AudioDataFunc   func(data []byte) error
	AudioConfigFunc func(sampleRate, channels int, codec string, config []byte) error
	ErrorFunc       func(err error)
	CloseFunc       func()
}

// MockVideoConfigCall records a video config call.
type MockVideoConfigCall struct {
	Width  int
	Height int
	Codec  string
	Config []byte
}

// MockAudioConfigCall records an audio config call.
type MockAudioConfigCall struct {
	SampleRate int
	Channels   int
	Codec      string
	Config     []byte
}

// NewMockVideoSink creates a new mock video sink.
func NewMockVideoSink() *MockVideoSink {
	return &MockVideoSink{}
}

// OnVideoData records video data.
func (m *MockVideoSink) OnVideoData(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.VideoDataFunc != nil {
		return m.VideoDataFunc(data)
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.VideoDataCalls = append(m.VideoDataCalls, dataCopy)
	return nil
}

// OnVideoConfig records video configuration.
func (m *MockVideoSink) OnVideoConfig(width, height int, codec string, config []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.VideoConfigFunc != nil {
		return m.VideoConfigFunc(width, height, codec, config)
	}

	configCopy := make([]byte, len(config))
	copy(configCopy, config)
	m.VideoConfigCalls = append(m.VideoConfigCalls, MockVideoConfigCall{
		Width:  width,
		Height: height,
		Codec:  codec,
		Config: configCopy,
	})
	return nil
}

// OnAudioData records audio data.
func (m *MockVideoSink) OnAudioData(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.AudioDataFunc != nil {
		return m.AudioDataFunc(data)
	}

	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	m.AudioDataCalls = append(m.AudioDataCalls, dataCopy)
	return nil
}

// OnAudioConfig records audio configuration.
func (m *MockVideoSink) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.AudioConfigFunc != nil {
		return m.AudioConfigFunc(sampleRate, channels, codec, config)
	}

	configCopy := make([]byte, len(config))
	copy(configCopy, config)
	m.AudioConfigCalls = append(m.AudioConfigCalls, MockAudioConfigCall{
		SampleRate: sampleRate,
		Channels:   channels,
		Codec:      codec,
		Config:     configCopy,
	})
	return nil
}

// OnError records an error.
func (m *MockVideoSink) OnError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrorFunc != nil {
		m.ErrorFunc(err)
		return
	}

	m.ErrorCalls = append(m.ErrorCalls, err)
}

// OnClose records close call.
func (m *MockVideoSink) OnClose() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.CloseFunc != nil {
		m.CloseFunc()
		return
	}

	m.CloseCalled = true
}

// Reset resets the mock sink.
func (m *MockVideoSink) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.VideoDataCalls = nil
	m.VideoConfigCalls = nil
	m.AudioDataCalls = nil
	m.AudioConfigCalls = nil
	m.ErrorCalls = nil
	m.CloseCalled = false
}

// Verify MockVideoSink implements VideoSink
var _ VideoSink = (*MockVideoSink)(nil)

// MockInputHandler is a mock implementation of InputHandler for testing.
type MockInputHandler struct {
	mu sync.Mutex

	TapCalls       []MockTapCall
	SwipeCalls     []MockSwipeCall
	LongPressCalls []MockLongPressCall
	TextCalls      []string
	KeyCalls       []int
	BackCalls      int
	HomeCalls      int
	RecentCalls    int

	DisplayWidth  int
	DisplayHeight int

	Error error // Error to return from all operations
}

// MockTapCall records a tap call.
type MockTapCall struct {
	X, Y int
}

// MockSwipeCall records a swipe call.
type MockSwipeCall struct {
	StartX, StartY, EndX, EndY, DurationMs int
}

// MockLongPressCall records a long press call.
type MockLongPressCall struct {
	X, Y, DurationMs int
}

// NewMockInputHandler creates a new mock input handler.
func NewMockInputHandler() *MockInputHandler {
	return &MockInputHandler{
		DisplayWidth:  1080,
		DisplayHeight: 1920,
	}
}

func (m *MockInputHandler) HandleTap(ctx context.Context, x, y int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.TapCalls = append(m.TapCalls, MockTapCall{X: x, Y: y})
	return nil
}

func (m *MockInputHandler) HandleSwipe(ctx context.Context, startX, startY, endX, endY, durationMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.SwipeCalls = append(m.SwipeCalls, MockSwipeCall{
		StartX: startX, StartY: startY, EndX: endX, EndY: endY, DurationMs: durationMs,
	})
	return nil
}

func (m *MockInputHandler) HandleLongPress(ctx context.Context, x, y, durationMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.LongPressCalls = append(m.LongPressCalls, MockLongPressCall{X: x, Y: y, DurationMs: durationMs})
	return nil
}

func (m *MockInputHandler) HandleText(ctx context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.TextCalls = append(m.TextCalls, text)
	return nil
}

func (m *MockInputHandler) HandleKey(ctx context.Context, keyCode int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.KeyCalls = append(m.KeyCalls, keyCode)
	return nil
}

func (m *MockInputHandler) HandleBack(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.BackCalls++
	return nil
}

func (m *MockInputHandler) HandleHome(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.HomeCalls++
	return nil
}

func (m *MockInputHandler) HandleRecent(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Error != nil {
		return m.Error
	}
	m.RecentCalls++
	return nil
}

func (m *MockInputHandler) SetDisplaySize(width, height int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DisplayWidth = width
	m.DisplayHeight = height
}

// Reset resets the mock input handler.
func (m *MockInputHandler) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TapCalls = nil
	m.SwipeCalls = nil
	m.LongPressCalls = nil
	m.TextCalls = nil
	m.KeyCalls = nil
	m.BackCalls = 0
	m.HomeCalls = 0
	m.RecentCalls = 0
	m.Error = nil
}

// Verify MockInputHandler implements InputHandler
var _ InputHandler = (*MockInputHandler)(nil)

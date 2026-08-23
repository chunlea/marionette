// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// MockProvider is a test implementation of Provider.
// It simulates browser streaming for unit tests.
type MockProvider struct {
	mu sync.RWMutex

	config  *ProviderConfig
	state   atomic.Value // stores StreamState
	options *StreamOptions
	info    *StreamInfo

	buffer       *FrameBuffer
	stateHandler StateHandler

	frameSeq   uint64
	startedAt  *time.Time
	stoppedAt  *time.Time
	createdAt  time.Time
	closedOnce sync.Once
	closed     atomic.Bool

	// Test hooks
	onStart    func(ctx context.Context, opts *StreamOptions) error
	onStop     func(ctx context.Context) error
	onInput    func(ctx context.Context, event *InputEvent) error
	onNavigate func(ctx context.Context, req *NavigateRequest) error

	// Mock data
	browserInfo *BrowserInfo
	tabs        []*TabInfo
}

// MockProviderOption configures a MockProvider.
type MockProviderOption func(*MockProvider)

// WithMockOnStart sets the onStart hook.
func WithMockOnStart(fn func(ctx context.Context, opts *StreamOptions) error) MockProviderOption {
	return func(p *MockProvider) {
		p.onStart = fn
	}
}

// WithMockOnStop sets the onStop hook.
func WithMockOnStop(fn func(ctx context.Context) error) MockProviderOption {
	return func(p *MockProvider) {
		p.onStop = fn
	}
}

// WithMockOnInput sets the onInput hook.
func WithMockOnInput(fn func(ctx context.Context, event *InputEvent) error) MockProviderOption {
	return func(p *MockProvider) {
		p.onInput = fn
	}
}

// WithMockOnNavigate sets the onNavigate hook.
func WithMockOnNavigate(fn func(ctx context.Context, req *NavigateRequest) error) MockProviderOption {
	return func(p *MockProvider) {
		p.onNavigate = fn
	}
}

// WithMockBrowserInfo sets the browser info.
func WithMockBrowserInfo(info *BrowserInfo) MockProviderOption {
	return func(p *MockProvider) {
		p.browserInfo = info
	}
}

// WithMockTabs sets the tab list.
func WithMockTabs(tabs []*TabInfo) MockProviderOption {
	return func(p *MockProvider) {
		p.tabs = tabs
	}
}

// NewMockProvider creates a new MockProvider for testing.
func NewMockProvider(cfg *ProviderConfig, opts ...MockProviderOption) *MockProvider {
	if cfg == nil {
		cfg = &ProviderConfig{
			CDPEndpoint: "ws://mock:9222/devtools/browser/mock",
			BufferSize:  DefaultBufferSize,
		}
	}

	now := time.Now()
	p := &MockProvider{
		config:    cfg,
		createdAt: now,
		buffer: NewFrameBuffer(FrameBufferConfig{
			Capacity:   cfg.BufferSize,
			DropPolicy: DropPolicyNewest,
		}),
		browserInfo: &BrowserInfo{
			Product:         "Mock/1.0.0",
			UserAgent:       "MockBrowser/1.0",
			ProtocolVersion: "1.3",
		},
		tabs: []*TabInfo{
			{
				ID:    "mock-tab-1",
				Title: "Mock Page",
				URL:   "about:blank",
				Type:  "page",
			},
		},
	}
	p.state.Store(StreamStateIdle)

	for _, opt := range opts {
		opt(p)
	}

	p.info = &StreamInfo{
		ID:        "mock-stream-1",
		SessionID: "mock-session-1",
		RunnerID:  "mock-runner-1",
		State:     StreamStateIdle,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return p
}

// Start implements Provider.Start.
func (p *MockProvider) Start(ctx context.Context, opts *StreamOptions) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	currentState := p.state.Load().(StreamState)
	if currentState == StreamStateActive {
		return ErrStreamAlreadyActive
	}

	if opts == nil {
		opts = &StreamOptions{}
	}
	if err := opts.Validate(); err != nil {
		return err
	}

	if p.onStart != nil {
		if err := p.onStart(ctx, opts); err != nil {
			return err
		}
	}

	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(StreamStateStarting)
	p.options = opts.Clone()
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateStarting, nil)

	now := time.Now()
	p.mu.Lock()
	p.startedAt = &now
	p.stoppedAt = nil
	oldState = StreamStateStarting
	p.state.Store(StreamStateActive)
	p.info.State = StreamStateActive
	p.info.Options = p.options
	p.info.StartedAt = p.startedAt
	p.info.StoppedAt = nil
	p.info.UpdatedAt = now
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateActive, nil)

	return nil
}

// Stop implements Provider.Stop.
func (p *MockProvider) Stop(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}

	currentState := p.state.Load().(StreamState)
	if currentState == StreamStateStopped || currentState == StreamStateIdle {
		return nil
	}

	if p.onStop != nil {
		if err := p.onStop(ctx); err != nil {
			return err
		}
	}

	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(StreamStateStopping)
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateStopping, nil)

	now := time.Now()
	p.mu.Lock()
	p.stoppedAt = &now
	oldState = StreamStateStopping
	p.state.Store(StreamStateStopped)
	p.info.State = StreamStateStopped
	p.info.StoppedAt = p.stoppedAt
	p.info.UpdatedAt = now
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateStopped, nil)

	return nil
}

// Pause implements Provider.Pause.
func (p *MockProvider) Pause(ctx context.Context) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	currentState := p.state.Load().(StreamState)
	if currentState != StreamStateActive {
		return ErrStreamNotActive
	}

	p.mu.Lock()
	oldState := currentState
	p.state.Store(StreamStatePaused)
	p.info.State = StreamStatePaused
	p.info.UpdatedAt = time.Now()
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStatePaused, nil)

	return nil
}

// Resume implements Provider.Resume.
func (p *MockProvider) Resume(ctx context.Context) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	currentState := p.state.Load().(StreamState)
	if currentState != StreamStatePaused {
		return ErrStreamNotActive
	}

	p.mu.Lock()
	oldState := currentState
	p.state.Store(StreamStateActive)
	p.info.State = StreamStateActive
	p.info.UpdatedAt = time.Now()
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateActive, nil)

	return nil
}

// State implements Provider.State.
func (p *MockProvider) State() StreamState {
	return p.state.Load().(StreamState)
}

// Info implements Provider.Info.
func (p *MockProvider) Info() *StreamInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	info := *p.info
	info.State = p.state.Load().(StreamState)
	if p.options != nil {
		info.Options = p.options.Clone()
	}
	info.Stats = p.statsLocked()
	return &info
}

// Stats implements Provider.Stats.
func (p *MockProvider) Stats() *StreamStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.statsLocked()
}

func (p *MockProvider) statsLocked() *StreamStats {
	bufStats := p.buffer.Stats()
	return &StreamStats{
		FramesSent:    bufStats.TotalFrames - bufStats.DroppedFrames,
		FramesDropped: bufStats.DroppedFrames,
	}
}

// Frames implements Provider.Frames.
func (p *MockProvider) Frames() <-chan *Frame {
	return p.buffer.Frames()
}

// SendInput implements Provider.SendInput.
func (p *MockProvider) SendInput(ctx context.Context, event *InputEvent) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	if event == nil {
		return ErrInvalidInputEvent
	}

	if !event.Type.IsValid() {
		return ErrInvalidInputEvent
	}

	if p.onInput != nil {
		return p.onInput(ctx, event)
	}

	return nil
}

// Navigate implements Provider.Navigate.
func (p *MockProvider) Navigate(ctx context.Context, req *NavigateRequest) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	if err := req.Validate(); err != nil {
		return err
	}

	if p.onNavigate != nil {
		return p.onNavigate(ctx, req)
	}

	return nil
}

// GetBrowserInfo implements Provider.GetBrowserInfo.
func (p *MockProvider) GetBrowserInfo(ctx context.Context) (*BrowserInfo, error) {
	if p.closed.Load() {
		return nil, ErrProviderClosed
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.browserInfo == nil {
		return nil, ErrBrowserNotConnected
	}

	info := *p.browserInfo
	return &info, nil
}

// ListTabs implements Provider.ListTabs.
func (p *MockProvider) ListTabs(ctx context.Context) ([]*TabInfo, error) {
	if p.closed.Load() {
		return nil, ErrProviderClosed
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	tabs := make([]*TabInfo, len(p.tabs))
	for i, tab := range p.tabs {
		t := *tab
		tabs[i] = &t
	}
	return tabs, nil
}

// SwitchTab implements Provider.SwitchTab.
func (p *MockProvider) SwitchTab(ctx context.Context, tabID string) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, tab := range p.tabs {
		if tab.ID == tabID {
			return nil
		}
	}

	return ErrStreamNotFound
}

// OnStateChange implements Provider.OnStateChange.
func (p *MockProvider) OnStateChange(handler StateHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stateHandler = handler
}

// Close implements Provider.Close.
func (p *MockProvider) Close() error {
	var err error
	p.closedOnce.Do(func() {
		// Stop first (before setting closed, otherwise Stop() will return early)
		currentState := p.state.Load().(StreamState)
		if currentState == StreamStateActive || currentState == StreamStatePaused {
			_ = p.Stop(context.Background())
		}

		p.closed.Store(true)
		err = p.buffer.Close()
	})
	return err
}

// notifyStateChange notifies the state handler of a state change.
func (p *MockProvider) notifyStateChange(oldState, newState StreamState, err error) {
	p.mu.RLock()
	handler := p.stateHandler
	p.mu.RUnlock()

	if handler != nil {
		handler(context.Background(), oldState, newState, err)
	}
}

// --- Test helper methods ---

// PushFrame adds a frame to the buffer (for testing).
func (p *MockProvider) PushFrame(frame *Frame) error {
	if frame.Sequence == 0 {
		p.mu.Lock()
		p.frameSeq++
		frame.Sequence = p.frameSeq
		p.mu.Unlock()
	}
	return p.buffer.Push(frame)
}

// SetState sets the stream state (for testing).
func (p *MockProvider) SetState(state StreamState) {
	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(state)
	p.info.State = state
	p.info.UpdatedAt = time.Now()
	p.mu.Unlock()

	p.notifyStateChange(oldState, state, nil)
}

// SetError sets an error state (for testing).
func (p *MockProvider) SetError(errMsg string) {
	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(StreamStateError)
	p.info.State = StreamStateError
	p.info.Error = errMsg
	p.info.UpdatedAt = time.Now()
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateError, nil)
}

// SetBrowserInfo sets the browser info (for testing).
func (p *MockProvider) SetBrowserInfo(info *BrowserInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.browserInfo = info
}

// SetTabs sets the tab list (for testing).
func (p *MockProvider) SetTabs(tabs []*TabInfo) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tabs = tabs
}

// Buffer returns the frame buffer (for testing).
func (p *MockProvider) Buffer() *FrameBuffer {
	return p.buffer
}

// Ensure MockProvider implements Provider.
var _ Provider = (*MockProvider)(nil)

// MockProviderFactory creates MockProvider instances.
type MockProviderFactory struct {
	providers []*MockProvider
	mu        sync.Mutex
}

// NewMockProviderFactory creates a new MockProviderFactory.
func NewMockProviderFactory() *MockProviderFactory {
	return &MockProviderFactory{}
}

// Create implements ProviderFactory.Create.
func (f *MockProviderFactory) Create(cfg *ProviderConfig) (Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	p := NewMockProvider(cfg)

	f.mu.Lock()
	f.providers = append(f.providers, p)
	f.mu.Unlock()

	return p, nil
}

// Name implements ProviderFactory.Name.
func (f *MockProviderFactory) Name() string {
	return "mock"
}

// Providers returns all created providers (for testing).
func (f *MockProviderFactory) Providers() []*MockProvider {
	f.mu.Lock()
	defer f.mu.Unlock()

	providers := make([]*MockProvider, len(f.providers))
	copy(providers, f.providers)
	return providers
}

// Ensure MockProviderFactory implements ProviderFactory.
var _ ProviderFactory = (*MockProviderFactory)(nil)

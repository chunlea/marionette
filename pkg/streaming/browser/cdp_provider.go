// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// CDPProvider implements Provider using Chrome DevTools Protocol.
type CDPProvider struct {
	mu sync.RWMutex

	// Configuration
	config  *ProviderConfig
	options *StreamOptions

	// State
	state        atomic.Value // stores StreamState
	info         *StreamInfo
	stateHandler StateHandler

	// Chrome process and connection
	chrome      *Chrome
	cdp         *CDPConnection
	browserInfo *BrowserInfo

	// Frame capture
	buffer        *FrameBuffer
	frameSeq      uint64
	captureCtx    context.Context
	captureCancel context.CancelFunc

	// Screencast session ID
	screencastSessionID atomic.Int32

	// Lifecycle
	startedAt  *time.Time
	stoppedAt  *time.Time
	createdAt  time.Time
	closedOnce sync.Once
	closed     atomic.Bool

	logger *zap.Logger
}

// CDPProviderConfig contains configuration for CDPProvider.
type CDPProviderConfig struct {
	// ChromeConfig is the Chrome process configuration.
	// If nil, defaults will be used.
	ChromeConfig *ChromeConfig

	// ProviderConfig is the base provider configuration.
	// CDPEndpoint will be overwritten after Chrome starts.
	ProviderConfig *ProviderConfig

	// Logger is the logger to use. If nil, a no-op logger is used.
	Logger *zap.Logger
}

// NewCDPProvider creates a new CDP-based browser streaming provider.
func NewCDPProvider(cfg *CDPProviderConfig) (*CDPProvider, error) {
	if cfg == nil {
		cfg = &CDPProviderConfig{}
	}

	chromeConfig := cfg.ChromeConfig
	if chromeConfig == nil {
		chromeConfig = &ChromeConfig{
			Headless: true,
		}
	}

	providerConfig := cfg.ProviderConfig
	if providerConfig == nil {
		providerConfig = &ProviderConfig{
			BufferSize: DefaultBufferSize,
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	now := time.Now()
	p := &CDPProvider{
		config:    providerConfig,
		chrome:    NewChrome(chromeConfig),
		createdAt: now,
		buffer: NewFrameBuffer(FrameBufferConfig{
			Capacity:   providerConfig.BufferSize,
			DropPolicy: DropPolicyNewest,
		}),
		logger: logger,
	}
	p.state.Store(StreamStateIdle)

	p.info = &StreamInfo{
		ID:        fmt.Sprintf("cdp-%d", now.UnixNano()),
		State:     StreamStateIdle,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return p, nil
}

// Start implements Provider.Start.
func (p *CDPProvider) Start(ctx context.Context, opts *StreamOptions) error {
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

	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(StreamStateStarting)
	p.options = opts.Clone()
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateStarting, nil)

	// Start Chrome
	p.logger.Info("starting chrome")
	if err := p.chrome.Start(ctx); err != nil {
		p.setState(StreamStateError)
		return fmt.Errorf("starting chrome: %w", err)
	}

	// Update config with actual CDP endpoint
	p.config.CDPEndpoint = p.chrome.CDPEndpoint()
	p.logger.Info("chrome started", zap.String("cdp_endpoint", p.config.CDPEndpoint))

	// Connect to CDP
	p.cdp = NewCDPConnection(p.config.CDPEndpoint)
	if err := p.cdp.Connect(ctx); err != nil {
		_ = p.chrome.Stop()
		p.setState(StreamStateError)
		return fmt.Errorf("connecting to CDP: %w", err)
	}
	p.logger.Info("connected to CDP")

	// Enable required domains
	if err := p.cdp.PageEnable(ctx); err != nil {
		p.cleanup()
		p.setState(StreamStateError)
		return fmt.Errorf("enabling Page domain: %w", err)
	}

	// Get browser info
	info, err := p.cdp.BrowserGetVersion(ctx)
	if err != nil {
		p.logger.Warn("failed to get browser version", zap.Error(err))
	} else {
		p.mu.Lock()
		p.browserInfo = info
		p.mu.Unlock()
		p.logger.Info("browser info", zap.String("product", info.Product))
	}

	// Register screencast frame handler
	p.cdp.OnEvent("Page.screencastFrame", p.handleScreencastFrame)

	// Start screencast
	screencastParams := &ScreencastFrameParams{
		Format:        string(opts.Format),
		Quality:       opts.Quality,
		MaxWidth:      opts.MaxWidth,
		MaxHeight:     opts.MaxHeight,
		EveryNthFrame: opts.EveryNthFrame,
	}
	if screencastParams.Format == "" {
		screencastParams.Format = "jpeg"
	}
	if screencastParams.Quality == 0 {
		screencastParams.Quality = 80
	}

	if err := p.cdp.PageStartScreencast(ctx, screencastParams); err != nil {
		p.cleanup()
		p.setState(StreamStateError)
		return fmt.Errorf("starting screencast: %w", err)
	}
	p.logger.Info("screencast started")

	// Update state
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

	// Start frame rate limiting goroutine if needed
	if opts.MaxFPS > 0 && opts.MaxFPS < 60 {
		p.captureCtx, p.captureCancel = context.WithCancel(context.Background())
		// Frame rate is controlled by EveryNthFrame parameter in screencast
	}

	return nil
}

// handleScreencastFrame processes Page.screencastFrame events.
func (p *CDPProvider) handleScreencastFrame(params json.RawMessage) {
	var event ScreencastFrameEvent
	if err := json.Unmarshal(params, &event); err != nil {
		p.logger.Error("failed to parse screencast frame", zap.Error(err))
		return
	}

	// Decode frame data
	data, err := base64.StdEncoding.DecodeString(event.Data)
	if err != nil {
		p.logger.Error("failed to decode frame data", zap.Error(err))
		return
	}

	// Acknowledge frame immediately to receive next one
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.cdp.PageScreencastFrameAck(ctx, event.SessionID); err != nil {
			p.logger.Warn("failed to ack screencast frame", zap.Error(err))
		}
	}()

	// Store session ID for internal tracking
	p.screencastSessionID.Store(int32(event.SessionID))

	// Create frame
	p.mu.Lock()
	p.frameSeq++
	seq := p.frameSeq
	p.mu.Unlock()

	frame := &Frame{
		Data:      data,
		Format:    p.options.Format,
		Width:     int(event.Metadata.DeviceWidth),
		Height:    int(event.Metadata.DeviceHeight),
		Timestamp: time.Now(),
		Sequence:  seq,
	}

	// Push to buffer
	if err := p.buffer.Push(frame); err != nil {
		// Buffer full or closed, frame dropped
		p.logger.Debug("frame dropped", zap.Uint64("sequence", seq))
	}
}

// Stop implements Provider.Stop.
func (p *CDPProvider) Stop(ctx context.Context) error {
	if p.closed.Load() {
		return nil
	}

	currentState := p.state.Load().(StreamState)
	if currentState == StreamStateStopped || currentState == StreamStateIdle {
		return nil
	}

	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(StreamStateStopping)
	p.mu.Unlock()

	p.notifyStateChange(oldState, StreamStateStopping, nil)

	// Cancel capture context
	if p.captureCancel != nil {
		p.captureCancel()
	}

	// Stop screencast
	if p.cdp != nil && !p.cdp.IsClosed() {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := p.cdp.PageStopScreencast(stopCtx); err != nil {
			p.logger.Warn("failed to stop screencast", zap.Error(err))
		}
	}

	// Cleanup Chrome and CDP
	p.cleanup()

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

// cleanup closes CDP connection and stops Chrome.
func (p *CDPProvider) cleanup() {
	if p.cdp != nil {
		_ = p.cdp.Close()
	}
	if p.chrome != nil {
		_ = p.chrome.Stop()
	}
}

// Pause implements Provider.Pause.
func (p *CDPProvider) Pause(ctx context.Context) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	currentState := p.state.Load().(StreamState)
	if currentState != StreamStateActive {
		return ErrStreamNotActive
	}

	// Stop screencast temporarily
	if p.cdp != nil && !p.cdp.IsClosed() {
		if err := p.cdp.PageStopScreencast(ctx); err != nil {
			return fmt.Errorf("stopping screencast: %w", err)
		}
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
func (p *CDPProvider) Resume(ctx context.Context) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	currentState := p.state.Load().(StreamState)
	if currentState != StreamStatePaused {
		return ErrStreamNotActive
	}

	// Restart screencast
	if p.cdp != nil && !p.cdp.IsClosed() {
		opts := p.options
		screencastParams := &ScreencastFrameParams{
			Format:        string(opts.Format),
			Quality:       opts.Quality,
			MaxWidth:      opts.MaxWidth,
			MaxHeight:     opts.MaxHeight,
			EveryNthFrame: opts.EveryNthFrame,
		}
		if err := p.cdp.PageStartScreencast(ctx, screencastParams); err != nil {
			return fmt.Errorf("starting screencast: %w", err)
		}
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
func (p *CDPProvider) State() StreamState {
	return p.state.Load().(StreamState)
}

// Info implements Provider.Info.
func (p *CDPProvider) Info() *StreamInfo {
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
func (p *CDPProvider) Stats() *StreamStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.statsLocked()
}

func (p *CDPProvider) statsLocked() *StreamStats {
	bufStats := p.buffer.Stats()
	stats := &StreamStats{
		FramesSent:    bufStats.TotalFrames - bufStats.DroppedFrames,
		FramesDropped: bufStats.DroppedFrames,
	}

	if p.startedAt != nil {
		elapsed := time.Since(*p.startedAt).Seconds()
		if elapsed > 0 {
			stats.AverageFPS = float64(stats.FramesSent) / elapsed
		}
	}

	return stats
}

// Frames implements Provider.Frames.
func (p *CDPProvider) Frames() <-chan *Frame {
	return p.buffer.Frames()
}

// SendInput implements Provider.SendInput.
func (p *CDPProvider) SendInput(ctx context.Context, event *InputEvent) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	if event == nil {
		return ErrInvalidInputEvent
	}

	if !event.Type.IsValid() {
		return ErrInvalidInputEvent
	}

	if p.cdp == nil || p.cdp.IsClosed() {
		return ErrBrowserNotConnected
	}

	switch event.Type {
	case InputEventMouseMove:
		return p.cdp.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
			Type:      "mouseMoved",
			X:         event.Mouse.X,
			Y:         event.Mouse.Y,
			Modifiers: modifiersToInt(event.Mouse.Modifiers),
		})

	case InputEventMouseDown:
		return p.cdp.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
			Type:       "mousePressed",
			X:          event.Mouse.X,
			Y:          event.Mouse.Y,
			Button:     string(event.Mouse.Button),
			ClickCount: event.Mouse.ClickCount,
			Modifiers:  modifiersToInt(event.Mouse.Modifiers),
		})

	case InputEventMouseUp:
		return p.cdp.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
			Type:       "mouseReleased",
			X:          event.Mouse.X,
			Y:          event.Mouse.Y,
			Button:     string(event.Mouse.Button),
			ClickCount: event.Mouse.ClickCount,
			Modifiers:  modifiersToInt(event.Mouse.Modifiers),
		})

	case InputEventMouseWheel:
		return p.cdp.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
			Type:      "mouseWheel",
			X:         event.Mouse.X,
			Y:         event.Mouse.Y,
			DeltaX:    event.Mouse.DeltaX,
			DeltaY:    event.Mouse.DeltaY,
			Modifiers: modifiersToInt(event.Mouse.Modifiers),
		})

	case InputEventKeyDown:
		return p.cdp.InputDispatchKeyEvent(ctx, &InputDispatchKeyEventParams{
			Type:      "keyDown",
			Key:       event.Keyboard.Key,
			Code:      event.Keyboard.Code,
			Text:      event.Keyboard.Text,
			Modifiers: modifiersToInt(event.Keyboard.Modifiers),
		})

	case InputEventKeyUp:
		return p.cdp.InputDispatchKeyEvent(ctx, &InputDispatchKeyEventParams{
			Type:      "keyUp",
			Key:       event.Keyboard.Key,
			Code:      event.Keyboard.Code,
			Modifiers: modifiersToInt(event.Keyboard.Modifiers),
		})

	default:
		return ErrInvalidInputEvent
	}
}

// modifiersToInt converts Modifiers to CDP modifiers bitfield.
func modifiersToInt(m *Modifiers) int {
	if m == nil {
		return 0
	}
	var mod int
	if m.Alt {
		mod |= 1
	}
	if m.Ctrl {
		mod |= 2
	}
	if m.Meta {
		mod |= 4
	}
	if m.Shift {
		mod |= 8
	}
	return mod
}

// Navigate implements Provider.Navigate.
func (p *CDPProvider) Navigate(ctx context.Context, req *NavigateRequest) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	if err := req.Validate(); err != nil {
		return err
	}

	if p.cdp == nil || p.cdp.IsClosed() {
		return ErrBrowserNotConnected
	}

	_, err := p.cdp.PageNavigate(ctx, req.URL, req.Referrer)
	return err
}

// GetBrowserInfo implements Provider.GetBrowserInfo.
func (p *CDPProvider) GetBrowserInfo(ctx context.Context) (*BrowserInfo, error) {
	if p.closed.Load() {
		return nil, ErrProviderClosed
	}

	p.mu.RLock()
	info := p.browserInfo
	p.mu.RUnlock()

	if info != nil {
		return info, nil
	}

	if p.cdp == nil || p.cdp.IsClosed() {
		return nil, ErrBrowserNotConnected
	}

	return p.cdp.BrowserGetVersion(ctx)
}

// ListTabs implements Provider.ListTabs.
func (p *CDPProvider) ListTabs(ctx context.Context) ([]*TabInfo, error) {
	if p.closed.Load() {
		return nil, ErrProviderClosed
	}

	if p.cdp == nil || p.cdp.IsClosed() {
		return nil, ErrBrowserNotConnected
	}

	targets, err := p.cdp.TargetGetTargets(ctx)
	if err != nil {
		return nil, err
	}

	var tabs []*TabInfo
	for _, t := range targets {
		if t.Type == "page" {
			tabs = append(tabs, &TabInfo{
				ID:       t.TargetID,
				Title:    t.Title,
				URL:      t.URL,
				Type:     t.Type,
				Attached: t.Attached,
			})
		}
	}

	return tabs, nil
}

// SwitchTab implements Provider.SwitchTab.
func (p *CDPProvider) SwitchTab(ctx context.Context, tabID string) error {
	if p.closed.Load() {
		return ErrProviderClosed
	}

	if p.cdp == nil || p.cdp.IsClosed() {
		return ErrBrowserNotConnected
	}

	return p.cdp.TargetActivateTarget(ctx, tabID)
}

// OnStateChange implements Provider.OnStateChange.
func (p *CDPProvider) OnStateChange(handler StateHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stateHandler = handler
}

// Close implements Provider.Close.
func (p *CDPProvider) Close() error {
	var err error
	p.closedOnce.Do(func() {
		// Stop first (before setting closed)
		currentState := p.state.Load().(StreamState)
		if currentState == StreamStateActive || currentState == StreamStatePaused {
			_ = p.Stop(context.Background())
		}

		p.closed.Store(true)
		err = p.buffer.Close()
	})
	return err
}

// setState updates the state and notifies handlers.
func (p *CDPProvider) setState(state StreamState) {
	p.mu.Lock()
	oldState := p.state.Load().(StreamState)
	p.state.Store(state)
	p.info.State = state
	p.info.UpdatedAt = time.Now()
	p.mu.Unlock()

	p.notifyStateChange(oldState, state, nil)
}

// notifyStateChange notifies the state handler of a state change.
func (p *CDPProvider) notifyStateChange(oldState, newState StreamState, err error) {
	p.mu.RLock()
	handler := p.stateHandler
	p.mu.RUnlock()

	if handler != nil {
		handler(context.Background(), oldState, newState, err)
	}
}

// Ensure CDPProvider implements Provider.
var _ Provider = (*CDPProvider)(nil)

// CDPProviderFactory creates CDPProvider instances.
type CDPProviderFactory struct {
	chromeConfig *ChromeConfig
	logger       *zap.Logger
}

// CDPFactoryOption configures a CDPProviderFactory.
type CDPFactoryOption func(*CDPProviderFactory)

// WithChromeConfig sets the Chrome configuration.
func WithChromeConfig(cfg *ChromeConfig) CDPFactoryOption {
	return func(f *CDPProviderFactory) {
		f.chromeConfig = cfg
	}
}

// WithLogger sets the logger.
func WithLogger(logger *zap.Logger) CDPFactoryOption {
	return func(f *CDPProviderFactory) {
		f.logger = logger
	}
}

// NewCDPProviderFactory creates a new CDPProviderFactory.
func NewCDPProviderFactory(opts ...CDPFactoryOption) *CDPProviderFactory {
	f := &CDPProviderFactory{
		chromeConfig: &ChromeConfig{
			Headless: true,
		},
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Create implements ProviderFactory.Create.
func (f *CDPProviderFactory) Create(cfg *ProviderConfig) (Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return NewCDPProvider(&CDPProviderConfig{
		ChromeConfig:   f.chromeConfig,
		ProviderConfig: cfg,
		Logger:         f.logger,
	})
}

// Name implements ProviderFactory.Name.
func (f *CDPProviderFactory) Name() string {
	return "cdp"
}

// Ensure CDPProviderFactory implements ProviderFactory.
var _ ProviderFactory = (*CDPProviderFactory)(nil)

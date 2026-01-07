package browser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewMockProvider(t *testing.T) {
	p := NewMockProvider(nil)

	if p.State() != StreamStateIdle {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateIdle)
	}

	info := p.Info()
	if info == nil {
		t.Fatal("Info() returned nil")
	}
	if info.ID == "" {
		t.Error("Info().ID is empty")
	}
	if info.SessionID == "" {
		t.Error("Info().SessionID is empty")
	}
}

func TestNewMockProvider_WithConfig(t *testing.T) {
	cfg := &ProviderConfig{
		CDPEndpoint: "ws://custom:9222/devtools/browser/test",
		BufferSize:  20,
	}

	p := NewMockProvider(cfg)

	// Buffer should use custom size
	if p.Buffer().Cap() != 20 {
		t.Errorf("Buffer.Cap() = %d, want 20", p.Buffer().Cap())
	}
}

func TestNewMockProvider_WithOptions(t *testing.T) {
	browserInfo := &BrowserInfo{
		Product:   "Custom/1.0",
		UserAgent: "CustomAgent",
	}

	tabs := []*TabInfo{
		{ID: "tab-1", Title: "Tab 1"},
		{ID: "tab-2", Title: "Tab 2"},
	}

	p := NewMockProvider(nil,
		WithMockBrowserInfo(browserInfo),
		WithMockTabs(tabs),
	)

	info, err := p.GetBrowserInfo(context.Background())
	if err != nil {
		t.Fatalf("GetBrowserInfo() error = %v", err)
	}
	if info.Product != "Custom/1.0" {
		t.Errorf("BrowserInfo.Product = %q, want %q", info.Product, "Custom/1.0")
	}

	gotTabs, err := p.ListTabs(context.Background())
	if err != nil {
		t.Fatalf("ListTabs() error = %v", err)
	}
	if len(gotTabs) != 2 {
		t.Errorf("ListTabs() returned %d tabs, want 2", len(gotTabs))
	}
}

func TestMockProvider_Start(t *testing.T) {
	p := NewMockProvider(nil)

	opts := &StreamOptions{Quality: 90}
	err := p.Start(context.Background(), opts)
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if p.State() != StreamStateActive {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateActive)
	}

	info := p.Info()
	if info.StartedAt == nil {
		t.Error("StartedAt should be set")
	}
	if info.Options == nil {
		t.Error("Options should be set")
	}
	if info.Options.Quality != 90 {
		t.Errorf("Options.Quality = %d, want 90", info.Options.Quality)
	}
}

func TestMockProvider_Start_AlreadyActive(t *testing.T) {
	p := NewMockProvider(nil)

	p.Start(context.Background(), nil)
	err := p.Start(context.Background(), nil)

	if err != ErrStreamAlreadyActive {
		t.Errorf("Start() error = %v, want ErrStreamAlreadyActive", err)
	}
}

func TestMockProvider_Start_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	err := p.Start(context.Background(), nil)
	if err != ErrProviderClosed {
		t.Errorf("Start() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_Start_InvalidOptions(t *testing.T) {
	p := NewMockProvider(nil)

	opts := &StreamOptions{Quality: -1}
	err := p.Start(context.Background(), opts)
	if err != ErrInvalidQuality {
		t.Errorf("Start() error = %v, want ErrInvalidQuality", err)
	}
}

func TestMockProvider_Start_WithHook(t *testing.T) {
	hookCalled := false
	hookErr := errors.New("hook error")

	p := NewMockProvider(nil,
		WithMockOnStart(func(ctx context.Context, opts *StreamOptions) error {
			hookCalled = true
			return hookErr
		}),
	)

	err := p.Start(context.Background(), nil)
	if !hookCalled {
		t.Error("onStart hook was not called")
	}
	if err != hookErr {
		t.Errorf("Start() error = %v, want %v", err, hookErr)
	}
}

func TestMockProvider_Stop(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)

	err := p.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	if p.State() != StreamStateStopped {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateStopped)
	}

	info := p.Info()
	if info.StoppedAt == nil {
		t.Error("StoppedAt should be set")
	}
}

func TestMockProvider_Stop_AlreadyStopped(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)
	p.Stop(context.Background())

	// Second stop should be no-op
	err := p.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() second call error = %v", err)
	}
}

func TestMockProvider_Stop_Idle(t *testing.T) {
	p := NewMockProvider(nil)

	// Stop when idle should be no-op
	err := p.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestMockProvider_Stop_WithHook(t *testing.T) {
	hookCalled := false
	hookErr := errors.New("hook error")

	p := NewMockProvider(nil,
		WithMockOnStop(func(ctx context.Context) error {
			hookCalled = true
			return hookErr
		}),
	)

	p.Start(context.Background(), nil)
	err := p.Stop(context.Background())

	if !hookCalled {
		t.Error("onStop hook was not called")
	}
	if err != hookErr {
		t.Errorf("Stop() error = %v, want %v", err, hookErr)
	}
}

func TestMockProvider_Pause(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)

	err := p.Pause(context.Background())
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}

	if p.State() != StreamStatePaused {
		t.Errorf("State() = %v, want %v", p.State(), StreamStatePaused)
	}
}

func TestMockProvider_Pause_NotActive(t *testing.T) {
	p := NewMockProvider(nil)

	err := p.Pause(context.Background())
	if err != ErrStreamNotActive {
		t.Errorf("Pause() error = %v, want ErrStreamNotActive", err)
	}
}

func TestMockProvider_Pause_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)
	p.Close()

	err := p.Pause(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("Pause() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_Resume(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)
	p.Pause(context.Background())

	err := p.Resume(context.Background())
	if err != nil {
		t.Errorf("Resume() error = %v", err)
	}

	if p.State() != StreamStateActive {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateActive)
	}
}

func TestMockProvider_Resume_NotPaused(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)

	err := p.Resume(context.Background())
	if err != ErrStreamNotActive {
		t.Errorf("Resume() error = %v, want ErrStreamNotActive", err)
	}
}

func TestMockProvider_Resume_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)
	p.Pause(context.Background())
	p.Close()

	err := p.Resume(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("Resume() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_Frames(t *testing.T) {
	p := NewMockProvider(nil)

	// Push frames
	p.PushFrame(&Frame{Sequence: 1})
	p.PushFrame(&Frame{Sequence: 2})

	ch := p.Frames()
	frame1 := <-ch
	frame2 := <-ch

	if frame1.Sequence != 1 || frame2.Sequence != 2 {
		t.Error("frames received in wrong order")
	}
}

func TestMockProvider_PushFrame_AutoSequence(t *testing.T) {
	p := NewMockProvider(nil)

	// Push without sequence
	p.PushFrame(&Frame{})
	p.PushFrame(&Frame{})
	p.PushFrame(&Frame{})

	ch := p.Frames()
	frame1 := <-ch
	frame2 := <-ch
	frame3 := <-ch

	if frame1.Sequence != 1 || frame2.Sequence != 2 || frame3.Sequence != 3 {
		t.Errorf("auto-sequence failed: got %d, %d, %d", frame1.Sequence, frame2.Sequence, frame3.Sequence)
	}
}

func TestMockProvider_SendInput(t *testing.T) {
	p := NewMockProvider(nil)

	event := &InputEvent{
		Type: InputEventMouseMove,
		Mouse: &MouseEvent{
			X: 100,
			Y: 200,
		},
	}

	err := p.SendInput(context.Background(), event)
	if err != nil {
		t.Errorf("SendInput() error = %v", err)
	}
}

func TestMockProvider_SendInput_NilEvent(t *testing.T) {
	p := NewMockProvider(nil)

	err := p.SendInput(context.Background(), nil)
	if err != ErrInvalidInputEvent {
		t.Errorf("SendInput() error = %v, want ErrInvalidInputEvent", err)
	}
}

func TestMockProvider_SendInput_InvalidType(t *testing.T) {
	p := NewMockProvider(nil)

	event := &InputEvent{Type: "invalid"}
	err := p.SendInput(context.Background(), event)
	if err != ErrInvalidInputEvent {
		t.Errorf("SendInput() error = %v, want ErrInvalidInputEvent", err)
	}
}

func TestMockProvider_SendInput_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	event := &InputEvent{Type: InputEventMouseMove}
	err := p.SendInput(context.Background(), event)
	if err != ErrProviderClosed {
		t.Errorf("SendInput() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_SendInput_WithHook(t *testing.T) {
	var receivedEvent *InputEvent
	hookErr := errors.New("hook error")

	p := NewMockProvider(nil,
		WithMockOnInput(func(ctx context.Context, event *InputEvent) error {
			receivedEvent = event
			return hookErr
		}),
	)

	event := &InputEvent{Type: InputEventKeyDown}
	err := p.SendInput(context.Background(), event)

	if receivedEvent != event {
		t.Error("hook did not receive correct event")
	}
	if err != hookErr {
		t.Errorf("SendInput() error = %v, want %v", err, hookErr)
	}
}

func TestMockProvider_Navigate(t *testing.T) {
	p := NewMockProvider(nil)

	req := &NavigateRequest{URL: "https://example.com"}
	err := p.Navigate(context.Background(), req)
	if err != nil {
		t.Errorf("Navigate() error = %v", err)
	}
}

func TestMockProvider_Navigate_EmptyURL(t *testing.T) {
	p := NewMockProvider(nil)

	req := &NavigateRequest{URL: ""}
	err := p.Navigate(context.Background(), req)
	if err != ErrEmptyURL {
		t.Errorf("Navigate() error = %v, want ErrEmptyURL", err)
	}
}

func TestMockProvider_Navigate_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	req := &NavigateRequest{URL: "https://example.com"}
	err := p.Navigate(context.Background(), req)
	if err != ErrProviderClosed {
		t.Errorf("Navigate() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_Navigate_WithHook(t *testing.T) {
	var receivedReq *NavigateRequest
	hookErr := errors.New("hook error")

	p := NewMockProvider(nil,
		WithMockOnNavigate(func(ctx context.Context, req *NavigateRequest) error {
			receivedReq = req
			return hookErr
		}),
	)

	req := &NavigateRequest{URL: "https://example.com"}
	err := p.Navigate(context.Background(), req)

	if receivedReq != req {
		t.Error("hook did not receive correct request")
	}
	if err != hookErr {
		t.Errorf("Navigate() error = %v, want %v", err, hookErr)
	}
}

func TestMockProvider_GetBrowserInfo(t *testing.T) {
	p := NewMockProvider(nil)

	info, err := p.GetBrowserInfo(context.Background())
	if err != nil {
		t.Errorf("GetBrowserInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("GetBrowserInfo() returned nil")
	}
	if info.Product == "" {
		t.Error("BrowserInfo.Product is empty")
	}
}

func TestMockProvider_GetBrowserInfo_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	_, err := p.GetBrowserInfo(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("GetBrowserInfo() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_GetBrowserInfo_NilInfo(t *testing.T) {
	p := NewMockProvider(nil)
	p.SetBrowserInfo(nil)

	_, err := p.GetBrowserInfo(context.Background())
	if err != ErrBrowserNotConnected {
		t.Errorf("GetBrowserInfo() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestMockProvider_ListTabs(t *testing.T) {
	p := NewMockProvider(nil)

	tabs, err := p.ListTabs(context.Background())
	if err != nil {
		t.Errorf("ListTabs() error = %v", err)
	}
	if len(tabs) == 0 {
		t.Error("ListTabs() returned empty list")
	}
}

func TestMockProvider_ListTabs_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	_, err := p.ListTabs(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("ListTabs() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_SwitchTab(t *testing.T) {
	p := NewMockProvider(nil)

	// Get existing tab ID
	tabs, _ := p.ListTabs(context.Background())
	if len(tabs) == 0 {
		t.Skip("no tabs available")
	}

	err := p.SwitchTab(context.Background(), tabs[0].ID)
	if err != nil {
		t.Errorf("SwitchTab() error = %v", err)
	}
}

func TestMockProvider_SwitchTab_NotFound(t *testing.T) {
	p := NewMockProvider(nil)

	err := p.SwitchTab(context.Background(), "nonexistent-tab")
	if err != ErrStreamNotFound {
		t.Errorf("SwitchTab() error = %v, want ErrStreamNotFound", err)
	}
}

func TestMockProvider_SwitchTab_Closed(t *testing.T) {
	p := NewMockProvider(nil)
	p.Close()

	err := p.SwitchTab(context.Background(), "any-tab")
	if err != ErrProviderClosed {
		t.Errorf("SwitchTab() error = %v, want ErrProviderClosed", err)
	}
}

func TestMockProvider_OnStateChange(t *testing.T) {
	p := NewMockProvider(nil)

	var stateChanges []struct {
		old, new StreamState
	}
	var mu sync.Mutex

	p.OnStateChange(func(ctx context.Context, oldState, newState StreamState, err error) {
		mu.Lock()
		stateChanges = append(stateChanges, struct {
			old, new StreamState
		}{oldState, newState})
		mu.Unlock()
	})

	p.Start(context.Background(), nil)
	p.Pause(context.Background())
	p.Resume(context.Background())
	p.Stop(context.Background())

	// Give time for async callbacks
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Expected transitions:
	// idle -> starting -> active (from Start)
	// active -> paused (from Pause)
	// paused -> active (from Resume)
	// active -> stopping -> stopped (from Stop)
	expectedCount := 6
	if len(stateChanges) != expectedCount {
		t.Errorf("got %d state changes, want %d", len(stateChanges), expectedCount)
	}
}

func TestMockProvider_Close(t *testing.T) {
	p := NewMockProvider(nil)
	p.Start(context.Background(), nil)

	err := p.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Should be stopped after close
	if p.State() != StreamStateStopped {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateStopped)
	}

	// Buffer should be closed
	if !p.Buffer().IsClosed() {
		t.Error("Buffer should be closed")
	}
}

func TestMockProvider_Close_Multiple(t *testing.T) {
	p := NewMockProvider(nil)

	// Multiple closes should be safe
	p.Close()
	err := p.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestMockProvider_SetState(t *testing.T) {
	p := NewMockProvider(nil)

	p.SetState(StreamStateActive)
	if p.State() != StreamStateActive {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateActive)
	}
}

func TestMockProvider_SetError(t *testing.T) {
	p := NewMockProvider(nil)

	p.SetError("test error")
	if p.State() != StreamStateError {
		t.Errorf("State() = %v, want %v", p.State(), StreamStateError)
	}

	info := p.Info()
	if info.Error != "test error" {
		t.Errorf("Error = %q, want %q", info.Error, "test error")
	}
}

func TestMockProvider_Stats(t *testing.T) {
	p := NewMockProvider(nil)

	p.PushFrame(&Frame{})
	p.PushFrame(&Frame{})

	// FramesSent = TotalFrames - DroppedFrames (frames successfully queued)
	// Both frames were pushed successfully with no drops
	stats := p.Stats()
	if stats.FramesSent != 2 {
		t.Errorf("FramesSent = %d, want 2", stats.FramesSent)
	}
}

func TestMockProviderFactory(t *testing.T) {
	factory := NewMockProviderFactory()

	if factory.Name() != "mock" {
		t.Errorf("Name() = %q, want %q", factory.Name(), "mock")
	}

	cfg := &ProviderConfig{
		CDPEndpoint: "ws://localhost:9222/devtools/browser/test",
		BufferSize:  15,
	}

	provider, err := factory.Create(cfg)
	if err != nil {
		t.Errorf("Create() error = %v", err)
	}
	if provider == nil {
		t.Fatal("Create() returned nil provider")
	}

	// Verify provider was tracked
	providers := factory.Providers()
	if len(providers) != 1 {
		t.Errorf("Providers() len = %d, want 1", len(providers))
	}
}

func TestMockProviderFactory_InvalidConfig(t *testing.T) {
	factory := NewMockProviderFactory()

	cfg := &ProviderConfig{CDPEndpoint: ""} // Invalid: empty endpoint

	_, err := factory.Create(cfg)
	if err == nil {
		t.Error("Create() should return error for invalid config")
	}
}

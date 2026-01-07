package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func TestModifiersToInt(t *testing.T) {
	tests := []struct {
		name string
		m    *Modifiers
		want int
	}{
		{
			name: "nil modifiers",
			m:    nil,
			want: 0,
		},
		{
			name: "no modifiers",
			m:    &Modifiers{},
			want: 0,
		},
		{
			name: "alt only",
			m:    &Modifiers{Alt: true},
			want: 1,
		},
		{
			name: "ctrl only",
			m:    &Modifiers{Ctrl: true},
			want: 2,
		},
		{
			name: "meta only",
			m:    &Modifiers{Meta: true},
			want: 4,
		},
		{
			name: "shift only",
			m:    &Modifiers{Shift: true},
			want: 8,
		},
		{
			name: "all modifiers",
			m:    &Modifiers{Alt: true, Ctrl: true, Meta: true, Shift: true},
			want: 15,
		},
		{
			name: "ctrl+shift",
			m:    &Modifiers{Ctrl: true, Shift: true},
			want: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modifiersToInt(tt.m)
			if got != tt.want {
				t.Errorf("modifiersToInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewCDPProvider(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *CDPProviderConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: false,
		},
		{
			name:    "empty config",
			cfg:     &CDPProviderConfig{},
			wantErr: false,
		},
		{
			name: "with chrome config",
			cfg: &CDPProviderConfig{
				ChromeConfig: &ChromeConfig{
					Headless: true,
				},
			},
			wantErr: false,
		},
		{
			name: "with provider config",
			cfg: &CDPProviderConfig{
				ProviderConfig: &ProviderConfig{
					BufferSize: 20,
				},
			},
			wantErr: false,
		},
		{
			name: "with logger",
			cfg: &CDPProviderConfig{
				Logger: zap.NewNop(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewCDPProvider(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCDPProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if p == nil {
					t.Error("NewCDPProvider() returned nil provider")
					return
				}
				if p.State() != StreamStateIdle {
					t.Errorf("initial state = %v, want %v", p.State(), StreamStateIdle)
				}
				if p.buffer == nil {
					t.Error("buffer is nil")
				}
				if p.chrome == nil {
					t.Error("chrome is nil")
				}
			}
		})
	}
}

func TestCDPProvider_Info(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	info := p.Info()
	if info == nil {
		t.Fatal("Info() returned nil")
	}
	if info.State != StreamStateIdle {
		t.Errorf("State = %v, want %v", info.State, StreamStateIdle)
	}
	if info.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if !strings.HasPrefix(info.ID, "cdp-") {
		t.Errorf("ID = %v, want prefix 'cdp-'", info.ID)
	}
}

func TestCDPProvider_Stats(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	stats := p.Stats()
	if stats == nil {
		t.Fatal("Stats() returned nil")
	}
	if stats.FramesSent != 0 {
		t.Errorf("FramesSent = %d, want 0", stats.FramesSent)
	}
	if stats.FramesDropped != 0 {
		t.Errorf("FramesDropped = %d, want 0", stats.FramesDropped)
	}
}

func TestCDPProvider_Close(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	err = p.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Double close should be safe
	err = p.Close()
	if err != nil {
		t.Errorf("second Close() error = %v", err)
	}
}

func TestCDPProvider_Start_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.Start(context.Background(), nil)
	if err != ErrProviderClosed {
		t.Errorf("Start() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_OnStateChange(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	var mu sync.Mutex
	var stateChanges []struct {
		oldState StreamState
		newState StreamState
	}

	p.OnStateChange(func(ctx context.Context, oldState, newState StreamState, err error) {
		mu.Lock()
		defer mu.Unlock()
		stateChanges = append(stateChanges, struct {
			oldState StreamState
			newState StreamState
		}{oldState, newState})
	})

	// Trigger state change by calling setState directly
	p.setState(StreamStateError)

	mu.Lock()
	changes := stateChanges
	mu.Unlock()

	if len(changes) != 1 {
		t.Errorf("expected 1 state change, got %d", len(changes))
	}
	if len(changes) > 0 {
		if changes[0].oldState != StreamStateIdle {
			t.Errorf("oldState = %v, want %v", changes[0].oldState, StreamStateIdle)
		}
		if changes[0].newState != StreamStateError {
			t.Errorf("newState = %v, want %v", changes[0].newState, StreamStateError)
		}
	}
}

func TestCDPProvider_SendInput_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.SendInput(context.Background(), &InputEvent{Type: InputEventMouseMove})
	if err != ErrProviderClosed {
		t.Errorf("SendInput() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_SendInput_NilEvent(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	err = p.SendInput(context.Background(), nil)
	if err != ErrInvalidInputEvent {
		t.Errorf("SendInput(nil) error = %v, want ErrInvalidInputEvent", err)
	}
}

func TestCDPProvider_SendInput_InvalidType(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	err = p.SendInput(context.Background(), &InputEvent{Type: "invalid"})
	if err != ErrInvalidInputEvent {
		t.Errorf("SendInput(invalid type) error = %v, want ErrInvalidInputEvent", err)
	}
}

func TestCDPProvider_SendInput_NotConnected(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	// CDP is nil, should return ErrBrowserNotConnected
	err = p.SendInput(context.Background(), &InputEvent{
		Type:  InputEventMouseMove,
		Mouse: &MouseEvent{X: 100, Y: 100},
	})
	if err != ErrBrowserNotConnected {
		t.Errorf("SendInput() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestCDPProvider_Navigate_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.Navigate(context.Background(), &NavigateRequest{URL: "https://example.com"})
	if err != ErrProviderClosed {
		t.Errorf("Navigate() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_Navigate_InvalidRequest(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	err = p.Navigate(context.Background(), &NavigateRequest{URL: ""})
	if err == nil {
		t.Error("Navigate() with empty URL should return error")
	}
}

func TestCDPProvider_Navigate_NotConnected(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	err = p.Navigate(context.Background(), &NavigateRequest{URL: "https://example.com"})
	if err != ErrBrowserNotConnected {
		t.Errorf("Navigate() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestCDPProvider_GetBrowserInfo_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	_, err = p.GetBrowserInfo(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("GetBrowserInfo() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_GetBrowserInfo_NotConnected(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	_, err = p.GetBrowserInfo(context.Background())
	if err != ErrBrowserNotConnected {
		t.Errorf("GetBrowserInfo() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestCDPProvider_ListTabs_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	_, err = p.ListTabs(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("ListTabs() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_ListTabs_NotConnected(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	_, err = p.ListTabs(context.Background())
	if err != ErrBrowserNotConnected {
		t.Errorf("ListTabs() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestCDPProvider_SwitchTab_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.SwitchTab(context.Background(), "tab-1")
	if err != ErrProviderClosed {
		t.Errorf("SwitchTab() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_SwitchTab_NotConnected(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	err = p.SwitchTab(context.Background(), "tab-1")
	if err != ErrBrowserNotConnected {
		t.Errorf("SwitchTab() error = %v, want ErrBrowserNotConnected", err)
	}
}

func TestCDPProvider_Pause_NotActive(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	// State is Idle, should fail
	err = p.Pause(context.Background())
	if err != ErrStreamNotActive {
		t.Errorf("Pause() on idle provider error = %v, want ErrStreamNotActive", err)
	}
}

func TestCDPProvider_Pause_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.Pause(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("Pause() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_Resume_NotPaused(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	// State is Idle, should fail
	err = p.Resume(context.Background())
	if err != ErrStreamNotActive {
		t.Errorf("Resume() on idle provider error = %v, want ErrStreamNotActive", err)
	}
}

func TestCDPProvider_Resume_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	err = p.Resume(context.Background())
	if err != ErrProviderClosed {
		t.Errorf("Resume() on closed provider error = %v, want ErrProviderClosed", err)
	}
}

func TestCDPProvider_Stop_Idle(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	// Stop on idle state should succeed (no-op)
	err = p.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() on idle provider error = %v", err)
	}
}

func TestCDPProvider_Stop_Closed(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}

	_ = p.Close()

	// Stop on closed should succeed (no-op)
	err = p.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() on closed provider error = %v", err)
	}
}

func TestCDPProvider_Frames(t *testing.T) {
	p, err := NewCDPProvider(nil)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	ch := p.Frames()
	if ch == nil {
		t.Error("Frames() returned nil channel")
	}
}

// CDPProviderFactory tests

func TestNewCDPProviderFactory(t *testing.T) {
	f := NewCDPProviderFactory()
	if f == nil {
		t.Fatal("NewCDPProviderFactory() returned nil")
	}
	if f.Name() != "cdp" {
		t.Errorf("Name() = %v, want 'cdp'", f.Name())
	}
}

func TestNewCDPProviderFactory_WithOptions(t *testing.T) {
	logger := zap.NewNop()
	chromeConfig := &ChromeConfig{Headless: false}

	f := NewCDPProviderFactory(
		WithLogger(logger),
		WithChromeConfig(chromeConfig),
	)

	if f == nil {
		t.Fatal("NewCDPProviderFactory() returned nil")
	}
	if f.logger != logger {
		t.Error("logger not set")
	}
	if f.chromeConfig != chromeConfig {
		t.Error("chromeConfig not set")
	}
}

func TestCDPProviderFactory_Create(t *testing.T) {
	f := NewCDPProviderFactory()

	// CDPEndpoint is required by ProviderConfig.Validate()
	// but CDPProvider will overwrite it with the actual Chrome endpoint
	cfg := &ProviderConfig{
		BufferSize:  10,
		CDPEndpoint: "ws://placeholder:9222/devtools/browser/placeholder",
	}

	provider, err := f.Create(cfg)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if provider == nil {
		t.Fatal("Create() returned nil provider")
	}
	defer func() { _ = provider.Close() }()

	if provider.State() != StreamStateIdle {
		t.Errorf("initial state = %v, want %v", provider.State(), StreamStateIdle)
	}
}

func TestCDPProviderFactory_Create_InvalidConfig(t *testing.T) {
	f := NewCDPProviderFactory()

	cfg := &ProviderConfig{
		BufferSize: -1, // Invalid
	}

	_, err := f.Create(cfg)
	if err == nil {
		t.Error("Create() with invalid config should return error")
	}
}

// Mock CDP server for connection tests

type mockCDPServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader
	messages []json.RawMessage
	mu       sync.Mutex
}

func newMockCDPServer() *mockCDPServer {
	m := &mockCDPServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := m.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}

			m.mu.Lock()
			m.messages = append(m.messages, msg)
			m.mu.Unlock()

			// Parse request and send response
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if err := json.Unmarshal(msg, &req); err != nil {
				continue
			}

			// Send generic success response
			resp := map[string]interface{}{
				"id":     req.ID,
				"result": map[string]interface{}{},
			}

			// Add specific responses for certain methods
			switch req.Method {
			case "Browser.getVersion":
				resp["result"] = map[string]interface{}{
					"product":         "Chrome/100.0.0.0",
					"protocolVersion": "1.3",
					"userAgent":       "Mozilla/5.0",
				}
			case "Target.getTargets":
				resp["result"] = map[string]interface{}{
					"targetInfos": []map[string]interface{}{
						{
							"targetId": "target-1",
							"type":     "page",
							"title":    "Test Page",
							"url":      "https://example.com",
							"attached": true,
						},
					},
				}
			case "Page.navigate":
				resp["result"] = map[string]interface{}{
					"frameId":  "frame-1",
					"loaderId": "loader-1",
				}
			}

			respBytes, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, respBytes)
		}
	}))

	return m
}

func (m *mockCDPServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockCDPServer) Close() {
	m.server.Close()
}

func TestCDPConnection_WithMockServer(t *testing.T) {
	server := newMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Test PageEnable
	err = conn.PageEnable(ctx)
	if err != nil {
		t.Errorf("PageEnable() error = %v", err)
	}

	// Test BrowserGetVersion
	info, err := conn.BrowserGetVersion(ctx)
	if err != nil {
		t.Errorf("BrowserGetVersion() error = %v", err)
	}
	if info == nil {
		t.Error("BrowserGetVersion() returned nil")
	} else if info.Product != "Chrome/100.0.0.0" {
		t.Errorf("Product = %v, want Chrome/100.0.0.0", info.Product)
	}

	// Test TargetGetTargets
	targets, err := conn.TargetGetTargets(ctx)
	if err != nil {
		t.Errorf("TargetGetTargets() error = %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("TargetGetTargets() returned %d targets, want 1", len(targets))
	}

	// Test PageNavigate
	_, err = conn.PageNavigate(ctx, "https://example.com", "")
	if err != nil {
		t.Errorf("PageNavigate() error = %v", err)
	}
}

func TestCDPConnection_Timeout(t *testing.T) {
	// Create a server that never responds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Just read and never respond
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn := NewCDPConnection(wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Send command with very short timeout - should timeout
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer shortCancel()

	err = conn.PageEnable(shortCtx)
	if err == nil {
		t.Error("PageEnable() should timeout")
	}
}

// Integration tests (require Chrome)

func TestCDPProvider_Integration(t *testing.T) {
	if os.Getenv("CHROME_PATH") == "" && os.Getenv("TEST_INTEGRATION") == "" {
		t.Skip("Skipping integration test: set CHROME_PATH or TEST_INTEGRATION=1")
	}

	cfg := &CDPProviderConfig{
		ChromeConfig: &ChromeConfig{
			Headless: true,
		},
		Logger: zap.NewNop(),
	}

	p, err := NewCDPProvider(cfg)
	if err != nil {
		t.Fatalf("NewCDPProvider() error = %v", err)
	}
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start streaming
	err = p.Start(ctx, &StreamOptions{
		Format:  FormatJPEG,
		Quality: 50,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if p.State() != StreamStateActive {
		t.Errorf("State = %v, want %v", p.State(), StreamStateActive)
	}

	// Get browser info
	info, err := p.GetBrowserInfo(ctx)
	if err != nil {
		t.Errorf("GetBrowserInfo() error = %v", err)
	} else {
		t.Logf("Browser: %s", info.Product)
	}

	// Navigate
	err = p.Navigate(ctx, &NavigateRequest{URL: "https://example.com"})
	if err != nil {
		t.Errorf("Navigate() error = %v", err)
	}

	// Wait for some frames
	framesCh := p.Frames()
	received := 0
	timeout := time.After(5 * time.Second)
	for received < 3 {
		select {
		case frame := <-framesCh:
			if frame != nil {
				received++
				t.Logf("Received frame %d: %dx%d, %d bytes", frame.Sequence, frame.Width, frame.Height, len(frame.Data))
			}
		case <-timeout:
			t.Log("Timeout waiting for frames")
			goto done
		}
	}
done:

	if received == 0 {
		t.Error("No frames received")
	}

	// Test pause/resume
	err = p.Pause(ctx)
	if err != nil {
		t.Errorf("Pause() error = %v", err)
	}
	if p.State() != StreamStatePaused {
		t.Errorf("State after pause = %v, want %v", p.State(), StreamStatePaused)
	}

	err = p.Resume(ctx)
	if err != nil {
		t.Errorf("Resume() error = %v", err)
	}
	if p.State() != StreamStateActive {
		t.Errorf("State after resume = %v, want %v", p.State(), StreamStateActive)
	}

	// Stop
	err = p.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
	if p.State() != StreamStateStopped {
		t.Errorf("State after stop = %v, want %v", p.State(), StreamStateStopped)
	}

	// Check stats
	stats := p.Stats()
	t.Logf("Stats: sent=%d, dropped=%d, fps=%.2f", stats.FramesSent, stats.FramesDropped, stats.AverageFPS)
}

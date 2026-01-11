package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming/browser"
)

// mockBrowserStreamService implements BrowserStreamService for testing.
type mockBrowserStreamService struct {
	frameHub     *browser.FrameHub
	validateErr  error
	validateFunc func(ctx context.Context, streamID, token string) error
}

func (m *mockBrowserStreamService) GetFrameHub() *browser.FrameHub {
	return m.frameHub
}

func (m *mockBrowserStreamService) ValidateStreamAccess(ctx context.Context, streamID, token string) error {
	if m.validateFunc != nil {
		return m.validateFunc(ctx, streamID, token)
	}
	return m.validateErr
}

func newMockBrowserStreamService(logger *zap.Logger) *mockBrowserStreamService {
	return &mockBrowserStreamService{
		frameHub: browser.NewFrameHub(logger),
	}
}

func TestHandleBrowserStreamWS_NotConfigured(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create server without browser stream service
	srv := New(Config{Host: "localhost", Port: 8080}, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/api/v1/streams/bstr_test123/ws", nil)
	w := httptest.NewRecorder()

	// Call handler directly
	srv.handleBrowserStreamWS(w, req)

	// Should return 501 Not Implemented
	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.Contains(t, w.Body.String(), "browser streaming not configured")
}

func TestHandleBrowserStreamWS_MissingStreamID(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockService := newMockBrowserStreamService(logger)
	srv := New(
		Config{Host: "localhost", Port: 8080},
		logger,
		WithBrowserStreamService(mockService),
	)

	// Create test request without stream ID
	req := httptest.NewRequest("GET", "/api/v1/streams//ws", nil)
	w := httptest.NewRecorder()

	srv.handleBrowserStreamWS(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "stream ID is required")
}

func TestHandleBrowserStreamWS_AccessDenied(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockService := newMockBrowserStreamService(logger)
	mockService.validateErr = assert.AnError

	srv := New(
		Config{Host: "localhost", Port: 8080},
		logger,
		WithBrowserStreamService(mockService),
	)

	// Create router with URL parameter
	r := chi.NewRouter()
	r.Get("/api/v1/streams/{streamID}/ws", srv.handleBrowserStreamWS)

	// Create test server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Make request
	resp, err := http.Get(ts.URL + "/api/v1/streams/bstr_test123/ws?token=invalid")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestHandleBrowserStreamWS_WebSocketUpgrade(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockService := newMockBrowserStreamService(logger)
	mockService.validateFunc = func(ctx context.Context, streamID, token string) error {
		assert.Equal(t, "bstr_test123", streamID)
		assert.Equal(t, "validtoken", token)
		return nil
	}

	srv := New(
		Config{Host: "localhost", Port: 8080},
		logger,
		WithBrowserStreamService(mockService),
	)

	// Create router with URL parameter
	r := chi.NewRouter()
	r.Get("/api/v1/streams/{streamID}/ws", srv.handleBrowserStreamWS)

	// Create test server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Convert HTTP URL to WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/streams/bstr_test123/ws?token=validtoken"

	// Connect WebSocket
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func TestHandleBrowserStreamWS_ReceiveFrames(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockService := newMockBrowserStreamService(logger)
	streamID := "bstr_test123"

	srv := New(
		Config{Host: "localhost", Port: 8080},
		logger,
		WithBrowserStreamService(mockService),
	)

	// Create router with URL parameter
	r := chi.NewRouter()
	r.Get("/api/v1/streams/{streamID}/ws", srv.handleBrowserStreamWS)

	// Create test server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Register a mock stream in FrameHub
	_, _ = mockService.frameHub.RegisterStream(streamID, "run_123", "sess_123", nil, nil)
	defer mockService.frameHub.UnregisterStream(streamID)

	// Connect WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/streams/" + streamID + "/ws?token=test"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Wait for subscriber to be registered
	time.Sleep(50 * time.Millisecond)

	// Broadcast a frame
	mockService.frameHub.BroadcastFrame(streamID, &pb.BrowserFrame{
		Data:     []byte("test-frame"),
		Format:   "jpeg",
		Width:    1920,
		Height:   1080,
		Sequence: 1,
	})

	// Read message from WebSocket with timeout
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	require.NoError(t, err)

	// Parse message
	var msg BrowserFrameMessage
	err = json.Unmarshal(data, &msg)
	require.NoError(t, err)

	assert.Equal(t, BrowserMsgTypeFrame, msg.Type)
	assert.Equal(t, []byte("test-frame"), msg.Data)
	assert.Equal(t, "jpeg", msg.Format)
	assert.Equal(t, int32(1920), msg.Width)
	assert.Equal(t, int32(1080), msg.Height)
	assert.Equal(t, uint64(1), msg.Sequence)
}

func TestHandleBrowserStreamWS_SendInput(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockService := newMockBrowserStreamService(logger)
	streamID := "bstr_test123"

	srv := New(
		Config{Host: "localhost", Port: 8080},
		logger,
		WithBrowserStreamService(mockService),
	)

	// Create router with URL parameter
	r := chi.NewRouter()
	r.Get("/api/v1/streams/{streamID}/ws", srv.handleBrowserStreamWS)

	// Create test server
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Track input events received
	inputReceived := make(chan *pb.BrowserInputEvent, 10)
	sendInput := func(event *pb.BrowserInputEvent) error {
		inputReceived <- event
		return nil
	}

	// Register stream with input handler
	_, _ = mockService.frameHub.RegisterStream(streamID, "run_123", "sess_123", sendInput, nil)
	defer mockService.frameHub.UnregisterStream(streamID)

	// Connect WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/streams/" + streamID + "/ws?token=test"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Send input event
	inputMsg := BrowserInputMessage{
		Type:  BrowserMsgTypeInput,
		Event: "mouseMove",
		Mouse: &MouseEventData{
			X: 100,
			Y: 200,
		},
	}
	err = ws.WriteJSON(inputMsg)
	require.NoError(t, err)

	// Wait for input to be processed
	select {
	case event := <-inputReceived:
		assert.Equal(t, "mouseMove", event.Type)
		mouse := event.GetMouse()
		require.NotNil(t, mouse)
		assert.Equal(t, float64(100), mouse.X)
		assert.Equal(t, float64(200), mouse.Y)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for input event")
	}
}

func TestConvertInputToProto_MouseEvents(t *testing.T) {
	tests := []struct {
		name      string
		input     *BrowserInputMessage
		wantType  string
		wantMouse bool
	}{
		{
			name: "mouseMove",
			input: &BrowserInputMessage{
				Event: "mouseMove",
				Mouse: &MouseEventData{X: 100, Y: 200},
			},
			wantType:  "mouseMove",
			wantMouse: true,
		},
		{
			name: "mouseDown",
			input: &BrowserInputMessage{
				Event: "mouseDown",
				Mouse: &MouseEventData{X: 50, Y: 75, Button: "left", ClickCount: 1},
			},
			wantType:  "mouseDown",
			wantMouse: true,
		},
		{
			name: "mouseWheel",
			input: &BrowserInputMessage{
				Event: "mouseWheel",
				Mouse: &MouseEventData{X: 100, Y: 100, DeltaY: -100},
			},
			wantType:  "mouseWheel",
			wantMouse: true,
		},
		{
			name: "mouseMove missing data",
			input: &BrowserInputMessage{
				Event: "mouseMove",
				Mouse: nil,
			},
			wantType:  "",
			wantMouse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInputToProto(tt.input)

			if tt.wantType == "" {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.wantType, result.Type)
			if tt.wantMouse {
				assert.NotNil(t, result.GetMouse())
			}
		})
	}
}

func TestConvertInputToProto_KeyboardEvents(t *testing.T) {
	tests := []struct {
		name         string
		input        *BrowserInputMessage
		wantType     string
		wantKeyboard bool
	}{
		{
			name: "keyDown",
			input: &BrowserInputMessage{
				Event:    "keyDown",
				Keyboard: &KeyEventData{Key: "a", Code: "KeyA"},
			},
			wantType:     "keyDown",
			wantKeyboard: true,
		},
		{
			name: "keyUp",
			input: &BrowserInputMessage{
				Event:    "keyUp",
				Keyboard: &KeyEventData{Key: "Enter", Code: "Enter"},
			},
			wantType:     "keyUp",
			wantKeyboard: true,
		},
		{
			name: "keyDown missing data",
			input: &BrowserInputMessage{
				Event:    "keyDown",
				Keyboard: nil,
			},
			wantType:     "",
			wantKeyboard: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInputToProto(tt.input)

			if tt.wantType == "" {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.wantType, result.Type)
			if tt.wantKeyboard {
				assert.NotNil(t, result.GetKeyboard())
			}
		})
	}
}

func TestConvertInputToProto_InvalidEvent(t *testing.T) {
	input := &BrowserInputMessage{
		Event: "invalidEvent",
	}

	result := convertInputToProto(input)
	assert.Nil(t, result)
}

func TestConvertModifiersToProto(t *testing.T) {
	tests := []struct {
		name  string
		input *ModifierData
		want  *pb.BrowserModifiers
	}{
		{
			name:  "nil modifiers",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty modifiers",
			input: &ModifierData{},
			want:  &pb.BrowserModifiers{},
		},
		{
			name:  "all modifiers",
			input: &ModifierData{Alt: true, Ctrl: true, Meta: true, Shift: true},
			want:  &pb.BrowserModifiers{Alt: true, Ctrl: true, Meta: true, Shift: true},
		},
		{
			name:  "partial modifiers",
			input: &ModifierData{Ctrl: true, Shift: true},
			want:  &pb.BrowserModifiers{Ctrl: true, Shift: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertModifiersToProto(tt.input)

			if tt.want == nil {
				assert.Nil(t, result)
				return
			}

			require.NotNil(t, result)
			assert.Equal(t, tt.want.Alt, result.Alt)
			assert.Equal(t, tt.want.Ctrl, result.Ctrl)
			assert.Equal(t, tt.want.Meta, result.Meta)
			assert.Equal(t, tt.want.Shift, result.Shift)
		})
	}
}

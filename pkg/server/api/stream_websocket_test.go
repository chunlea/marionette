package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/core"
)

// MockBrowserStreamService implements BrowserStreamService for testing.
type MockBrowserStreamService struct {
	mu          sync.Mutex
	subscribers map[string]*core.FrameSubscriber
	connected   map[string]bool
	tunnels     map[string]*core.TunnelInfo
	inputs      []*pb.BrowserInputEvent
	controls    []*pb.ServerBrowserMessage
}

func NewMockBrowserStreamService() *MockBrowserStreamService {
	return &MockBrowserStreamService{
		subscribers: make(map[string]*core.FrameSubscriber),
		connected:   make(map[string]bool),
		tunnels:     make(map[string]*core.TunnelInfo),
		inputs:      make([]*pb.BrowserInputEvent, 0),
		controls:    make([]*pb.ServerBrowserMessage, 0),
	}
}

func (m *MockBrowserStreamService) AddTunnel(token string, info *core.TunnelInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tunnels[token] = info
	m.connected[info.TunnelID] = true
}

func (m *MockBrowserStreamService) ValidateTunnelToken(_ context.Context, token string) (*core.TunnelInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.tunnels[token]; ok {
		return info, nil
	}
	return nil, core.ErrTunnelNotFound
}

func (m *MockBrowserStreamService) Subscribe(subscriber *core.FrameSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[subscriber.ID] = subscriber
}

func (m *MockBrowserStreamService) Unsubscribe(subscriber *core.FrameSubscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subscribers, subscriber.ID)
}

func (m *MockBrowserStreamService) SendInput(_ context.Context, _ string, event *pb.BrowserInputEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, event)
	return nil
}

func (m *MockBrowserStreamService) SendControl(_ context.Context, _ string, msg *pb.ServerBrowserMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.controls = append(m.controls, msg)
	return nil
}

func (m *MockBrowserStreamService) GetStats(tunnelID string) *core.FrameHubStats {
	return &core.FrameHubStats{
		TunnelID:        tunnelID,
		StreamConnected: true,
		FramesReceived:  100,
	}
}

func (m *MockBrowserStreamService) IsConnected(tunnelID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected[tunnelID]
}

func (m *MockBrowserStreamService) BroadcastFrame(tunnelID string, frame *pb.BrowserFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subscribers {
		if sub.TunnelID == tunnelID {
			select {
			case sub.FrameCh <- frame:
			default:
				// Channel full
			}
		}
	}
}

func (m *MockBrowserStreamService) GetInputs() []*pb.BrowserInputEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.BrowserInputEvent, len(m.inputs))
	copy(result, m.inputs)
	return result
}

func (m *MockBrowserStreamService) GetControls() []*pb.ServerBrowserMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.ServerBrowserMessage, len(m.controls))
	copy(result, m.controls)
	return result
}

func TestHandleBrowserStream_MissingToken(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	req := httptest.NewRequest("GET", "/streams/tun_123/connect", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "token is required")
}

func TestHandleBrowserStream_InvalidToken(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	req := httptest.NewRequest("GET", "/streams/tun_123/connect?token=invalid_token", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired token")
}

func TestHandleBrowserStream_TunnelIDMismatch(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_456",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	req := httptest.NewRequest("GET", "/streams/tun_123/connect?token=valid_token", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "tunnel ID mismatch")
}

func TestHandleBrowserStream_StreamNotConnected(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.tunnels["valid_token"] = &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	}
	// Not setting connected = true

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	req := httptest.NewRequest("GET", "/streams/tun_123/connect?token=valid_token", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "stream not connected")
}

func TestHandleBrowserStream_WebSocketConnection(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	server := httptest.NewServer(r)
	defer server.Close()

	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/streams/tun_123/connect?token=valid_token"

	// Connect WebSocket
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Wait a bit for subscription to register
	time.Sleep(50 * time.Millisecond)

	// Check subscriber was registered
	mockService.mu.Lock()
	assert.Equal(t, 1, len(mockService.subscribers))
	mockService.mu.Unlock()

	// Close connection
	_ = ws.Close()

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Check subscriber was unregistered
	mockService.mu.Lock()
	assert.Equal(t, 0, len(mockService.subscribers))
	mockService.mu.Unlock()
}

func TestHandleBrowserStream_ReceiveFrame(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	server := httptest.NewServer(r)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/streams/tun_123/connect?token=valid_token"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Wait for subscription
	time.Sleep(50 * time.Millisecond)

	// Send a frame from the "agent"
	testData := []byte("test-frame-data")
	mockService.BroadcastFrame("tun_123", &pb.BrowserFrame{
		Data:            testData,
		Format:          "jpeg",
		Width:           1920,
		Height:          1080,
		Sequence:        1,
		TimestampUnixMs: time.Now().UnixMilli(),
	})

	// Read the frame from WebSocket
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, message, err := ws.ReadMessage()
	require.NoError(t, err)

	var frameMsg WSFrameMessage
	err = json.Unmarshal(message, &frameMsg)
	require.NoError(t, err)

	assert.Equal(t, StreamMessageTypeFrame, frameMsg.Type)
	assert.Equal(t, "jpeg", frameMsg.Format)
	assert.Equal(t, 1920, frameMsg.Width)
	assert.Equal(t, 1080, frameMsg.Height)
	assert.Equal(t, uint64(1), frameMsg.Sequence)

	// Decode base64 data
	decoded, err := base64.StdEncoding.DecodeString(frameMsg.Data)
	require.NoError(t, err)
	assert.Equal(t, testData, decoded)
}

func TestHandleBrowserStream_SendInput(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	server := httptest.NewServer(r)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/streams/tun_123/connect?token=valid_token"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Wait for subscription
	time.Sleep(50 * time.Millisecond)

	// Send a mouse input
	inputMsg := WSInputMessage{
		Type: StreamMessageTypeInput,
		Event: WSInputEvent{
			EventType: "mouseDown",
			Mouse: &WSMouseEvent{
				X:      100,
				Y:      200,
				Button: "left",
			},
		},
	}

	err = ws.WriteJSON(inputMsg)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check input was received
	inputs := mockService.GetInputs()
	require.Len(t, inputs, 1)
	assert.Equal(t, "mouseDown", inputs[0].Type)
	assert.NotNil(t, inputs[0].GetMouse())
	assert.Equal(t, float64(100), inputs[0].GetMouse().X)
	assert.Equal(t, float64(200), inputs[0].GetMouse().Y)
	assert.Equal(t, "left", inputs[0].GetMouse().Button)
}

func TestHandleBrowserStream_SendControlPause(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	server := httptest.NewServer(r)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/streams/tun_123/connect?token=valid_token"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Wait for subscription
	time.Sleep(50 * time.Millisecond)

	// Send pause control
	ctrlMsg := WSControlMessage{
		Type:    StreamMessageTypeControl,
		Command: "pause",
	}

	err = ws.WriteJSON(ctrlMsg)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check control was received
	controls := mockService.GetControls()
	require.Len(t, controls, 1)
	assert.NotNil(t, controls[0].GetControl())
	assert.True(t, controls[0].GetControl().GetPause())
}

func TestHandleBrowserStream_SendControlNavigate(t *testing.T) {
	mockService := NewMockBrowserStreamService()
	mockService.AddTunnel("valid_token", &core.TunnelInfo{
		TunnelID:  "tun_123",
		SessionID: "sess_123",
		Type:      "browser",
	})

	srv := &Server{
		logger:        zap.NewNop(),
		browserStream: mockService,
	}

	r := chi.NewRouter()
	r.Get("/streams/{tunnelID}/connect", srv.handleBrowserStream)

	server := httptest.NewServer(r)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/streams/tun_123/connect?token=valid_token"

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = ws.Close() }()

	// Wait for subscription
	time.Sleep(50 * time.Millisecond)

	// Send navigate control
	ctrlMsg := WSControlMessage{
		Type:    StreamMessageTypeControl,
		Command: "navigate",
		Payload: json.RawMessage(`{"url":"https://example.com","referrer":"https://google.com"}`),
	}

	err = ws.WriteJSON(ctrlMsg)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check control was received
	controls := mockService.GetControls()
	require.Len(t, controls, 1)
	assert.NotNil(t, controls[0].GetControl())
	assert.NotNil(t, controls[0].GetControl().GetNavigate())
	assert.Equal(t, "https://example.com", controls[0].GetControl().GetNavigate().Url)
	assert.Equal(t, "https://google.com", controls[0].GetControl().GetNavigate().Referrer)
}

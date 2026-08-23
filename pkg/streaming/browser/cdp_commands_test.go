// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

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

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCDPError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *cdpError
		expected string
	}{
		{
			name:     "error without data",
			err:      &cdpError{Code: -32600, Message: "Invalid Request"},
			expected: "CDP error -32600: Invalid Request",
		},
		{
			name:     "error with data",
			err:      &cdpError{Code: -32601, Message: "Method not found", Data: "unknownMethod"},
			expected: "CDP error -32601: Method not found (unknownMethod)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.err.Error()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewCDPConnection(t *testing.T) {
	endpoint := "ws://localhost:9222/devtools/browser/abc123"
	conn := NewCDPConnection(endpoint)

	require.NotNil(t, conn)
	assert.Equal(t, endpoint, conn.endpoint)
	assert.NotNil(t, conn.pending)
	assert.NotNil(t, conn.eventHandlers)
	assert.NotNil(t, conn.closedCh)
	assert.False(t, conn.IsClosed())
}

func TestCDPConnection_IsClosed(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	assert.False(t, conn.IsClosed())

	_ = conn.Close()

	assert.True(t, conn.IsClosed())
}

func TestCDPConnection_Close_NotConnected(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	err := conn.Close()

	assert.NoError(t, err)
	assert.True(t, conn.IsClosed())
}

func TestCDPConnection_Close_Idempotent(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	err1 := conn.Close()
	err2 := conn.Close()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
}

func TestCDPConnection_OnEvent(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	handler := func(params json.RawMessage) {
		// Handler will be called when event is received
	}

	conn.OnEvent("Page.loadEventFired", handler)

	conn.eventHandlersMu.RLock()
	_, exists := conn.eventHandlers["Page.loadEventFired"]
	conn.eventHandlersMu.RUnlock()

	assert.True(t, exists)
}

func TestCDPConnection_Send_Closed(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")
	_ = conn.Close()

	ctx := context.Background()
	_, err := conn.Send(ctx, "Page.enable", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection closed")
}

func TestCDPConnection_SendWithSession_Closed(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")
	_ = conn.Close()

	ctx := context.Background()
	_, err := conn.SendWithSession(ctx, "session-123", "Page.enable", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection closed")
}

func TestCDPConnection_Send_NotConnected(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	ctx := context.Background()
	_, err := conn.Send(ctx, "Page.enable", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// extendedMockCDPServer is an enhanced mock server for testing additional commands.
type extendedMockCDPServer struct {
	server   *httptest.Server
	upgrader websocket.Upgrader
	messages []json.RawMessage
	mu       sync.Mutex
}

func newExtendedMockCDPServer() *extendedMockCDPServer {
	m := &extendedMockCDPServer{
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
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(msg, &req); err != nil {
				continue
			}

			// Send appropriate response based on method
			resp := map[string]interface{}{
				"id":     req.ID,
				"result": map[string]interface{}{},
			}

			switch req.Method {
			case "Page.startScreencast", "Page.stopScreencast", "Page.screencastFrameAck":
				// These return empty result
			case "Page.captureScreenshot":
				// Return base64 encoded image data
				imgData := base64.StdEncoding.EncodeToString([]byte("fake-image-data"))
				resp["result"] = map[string]interface{}{
					"data": imgData,
				}
			case "Input.dispatchMouseEvent", "Input.dispatchKeyEvent":
				// These return empty result
			case "Target.activateTarget", "Target.setDiscoverTargets":
				// These return empty result
			case "Emulation.setDeviceMetricsOverride":
				// Returns empty result
			case "Runtime.enable":
				// Returns empty result
			case "Page.navigate":
				resp["result"] = map[string]interface{}{
					"frameId":  "frame-123",
					"loaderId": "loader-456",
				}
			}

			respBytes, _ := json.Marshal(resp)
			_ = conn.WriteMessage(websocket.TextMessage, respBytes)
		}
	}))

	return m
}

func (m *extendedMockCDPServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *extendedMockCDPServer) Close() {
	m.server.Close()
}

func TestCDPConnection_PageStartScreencast(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Test with nil params
	err = conn.PageStartScreencast(ctx, nil)
	assert.NoError(t, err)

	// Test with custom params
	err = conn.PageStartScreencast(ctx, &ScreencastFrameParams{
		Format:        "jpeg",
		Quality:       80,
		MaxWidth:      1920,
		MaxHeight:     1080,
		EveryNthFrame: 1,
	})
	assert.NoError(t, err)
}

func TestCDPConnection_PageStopScreencast(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.PageStopScreencast(ctx)
	assert.NoError(t, err)
}

func TestCDPConnection_PageScreencastFrameAck(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.PageScreencastFrameAck(ctx, 42)
	assert.NoError(t, err)
}

func TestCDPConnection_PageCaptureScreenshot(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Test with nil params
	data, err := conn.PageCaptureScreenshot(ctx, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test with custom params
	data, err = conn.PageCaptureScreenshot(ctx, &PageCaptureScreenshotParams{
		Format:  "jpeg",
		Quality: 90,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestCDPConnection_InputDispatchMouseEvent(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
		Type:       "mousePressed",
		X:          100,
		Y:          200,
		Button:     "left",
		ClickCount: 1,
	})
	assert.NoError(t, err)

	// Test mouse move
	err = conn.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
		Type: "mouseMoved",
		X:    150,
		Y:    250,
	})
	assert.NoError(t, err)

	// Test mouse wheel
	err = conn.InputDispatchMouseEvent(ctx, &InputDispatchMouseEventParams{
		Type:   "mouseWheel",
		X:      100,
		Y:      200,
		DeltaX: 0,
		DeltaY: -100,
	})
	assert.NoError(t, err)
}

func TestCDPConnection_InputDispatchKeyEvent(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Test key down
	err = conn.InputDispatchKeyEvent(ctx, &InputDispatchKeyEventParams{
		Type: "keyDown",
		Key:  "a",
		Code: "KeyA",
	})
	assert.NoError(t, err)

	// Test key up
	err = conn.InputDispatchKeyEvent(ctx, &InputDispatchKeyEventParams{
		Type: "keyUp",
		Key:  "a",
		Code: "KeyA",
	})
	assert.NoError(t, err)

	// Test char event
	err = conn.InputDispatchKeyEvent(ctx, &InputDispatchKeyEventParams{
		Type: "char",
		Text: "a",
	})
	assert.NoError(t, err)
}

func TestCDPConnection_TargetActivateTarget(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.TargetActivateTarget(ctx, "target-123")
	assert.NoError(t, err)
}

func TestCDPConnection_TargetSetDiscoverTargets(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.TargetSetDiscoverTargets(ctx, true)
	assert.NoError(t, err)

	err = conn.TargetSetDiscoverTargets(ctx, false)
	assert.NoError(t, err)
}

func TestCDPConnection_EmulationSetDeviceMetricsOverride(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.EmulationSetDeviceMetricsOverride(ctx, 1920, 1080, 1.0, false)
	assert.NoError(t, err)

	// Mobile mode
	err = conn.EmulationSetDeviceMetricsOverride(ctx, 375, 812, 3.0, true)
	assert.NoError(t, err)
}

func TestCDPConnection_RuntimeEnable(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	err = conn.RuntimeEnable(ctx)
	assert.NoError(t, err)
}

func TestCDPConnection_PageNavigate(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	result, err := conn.PageNavigate(ctx, "https://example.com", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "frame-123", result.FrameID)
	assert.Equal(t, "loader-456", result.LoaderID)

	// With referrer
	result, err = conn.PageNavigate(ctx, "https://example.com/page", "https://example.com")
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestCDPConnection_Connect_AlreadyConnected(t *testing.T) {
	server := newExtendedMockCDPServer()
	defer server.Close()

	conn := NewCDPConnection(server.URL())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Try to connect again
	err = conn.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected")
}

func TestCDPConnection_Connect_InvalidEndpoint(t *testing.T) {
	conn := NewCDPConnection("ws://invalid-host-that-does-not-exist:9222/devtools/browser/abc")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := conn.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dialing CDP endpoint")
}

func TestCDPConnection_handleMessage_Response(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	// Setup a pending response channel
	respCh := make(chan *cdpResponse, 1)
	conn.pendingMu.Lock()
	conn.pending[1] = respCh
	conn.pendingMu.Unlock()

	// Simulate receiving a response
	data := []byte(`{"id":1,"result":{"success":true}}`)
	conn.handleMessage(data)

	// Check response was received
	select {
	case resp := <-respCh:
		assert.NotNil(t, resp)
		assert.Equal(t, int64(1), resp.ID)
		assert.NotNil(t, resp.Result)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestCDPConnection_handleMessage_Event(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	eventReceived := make(chan json.RawMessage, 1)
	conn.OnEvent("Page.loadEventFired", func(params json.RawMessage) {
		eventReceived <- params
	})

	// Simulate receiving an event
	data := []byte(`{"method":"Page.loadEventFired","params":{"timestamp":12345}}`)
	conn.handleMessage(data)

	// Check event was handled
	select {
	case params := <-eventReceived:
		assert.NotNil(t, params)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestCDPConnection_handleMessage_UnknownEvent(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	// No handler registered for this event - should not panic
	data := []byte(`{"method":"Unknown.event","params":{}}`)
	conn.handleMessage(data) // Should not panic
}

func TestCDPConnection_handleMessage_InvalidJSON(t *testing.T) {
	conn := NewCDPConnection("ws://localhost:9222")

	// Should not panic on invalid JSON
	data := []byte(`not valid json`)
	conn.handleMessage(data) // Should not panic
}

// Test struct definitions
func TestPageNavigateParams(t *testing.T) {
	params := PageNavigateParams{
		URL:      "https://example.com",
		Referrer: "https://google.com",
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded PageNavigateParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "https://example.com", decoded.URL)
	assert.Equal(t, "https://google.com", decoded.Referrer)
}

func TestScreencastFrameParams(t *testing.T) {
	params := ScreencastFrameParams{
		Format:        "jpeg",
		Quality:       80,
		MaxWidth:      1920,
		MaxHeight:     1080,
		EveryNthFrame: 2,
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded ScreencastFrameParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "jpeg", decoded.Format)
	assert.Equal(t, 80, decoded.Quality)
	assert.Equal(t, 1920, decoded.MaxWidth)
}

func TestInputDispatchMouseEventParams(t *testing.T) {
	params := InputDispatchMouseEventParams{
		Type:        "mousePressed",
		X:           100.5,
		Y:           200.5,
		Modifiers:   2, // Ctrl
		Button:      "left",
		ClickCount:  1,
		PointerType: "mouse",
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded InputDispatchMouseEventParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "mousePressed", decoded.Type)
	assert.Equal(t, 100.5, decoded.X)
	assert.Equal(t, "left", decoded.Button)
}

func TestInputDispatchKeyEventParams(t *testing.T) {
	params := InputDispatchKeyEventParams{
		Type:       "keyDown",
		Key:        "Enter",
		Code:       "Enter",
		Modifiers:  8, // Shift
		AutoRepeat: false,
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded InputDispatchKeyEventParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "keyDown", decoded.Type)
	assert.Equal(t, "Enter", decoded.Key)
}

func TestTargetInfo(t *testing.T) {
	info := TargetInfo{
		TargetID:         "target-123",
		Type:             "page",
		Title:            "Test Page",
		URL:              "https://example.com",
		Attached:         true,
		CanAccessOpener:  false,
		BrowserContextID: "context-1",
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded TargetInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "target-123", decoded.TargetID)
	assert.Equal(t, "page", decoded.Type)
	assert.True(t, decoded.Attached)
}

func TestViewport(t *testing.T) {
	vp := Viewport{
		X:      0,
		Y:      100,
		Width:  1920,
		Height: 1080,
		Scale:  1.0,
	}

	data, err := json.Marshal(vp)
	require.NoError(t, err)

	var decoded Viewport
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, float64(1920), decoded.Width)
	assert.Equal(t, float64(1.0), decoded.Scale)
}

func TestScreencastFrameMetadata(t *testing.T) {
	meta := ScreencastFrameMetadata{
		OffsetTop:       10,
		PageScaleFactor: 1.0,
		DeviceWidth:     1920,
		DeviceHeight:    1080,
		ScrollOffsetX:   0,
		ScrollOffsetY:   100,
		Timestamp:       1234567890.123,
	}

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	var decoded ScreencastFrameMetadata
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, float64(1920), decoded.DeviceWidth)
	assert.Equal(t, 1234567890.123, decoded.Timestamp)
}

func TestEmulationSetDeviceMetricsOverrideParams(t *testing.T) {
	params := EmulationSetDeviceMetricsOverrideParams{
		Width:             375,
		Height:            812,
		DeviceScaleFactor: 3.0,
		Mobile:            true,
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded EmulationSetDeviceMetricsOverrideParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, 375, decoded.Width)
	assert.True(t, decoded.Mobile)
	assert.Equal(t, 3.0, decoded.DeviceScaleFactor)
}

func TestPageCaptureScreenshotParams(t *testing.T) {
	params := PageCaptureScreenshotParams{
		Format:                "jpeg",
		Quality:               90,
		FromSurface:           true,
		CaptureBeyondViewport: false,
		OptimizeForSpeed:      true,
		Clip: &Viewport{
			X:      0,
			Y:      0,
			Width:  800,
			Height: 600,
			Scale:  1,
		},
	}

	data, err := json.Marshal(params)
	require.NoError(t, err)

	var decoded PageCaptureScreenshotParams
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "jpeg", decoded.Format)
	assert.Equal(t, 90, decoded.Quality)
	require.NotNil(t, decoded.Clip)
	assert.Equal(t, float64(800), decoded.Clip.Width)
}

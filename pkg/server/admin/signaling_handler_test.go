package admin

import (
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
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/chunlea/marionette/pkg/streaming/sfu"
)

func TestDefaultSignalingConfig(t *testing.T) {
	config := DefaultSignalingConfig()

	assert.Equal(t, 1024, config.ReadBufferSize)
	assert.Equal(t, 1024, config.WriteBufferSize)
	assert.Equal(t, 10*time.Second, config.WriteWait)
	assert.Equal(t, 60*time.Second, config.PongWait)
	assert.Equal(t, 54*time.Second, config.PingPeriod)
	assert.Equal(t, int64(65536), config.MaxMessageSize)
	assert.Equal(t, []string{"*"}, config.AllowedOrigins)
}

func TestNewSignalingHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultSignalingConfig()

	// Create a real SFU for the handler test
	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	assert.NotNil(t, handler)
}

func TestNewSignalingHandler_NilLogger(t *testing.T) {
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, nil)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, nil)

	// Should not panic with nil logger
	handler := NewSignalingHandler(sfuHandler, config, nil)

	assert.NotNil(t, handler)
}

func TestSignalingHandler_ServeHTTP_MissingParams(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect without stream_id and peer_id
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)

	// Should fail to connect or get closed immediately
	if err == nil {
		// Connection was established, should get close message
		_, _, err := conn.ReadMessage()
		assert.Error(t, err)
		_ = conn.Close()
	} else if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	// Give time for server goroutines to finish
	time.Sleep(50 * time.Millisecond)
}

func TestSignalingHandler_ServeHTTP_Connection(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect with stream_id and peer_id
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?stream_id=stream_123&peer_id=peer_456"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Send a test message
	msg := sfu.SignalingMessage{
		Type:     sfu.SignalingTypeOffer,
		StreamID: "stream_123",
		PeerID:   "peer_456",
		SDP:      "test sdp",
	}
	msgBytes, _ := json.Marshal(msg)
	err = conn.WriteMessage(websocket.TextMessage, msgBytes)
	require.NoError(t, err)

	// Give time for message to be processed
	time.Sleep(50 * time.Millisecond)

	// Close connection
	_ = conn.Close()

	// Give time for server goroutines to finish
	time.Sleep(50 * time.Millisecond)
}

func TestSignalingHandler_Close(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect multiple clients
	var conns []*websocket.Conn
	for i := 0; i < 3; i++ {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?stream_id=stream_123&peer_id=peer_" + string(rune('A'+i))
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		require.NoError(t, err)
		conns = append(conns, conn)
	}

	// Close all connections via handler
	handler.Close()

	// Try to read from connections - should fail
	for _, conn := range conns {
		_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, _, err := conn.ReadMessage()
		// Should get an error because connection was closed
		assert.Error(t, err)
		_ = conn.Close()
	}

	// Give time for server goroutines to finish
	time.Sleep(50 * time.Millisecond)
}

func TestSignalingHandler_SendMessage(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect with stream_id and peer_id
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?stream_id=stream_123&peer_id=peer_456"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	// Give time for connection to be registered
	time.Sleep(50 * time.Millisecond)

	// Send message via handler
	handler.sendMessage("peer_456", &sfu.SignalingMessage{
		Type: sfu.SignalingTypeAnswer,
		SDP:  "test answer",
	})

	// Read message from client
	_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)

	var received sfu.SignalingMessage
	err = json.Unmarshal(data, &received)
	require.NoError(t, err)

	assert.Equal(t, sfu.SignalingTypeAnswer, received.Type)
	assert.Equal(t, "test answer", received.SDP)

	// Give time for server goroutines to finish
	time.Sleep(50 * time.Millisecond)
}

func TestSignalingHandler_SendMessage_PeerNotFound(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Send message to non-existent peer - should not panic
	handler.sendMessage("nonexistent", &sfu.SignalingMessage{
		Type: sfu.SignalingTypeAnswer,
	})
}

func TestSignalingHandler_CheckOrigin(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name           string
		allowedOrigins []string
		origin         string
		shouldAllow    bool
	}{
		{
			name:           "wildcard allows all",
			allowedOrigins: []string{"*"},
			origin:         "https://example.com",
			shouldAllow:    true,
		},
		{
			name:           "exact match",
			allowedOrigins: []string{"https://example.com"},
			origin:         "https://example.com",
			shouldAllow:    true,
		},
		{
			name:           "no match",
			allowedOrigins: []string{"https://allowed.com"},
			origin:         "https://denied.com",
			shouldAllow:    false,
		},
		{
			name:           "multiple origins - match",
			allowedOrigins: []string{"https://one.com", "https://two.com"},
			origin:         "https://two.com",
			shouldAllow:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultSignalingConfig()
			config.AllowedOrigins = tt.allowedOrigins

			sfuConfig := sfu.DefaultConfig()
			sfuInstance, _ := sfu.New(sfuConfig, logger)
			sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

			handler := NewSignalingHandler(sfuHandler, config, logger)

			// Test the upgrader's CheckOrigin function
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", tt.origin)

			result := handler.upgrader.CheckOrigin(req)
			assert.Equal(t, tt.shouldAllow, result)
		})
	}
}

func TestSignalingHandler_ConcurrentConnections(t *testing.T) {
	// Use nop logger to avoid data race with test cleanup
	logger := zap.NewNop()
	config := DefaultSignalingConfig()

	sfuConfig := sfu.DefaultConfig()
	sfuInstance, _ := sfu.New(sfuConfig, logger)
	sfuHandler := sfu.NewSignalingHandler(sfuInstance, logger)

	handler := NewSignalingHandler(sfuHandler, config, logger)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect multiple clients concurrently
	var wg sync.WaitGroup
	numClients := 5

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?stream_id=stream_123&peer_id=peer_" + string(rune('0'+idx))
			conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				return
			}
			defer func() { _ = conn.Close() }()

			// Send a message
			msg := sfu.SignalingMessage{
				Type: sfu.SignalingTypeOffer,
				SDP:  "test sdp from peer " + string(rune('0'+idx)),
			}
			msgBytes, _ := json.Marshal(msg)
			_ = conn.WriteMessage(websocket.TextMessage, msgBytes)

			// Brief wait
			time.Sleep(20 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// Close handler
	handler.Close()

	// Give time for server goroutines to finish
	time.Sleep(50 * time.Millisecond)
}

package agent

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockMessageSender is defined in task_runner_test.go

func TestNewTunnelManager(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tm := NewTunnelManager(
		WithTMLogger(logger),
	)

	require.NotNil(t, tm)
	assert.NotNil(t, tm.logger)
	assert.NotNil(t, tm.relayManager)
	assert.NotNil(t, tm.pendingRequests)
	assert.NotNil(t, tm.tunnels)
}

func TestTunnelManager_SessionID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Initially empty
	assert.Empty(t, tm.GetSessionID())

	// Set session ID
	tm.SetSessionID("sess_test123")
	assert.Equal(t, "sess_test123", tm.GetSessionID())

	// Update session ID
	tm.SetSessionID("sess_test456")
	assert.Equal(t, "sess_test456", tm.GetSessionID())
}

func TestTunnelManager_CreateTunnel_NoSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// No session attached
	result, err := tm.CreateTunnel(context.Background(), &CreateTunnelParams{
		Type:      "http",
		LocalPort: 8000,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no session attached")
}

func TestTunnelManager_CreateTunnel_NoSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(
		WithTMLogger(logger),
		// No sender configured
	)

	tm.SetSessionID("sess_test123")

	result, err := tm.CreateTunnel(context.Background(), &CreateTunnelParams{
		Type:      "http",
		LocalPort: 8000,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "message sender not configured")
}

func TestTunnelManager_CreateTunnel_InvalidPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.SetSessionID("sess_test123")

	testCases := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"port too high", 70000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tm.CreateTunnel(context.Background(), &CreateTunnelParams{
				Type:      "http",
				LocalPort: tc.port,
			})

			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "local_port")
		})
	}
}

func TestTunnelManager_CreateTunnel_SendsRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.SetSessionID("sess_test123")

	// Start CreateTunnel in background (will timeout waiting for response)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _ = tm.CreateTunnel(ctx, &CreateTunnelParams{
		Type:      "http",
		LocalPort: 8000,
		Timeout:   100 * time.Millisecond,
	})

	// Verify request was sent
	require.Len(t, sender.Messages(), 1)
	msg := sender.Messages()[0]

	req := msg.GetCreateTunnelRequest()
	require.NotNil(t, req)
	assert.NotEmpty(t, req.RequestId) // Verify request_id is set
	assert.Equal(t, "sess_test123", req.SessionId)
	assert.Equal(t, "http", req.Type)
	assert.Equal(t, int32(8000), req.LocalPort)
	assert.Equal(t, "outbound", req.Direction)
}

func TestTunnelManager_CreateTunnel_DefaultType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.SetSessionID("sess_test123")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Type not specified, should default to "http"
	_, _ = tm.CreateTunnel(ctx, &CreateTunnelParams{
		LocalPort: 8000,
		Timeout:   100 * time.Millisecond,
	})

	require.Len(t, sender.Messages(), 1)
	req := sender.Messages()[0].GetCreateTunnelRequest()
	require.NotNil(t, req)
	assert.Equal(t, "http", req.Type)
}

func TestTunnelManager_HandleCreateTunnelResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.SetSessionID("sess_test123")

	// Start CreateTunnel in background
	resultCh := make(chan *CreateTunnelResult)
	errCh := make(chan error)

	go func() {
		result, err := tm.CreateTunnel(context.Background(), &CreateTunnelParams{
			Type:      "http",
			LocalPort: 8000,
			Timeout:   5 * time.Second,
		})
		if err != nil {
			errCh <- err
		} else {
			resultCh <- result
		}
	}()

	// Wait for request to be sent
	time.Sleep(50 * time.Millisecond)

	// Get the request_id from the sent message
	require.Len(t, sender.Messages(), 1)
	req := sender.Messages()[0].GetCreateTunnelRequest()
	require.NotNil(t, req)
	requestID := req.GetRequestId()

	// Send response with matching request_id
	tm.HandleCreateTunnelResponse(&pb.CreateTunnelResponse{
		RequestId:       requestID,
		Success:         true,
		TunnelId:        "tun_test123",
		Token:           "ttok_test456",
		PublicUrl:       "http://localhost:8080/tunnels/tun_test123",
		ExpiresAtUnixMs: time.Now().Add(time.Hour).UnixMilli(),
	})

	// Wait for result
	select {
	case result := <-resultCh:
		require.NotNil(t, result)
		assert.Equal(t, "tun_test123", result.TunnelID)
		assert.Equal(t, "ttok_test456", result.Token)
		assert.Equal(t, "http://localhost:8080/tunnels/tun_test123", result.PublicURL)
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	// Verify tunnel is stored
	info, ok := tm.GetTunnel("tun_test123")
	require.True(t, ok)
	assert.Equal(t, "tun_test123", info.ID)
	assert.Equal(t, "http", info.Type)
	assert.Equal(t, 8000, info.LocalPort)
}

func TestTunnelManager_HandleCreateTunnelResponse_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.SetSessionID("sess_test123")

	// Start CreateTunnel in background
	errCh := make(chan error)

	go func() {
		_, err := tm.CreateTunnel(context.Background(), &CreateTunnelParams{
			Type:      "http",
			LocalPort: 8000,
			Timeout:   5 * time.Second,
		})
		errCh <- err
	}()

	// Wait for request to be sent
	time.Sleep(50 * time.Millisecond)

	// Get the request_id from the sent message
	require.Len(t, sender.Messages(), 1)
	req := sender.Messages()[0].GetCreateTunnelRequest()
	require.NotNil(t, req)
	requestID := req.GetRequestId()

	// Send error response with matching request_id
	tm.HandleCreateTunnelResponse(&pb.CreateTunnelResponse{
		RequestId: requestID,
		Success:   false,
		Error:     "tunnel limit exceeded",
	})

	// Wait for error
	select {
	case err := <-errCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tunnel limit exceeded")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error")
	}
}

func TestTunnelManager_ListTunnels(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Initially empty
	tunnels := tm.ListTunnels()
	assert.Empty(t, tunnels)

	// Add tunnels directly for testing
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_1"] = &TunnelInfo{ID: "tun_1", Type: "http", LocalPort: 8000}
	tm.tunnels["tun_2"] = &TunnelInfo{ID: "tun_2", Type: "http", LocalPort: 9000}
	tm.tunnelsMu.Unlock()

	tunnels = tm.ListTunnels()
	assert.Len(t, tunnels, 2)
}

func TestTunnelManager_CloseTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: 8000}
	tm.tunnelsMu.Unlock()

	// Close it
	err := tm.CloseTunnel("tun_test", "test cleanup")
	require.NoError(t, err)

	// Verify removed from map
	_, ok := tm.GetTunnel("tun_test")
	assert.False(t, ok)

	// Verify CloseTunnel message was sent
	require.Len(t, sender.Messages(), 1)
	msg := sender.Messages()[0]
	close := msg.GetCloseTunnel()
	require.NotNil(t, close)
	assert.Equal(t, "tun_test", close.TunnelId)
	assert.Equal(t, "test cleanup", close.Reason)
}

func TestTunnelManager_CloseTunnel_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(
		WithTMLogger(logger),
	)

	err := tm.CloseTunnel("tun_nonexistent", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel not found")
}

func TestTunnelManager_HandleTunnelData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(
		WithTMLogger(logger),
	)

	// HandleTunnelData should work even if tunnel connection isn't established
	// (RelayManager handles the routing)
	err := tm.HandleTunnelData(context.Background(), &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_123",
		Data:         []byte("test data"),
		Eof:          false,
	})

	// Should return error since no relay is connected
	assert.Error(t, err)
}

func TestTunnelManager_HandleCreateTunnelResponse_NoMatchingRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(
		WithTMLogger(logger),
	)

	// Send response without any pending request
	// Should not panic, just log warning
	tm.HandleCreateTunnelResponse(&pb.CreateTunnelResponse{
		RequestId: "req_nonexistent",
		Success:   true,
		TunnelId:  "tun_test",
	})

	// No assertion needed - just verify it doesn't panic
}

func TestTunnelManager_SendFrame_NoSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	// Create TunnelManager without sender
	tm := NewTunnelManager(
		WithTMLogger(logger),
	)

	// Call sendFrame directly via relayManager callback
	// The relayManager was initialized with sendFrame as callback
	err := tm.sendFrame(&tunnel.Frame{
		Type:     tunnel.FrameTypeData,
		TunnelID: "tun_test",
		Payload:  []byte("test"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "message sender not configured")
}

func TestTunnelManager_IsWebSocketUpgrade(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	testCases := []struct {
		name     string
		request  string
		expected bool
	}{
		{
			name: "valid websocket upgrade",
			request: "GET /ws HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
				"\r\n",
			expected: true,
		},
		{
			name: "websocket upgrade case insensitive",
			request: "GET /ws HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"UPGRADE: WEBSOCKET\r\n" +
				"CONNECTION: UPGRADE\r\n" +
				"\r\n",
			expected: true,
		},
		{
			name: "regular http request",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"\r\n",
			expected: false,
		},
		{
			name: "only upgrade header",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Upgrade: websocket\r\n" +
				"\r\n",
			expected: false,
		},
		{
			name: "only connection header",
			request: "GET / HTTP/1.1\r\n" +
				"Host: localhost\r\n" +
				"Connection: upgrade\r\n" +
				"\r\n",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tm.isWebSocketUpgrade([]byte(tc.request))
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestTunnelManager_SendTunnelData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Send some data
	tm.sendTunnelData("tun_test", "conn_123", []byte("hello world"), false)

	require.Len(t, sender.Messages(), 1)
	msg := sender.Messages()[0]
	data := msg.GetTunnelData()
	require.NotNil(t, data)
	assert.Equal(t, "tun_test", data.TunnelId)
	assert.Equal(t, "conn_123", data.ConnectionId)
	assert.Equal(t, []byte("hello world"), data.Data)
	assert.False(t, data.Eof)

	// Send EOF
	tm.sendTunnelData("tun_test", "conn_123", nil, true)
	require.Len(t, sender.Messages(), 2)
	eofMsg := sender.Messages()[1].GetTunnelData()
	assert.True(t, eofMsg.Eof)
}

func TestTunnelManager_SendTunnelData_NoSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Should not panic when sender is nil
	tm.sendTunnelData("tun_test", "conn_123", []byte("data"), false)
}

func TestTunnelManager_SendHTTPErrorResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	tm.sendHTTPErrorResponse("tun_test", "conn_123", fmt.Errorf("connection refused"))

	require.Len(t, sender.Messages(), 1)
	msg := sender.Messages()[0]
	data := msg.GetTunnelData()
	require.NotNil(t, data)
	assert.Equal(t, "tun_test", data.TunnelId)
	assert.Equal(t, "conn_123", data.ConnectionId)
	assert.True(t, data.Eof)

	// Check response format
	response := string(data.Data)
	assert.Contains(t, response, "HTTP/1.1 502 Bad Gateway")
	assert.Contains(t, response, "connection refused")
}

func TestTunnelManager_ClosePersistentConn(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Create a mock connection using net.Pipe
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	// Add persistent connection
	tm.persistentConnsMu.Lock()
	tm.persistentConns["conn_123"] = &persistentConn{
		conn:      server,
		tunnelID:  "tun_test",
		localPort: 8000,
		cancel:    cancel,
	}
	tm.persistentConnsMu.Unlock()

	// Close it
	tm.closePersistentConn("conn_123")

	// Verify removed from map
	tm.persistentConnsMu.RLock()
	_, exists := tm.persistentConns["conn_123"]
	tm.persistentConnsMu.RUnlock()
	assert.False(t, exists)

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("context should be cancelled")
	}
}

func TestTunnelManager_ClosePersistentConn_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Should not panic when connection doesn't exist
	tm.closePersistentConn("nonexistent")
}

func TestTunnelManager_HandleTunnelData_EOF(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Create a mock connection
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	_, cancel := context.WithCancel(context.Background())

	// Add persistent connection
	tm.persistentConnsMu.Lock()
	tm.persistentConns["conn_123"] = &persistentConn{
		conn:      server,
		tunnelID:  "tun_test",
		localPort: 8000,
		cancel:    cancel,
	}
	tm.persistentConnsMu.Unlock()

	// Send EOF
	err := tm.HandleTunnelData(context.Background(), &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_123",
		Eof:          true,
	})

	require.NoError(t, err)

	// Verify connection was closed
	tm.persistentConnsMu.RLock()
	_, exists := tm.persistentConns["conn_123"]
	tm.persistentConnsMu.RUnlock()
	assert.False(t, exists)
}

func TestTunnelManager_HandleTunnelData_PersistentConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Create a mock connection
	client, server := net.Pipe()
	defer func() {
		_ = client.Close()
		_ = server.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Add persistent connection
	tm.persistentConnsMu.Lock()
	tm.persistentConns["conn_123"] = &persistentConn{
		conn:      server,
		tunnelID:  "tun_test",
		localPort: 8000,
		cancel:    cancel,
	}
	tm.persistentConnsMu.Unlock()

	// Send data through tunnel
	testData := []byte("hello from client")
	go func() {
		err := tm.HandleTunnelData(ctx, &pb.TunnelData{
			TunnelId:     "tun_test",
			ConnectionId: "conn_123",
			Data:         testData,
			Eof:          false,
		})
		assert.NoError(t, err)
	}()

	// Read data from the other end of the pipe
	buf := make([]byte, 100)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	n, err := client.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf[:n])
}

// startMockHTTPServer starts a mock HTTP server for testing
func startMockHTTPServer(t *testing.T, handler http.HandlerFunc) (int, func()) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()

	port := listener.Addr().(*net.TCPAddr).Port
	cleanup := func() {
		_ = server.Close()
		_ = listener.Close()
	}

	return port, cleanup
}

func TestTunnelManager_HandleHTTPRequest_RegularResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start mock server
	port, cleanup := startMockHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, World!"))
	})
	defer cleanup()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request
	request := fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", port)
	tm.handleHTTPRequest(context.Background(), "tun_test", "conn_123", port, []byte(request))

	// Wait for response
	time.Sleep(100 * time.Millisecond)

	// Check that response was sent
	require.NotEmpty(t, sender.Messages())

	// Collect all data
	var responseData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil {
			responseData = append(responseData, data.Data...)
		}
	}

	response := string(responseData)
	assert.Contains(t, response, "HTTP/1.1 200 OK")
	assert.Contains(t, response, "Hello, World!")
}

func TestTunnelManager_HandleHTTPRequest_ConnectionError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Use a port that's not listening
	port := 59999

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request to non-existent server
	request := fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", port)
	tm.handleHTTPRequest(context.Background(), "tun_test", "conn_123", port, []byte(request))

	// Wait for error response
	time.Sleep(100 * time.Millisecond)

	// Check that error response was sent
	require.NotEmpty(t, sender.Messages())
	msg := sender.Messages()[0]
	data := msg.GetTunnelData()
	require.NotNil(t, data)
	assert.True(t, data.Eof)
	assert.Contains(t, string(data.Data), "502 Bad Gateway")
}

func TestTunnelManager_HandleHTTPRequest_StreamingResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a raw TCP server to avoid Go's http.Server adding chunked encoding
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}

		// Send SSE response
		response := "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/event-stream\r\n" +
			"Cache-Control: no-cache\r\n" +
			"\r\n"
		_, _ = conn.Write([]byte(response))

		// Send events with small delays
		for i := 0; i < 3; i++ {
			_, _ = fmt.Fprintf(conn, "data: event %d\n\n", i)
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request with context that will be cancelled
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	request := fmt.Sprintf("GET /events HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", port)
	tm.handleHTTPRequest(ctx, "tun_test", "conn_123", port, []byte(request))

	// Wait for streaming to complete
	time.Sleep(200 * time.Millisecond)

	// Check that multiple data messages were sent
	require.NotEmpty(t, sender.Messages())

	// Collect all data
	var responseData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil {
			responseData = append(responseData, data.Data...)
		}
	}

	response := string(responseData)
	assert.Contains(t, response, "text/event-stream")
	assert.Contains(t, response, "data: event")
}

func TestTunnelManager_HandleWebSocketUpgrade(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a mock WebSocket server using raw TCP
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	// Handle connection in background
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read upgrade request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}

		// Send WebSocket upgrade response
		response := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n" +
			"\r\n"
		_, _ = conn.Write([]byte(response))

		// Echo back any data received
		buf := make([]byte, 1024)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			_, _ = conn.Write(buf[:n])
		}
	}()

	// Create connection to mock server
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	require.NoError(t, err)

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send WebSocket upgrade request
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tm.handleWebSocketUpgrade(ctx, "tun_test", "conn_123", port, conn, []byte(request))

	// Wait for upgrade to complete
	time.Sleep(100 * time.Millisecond)

	// Check that upgrade response was sent
	require.NotEmpty(t, sender.Messages())

	// Collect response
	var responseData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && !data.Eof {
			responseData = append(responseData, data.Data...)
		}
	}

	response := string(responseData)
	assert.Contains(t, response, "101 Switching Protocols")

	// Check that persistent connection was created
	tm.persistentConnsMu.RLock()
	_, exists := tm.persistentConns["conn_123"]
	tm.persistentConnsMu.RUnlock()
	assert.True(t, exists, "persistent connection should be created")

	// Cleanup
	cancel()
	<-serverDone
}

func TestTunnelManager_HandleWebSocketUpgrade_FailedUpgrade(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a mock server that rejects WebSocket upgrade
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\r\n" {
				break
			}
		}

		// Send rejection
		response := "HTTP/1.1 400 Bad Request\r\n" +
			"Content-Type: text/plain\r\n" +
			"Content-Length: 18\r\n" +
			"\r\n" +
			"Upgrade rejected\r\n"
		_, _ = conn.Write([]byte(response))
	}()

	// Create connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	require.NoError(t, err)

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send WebSocket upgrade request
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"\r\n"

	ctx := context.Background()
	tm.handleWebSocketUpgrade(ctx, "tun_test", "conn_123", port, conn, []byte(request))

	// Wait for response
	time.Sleep(100 * time.Millisecond)

	// Check that error response was sent
	require.NotEmpty(t, sender.Messages())
	lastMsg := sender.Messages()[len(sender.Messages())-1]
	data := lastMsg.GetTunnelData()
	require.NotNil(t, data)
	assert.True(t, data.Eof, "should send EOF on failed upgrade")

	// Persistent connection should NOT be created
	tm.persistentConnsMu.RLock()
	_, exists := tm.persistentConns["conn_123"]
	tm.persistentConnsMu.RUnlock()
	assert.False(t, exists, "no persistent connection on failed upgrade")
}

func TestTunnelManager_HandleCreateTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Handle create tunnel from server
	err := tm.HandleCreateTunnel("tun_test", "http", 8000)
	require.NoError(t, err)

	// Verify tunnel is stored
	info, ok := tm.GetTunnel("tun_test")
	require.True(t, ok)
	assert.Equal(t, "tun_test", info.ID)
	assert.Equal(t, "http", info.Type)
	assert.Equal(t, 8000, info.LocalPort)
}

func TestTunnelManager_HandleTunnelData_HTTPTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start mock server
	port, cleanup := startMockHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	defer cleanup()

	// Add HTTP tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send data through HandleTunnelData
	request := fmt.Sprintf("GET / HTTP/1.1\r\nHost: localhost:%d\r\n\r\n", port)
	err := tm.HandleTunnelData(context.Background(), &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_123",
		Data:         []byte(request),
		Eof:          false,
	})

	require.NoError(t, err)

	// Wait for response
	time.Sleep(100 * time.Millisecond)

	// Verify response was sent
	require.NotEmpty(t, sender.Messages())
}

func TestTunnelManager_RelayFromLocalService(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate local service
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	// Track when goroutine exits
	done := make(chan struct{})

	// Start relay
	reader := bufio.NewReader(server)
	go func() {
		defer close(done)
		tm.relayFromLocalService(ctx, "tun_test", "conn_123", server, reader)
	}()

	// Write data from "local service"
	testData := []byte("hello from local service")
	_, err := client.Write(testData)
	require.NoError(t, err)

	// Wait for relay
	time.Sleep(200 * time.Millisecond)

	// Cancel to stop relay and wait for goroutine to exit
	cancel()
	// Close server connection to unblock Read
	_ = server.Close()

	// Wait for goroutine to finish before test ends
	select {
	case <-done:
		// Goroutine exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("relay goroutine did not exit in time")
	}

	// Check that data was forwarded
	var receivedData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && !data.Eof {
			receivedData = append(receivedData, data.Data...)
		}
	}
	assert.Equal(t, testData, receivedData)
}

func TestTunnelManager_HandleRegularResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate connection
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	// Write response in background
	go func() {
		// Body with known content length
		body := "Hello, World!"
		_, _ = client.Write([]byte(body))
		_ = client.Close()
	}()

	// Handle response
	reader := bufio.NewReader(server)
	tm.handleRegularResponse("tun_test", "conn_123", server, reader, int64(len("Hello, World!")))

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check that data was sent
	var receivedData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && !data.Eof {
			receivedData = append(receivedData, data.Data...)
		}
	}
	assert.Equal(t, "Hello, World!", string(receivedData))
}

func TestTunnelManager_HandleStreamingResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate connection
	client, server := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Write streaming data in background - write all at once since net.Pipe is synchronous
	go func() {
		// Write all events together
		_, _ = client.Write([]byte("data: event 0\n\ndata: event 1\n\ndata: event 2\n\n"))
		_ = client.Close()
	}()

	// Handle streaming response
	reader := bufio.NewReader(server)
	tm.handleStreamingResponse(ctx, "tun_test", "conn_123", server, reader)

	// Check that data was sent
	var receivedData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && !data.Eof {
			receivedData = append(receivedData, data.Data...)
		}
	}
	response := string(receivedData)
	assert.Contains(t, response, "data: event 0")
	assert.Contains(t, response, "data: event 1")
	assert.Contains(t, response, "data: event 2")
}

func TestTunnelManager_HandleHTTPRequest_WebSocketDetection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a mock WebSocket server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.TrimSpace(line) == "" {
				break
			}
		}

		// Send WebSocket upgrade response
		response := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"\r\n"
		_, _ = conn.Write([]byte(response))
		time.Sleep(100 * time.Millisecond)
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send WebSocket upgrade request through handleHTTPRequest
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"\r\n"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tm.handleHTTPRequest(ctx, "tun_test", "conn_123", port, []byte(request))

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check that upgrade response was sent
	var responseData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil {
			responseData = append(responseData, data.Data...)
		}
	}

	response := string(responseData)
	assert.Contains(t, response, "101 Switching Protocols")
}

func TestTunnelManager_SendFrame_WithSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Send a frame
	err := tm.sendFrame(&tunnel.Frame{
		Type:     tunnel.FrameTypeData,
		TunnelID: "tun_test",
		Payload:  []byte("test data"),
	})

	require.NoError(t, err)
	require.Len(t, sender.Messages(), 1)

	msg := sender.Messages()[0]
	data := msg.GetTunnelData()
	require.NotNil(t, data)
	assert.Equal(t, "tun_test", data.TunnelId)
	assert.Equal(t, []byte("test data"), data.Data)
	assert.False(t, data.Eof)
}

func TestTunnelManager_SendFrame_CloseType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Send a close frame
	err := tm.sendFrame(&tunnel.Frame{
		Type:     tunnel.FrameTypeClose,
		TunnelID: "tun_test",
		Payload:  nil,
	})

	require.NoError(t, err)
	require.Len(t, sender.Messages(), 1)

	msg := sender.Messages()[0]
	data := msg.GetTunnelData()
	require.NotNil(t, data)
	assert.True(t, data.Eof)
}

func TestTunnelManager_HandleRegularResponse_UnknownContentLength(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate connection
	client, server := net.Pipe()

	// Write response in background
	go func() {
		// Send data in chunks
		_, _ = client.Write([]byte("Hello, "))
		time.Sleep(10 * time.Millisecond)
		_, _ = client.Write([]byte("World!"))
		_ = client.Close()
	}()

	// Handle response with unknown content length (0 means read until EOF/timeout)
	reader := bufio.NewReader(server)
	tm.handleRegularResponse("tun_test", "conn_123", server, reader, 0)

	// Check that data was sent
	var receivedData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && !data.Eof {
			receivedData = append(receivedData, data.Data...)
		}
	}
	// Should have received all data
	assert.Contains(t, string(receivedData), "Hello")
}

func TestTunnelManager_HandleCreateTunnel_TCPTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// TCP tunnels will try to start a relay, which will fail since no local service
	// is running. This tests the TCP path.
	err := tm.HandleCreateTunnel("tun_test", "tcp", 59998)

	// The relay will fail since there's no local service, but the tunnel should still be created
	// Actually HandleCreateTunnel cleans up on relay failure for non-HTTP tunnels
	assert.Error(t, err)

	// Tunnel should not exist because relay failed
	_, ok := tm.GetTunnel("tun_test")
	assert.False(t, ok)
}

func TestTunnelManager_HandleWebSocketUpgrade_ConnectionError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a connection that's already closed
	client, server := net.Pipe()
	_ = server.Close()
	_ = client.Close()

	// Try to upgrade on closed connection
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"\r\n"

	tm.handleWebSocketUpgrade(context.Background(), "tun_test", "conn_123", 8000, server, []byte(request))

	// Should have sent an error response
	require.NotEmpty(t, sender.Messages())
}

func TestTunnelManager_HandleHTTPRequest_WriteError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a server that immediately closes the connection
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Close immediately to cause write error
		_ = conn.Close()
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	tm.handleHTTPRequest(context.Background(), "tun_test", "conn_123", port, []byte(request))

	// Wait for error handling
	time.Sleep(100 * time.Millisecond)

	// Should have sent an error response
	require.NotEmpty(t, sender.Messages())
	data := sender.Messages()[0].GetTunnelData()
	assert.True(t, data.Eof)
}

func TestTunnelManager_HandleHTTPRequest_InvalidResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a server that sends invalid HTTP response
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}

		// Send invalid HTTP response
		_, _ = conn.Write([]byte("NOT A VALID HTTP RESPONSE\r\n\r\n"))
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request
	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	tm.handleHTTPRequest(context.Background(), "tun_test", "conn_123", port, []byte(request))

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should have sent an error response
	require.NotEmpty(t, sender.Messages())
}

func TestTunnelManager_HandleTunnelData_PersistentConnectionWriteError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// Create a connection and close one end
	client, server := net.Pipe()
	_ = client.Close() // Close client side so write will fail

	_, cancel := context.WithCancel(context.Background())

	// Add persistent connection with closed pipe
	tm.persistentConnsMu.Lock()
	tm.persistentConns["conn_123"] = &persistentConn{
		conn:      server,
		tunnelID:  "tun_test",
		localPort: 8000,
		cancel:    cancel,
	}
	tm.persistentConnsMu.Unlock()

	// Try to send data - should fail and close connection
	err := tm.HandleTunnelData(context.Background(), &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_123",
		Data:         []byte("test data"),
		Eof:          false,
	})

	assert.Error(t, err)

	// Connection should be removed
	tm.persistentConnsMu.RLock()
	_, exists := tm.persistentConns["conn_123"]
	tm.persistentConnsMu.RUnlock()
	assert.False(t, exists)
}

func TestTunnelManager_StartRelay(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := NewTunnelManager(WithTMLogger(logger))

	// startRelay will fail because there's no local service
	err := tm.startRelay("tun_test", 59997)
	assert.Error(t, err)
}

func TestTunnelManager_HandleStreamingResponse_ContextCancel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate connection
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())

	// Write some data then cancel context
	go func() {
		_, _ = client.Write([]byte("data: event 0\n\n"))
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Handle streaming response - should exit when context is cancelled
	reader := bufio.NewReader(server)
	tm.handleStreamingResponse(ctx, "tun_test", "conn_123", server, reader)

	// Verify EOF was sent
	var hasEof bool
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && data.Eof {
			hasEof = true
			break
		}
	}
	assert.True(t, hasEof, "should send EOF when context is cancelled")
}

func TestTunnelManager_HandleStreamingResponse_ReadError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe and immediately close the writer to cause read error
	client, server := net.Pipe()

	ctx := context.Background()

	// Close client to cause read error
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = client.Close()
	}()

	// Handle streaming response
	reader := bufio.NewReader(server)
	tm.handleStreamingResponse(ctx, "tun_test", "conn_123", server, reader)

	// Verify EOF was sent after error
	var hasEof bool
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && data.Eof {
			hasEof = true
			break
		}
	}
	assert.True(t, hasEof, "should send EOF on read error")
}

func TestTunnelManager_RelayFromLocalService_ReadError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe to simulate local service
	client, server := net.Pipe()

	ctx := context.Background()

	// Add persistent connection so it can be cleaned up
	tm.persistentConnsMu.Lock()
	tm.persistentConns["conn_123"] = &persistentConn{
		conn:      server,
		tunnelID:  "tun_test",
		localPort: 8000,
		cancel:    func() {},
	}
	tm.persistentConnsMu.Unlock()

	// Close client side to cause read error
	_ = client.Close()

	// Start relay - should exit quickly due to error
	reader := bufio.NewReader(server)
	tm.relayFromLocalService(ctx, "tun_test", "conn_123", server, reader)

	// Verify EOF was sent
	var hasEof bool
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil && data.Eof {
			hasEof = true
			break
		}
	}
	assert.True(t, hasEof, "should send EOF on read error")
}

func TestTunnelManager_HandleWebSocketUpgrade_ReadHeaderError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a server that accepts but doesn't respond
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		// Close immediately after accepting to cause read error
		_ = conn.Close()
	}()

	// Create connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	require.NoError(t, err)

	// Send WebSocket upgrade request
	request := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"\r\n"

	tm.handleWebSocketUpgrade(context.Background(), "tun_test", "conn_123", port, conn, []byte(request))

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should have sent an error response
	require.NotEmpty(t, sender.Messages())
}

func TestTunnelManager_HandleRegularResponse_ReadBodyError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Create a pipe and close it partway through
	client, server := net.Pipe()

	// Write partial data then close
	go func() {
		_, _ = client.Write([]byte("Hell"))
		_ = client.Close()
	}()

	// Try to read more than available
	reader := bufio.NewReader(server)
	tm.handleRegularResponse("tun_test", "conn_123", server, reader, 100) // Content-Length is 100 but only 4 bytes available

	// Should still send what we got plus EOF
	require.NotEmpty(t, sender.Messages())
}

func TestTunnelManager_HandleHTTPRequest_ChunkedResponse(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a raw TCP server that sends chunked response
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}

		// Send chunked response
		response := "HTTP/1.1 200 OK\r\n" +
			"Transfer-Encoding: chunked\r\n" +
			"\r\n" +
			"5\r\n" +
			"Hello\r\n" +
			"0\r\n" +
			"\r\n"
		_, _ = conn.Write([]byte(response))
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	tm.handleHTTPRequest(ctx, "tun_test", "conn_123", port, []byte(request))

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Collect all data
	var responseData []byte
	for _, msg := range sender.Messages() {
		if data := msg.GetTunnelData(); data != nil {
			responseData = append(responseData, data.Data...)
		}
	}

	response := string(responseData)
	assert.Contains(t, response, "Transfer-Encoding: chunked")
	assert.Contains(t, response, "Hello")
}

func TestTunnelManager_HandleHTTPRequest_NegativeContentLength(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := &mockMessageSender{}
	tm := NewTunnelManager(
		WithTMLogger(logger),
		WithTMSender(sender),
	)

	// Start a raw TCP server that sends response without Content-Length
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read request
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}

		// Send response without Content-Length (will be treated as streaming)
		response := "HTTP/1.1 200 OK\r\n" +
			"Content-Type: application/octet-stream\r\n" +
			"\r\n" +
			"some data"
		_, _ = conn.Write([]byte(response))
	}()

	// Add tunnel
	tm.tunnelsMu.Lock()
	tm.tunnels["tun_test"] = &TunnelInfo{ID: "tun_test", Type: "http", LocalPort: port}
	tm.tunnelsMu.Unlock()

	// Send HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	request := "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"
	tm.handleHTTPRequest(ctx, "tun_test", "conn_123", port, []byte(request))

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Should have received data
	require.NotEmpty(t, sender.Messages())
}

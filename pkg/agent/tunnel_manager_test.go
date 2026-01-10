package agent

import (
	"context"
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

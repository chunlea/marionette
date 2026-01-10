package grpc

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

func TestTunnelHandler_HandleCreateTunnelRequest(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create tunnel manager without store (works for in-memory only)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithBaseURL("http://localhost:8080"),
	)

	// Create tunnel handler
	handler := NewTunnelHandler(
		WithTHLogger(logger),
		WithTHTunnelManager(tm),
	)

	t.Run("successful tunnel creation", func(t *testing.T) {
		req := &pb.CreateTunnelRequest{
			SessionId: "sess_test123",
			Type:      "http",
			LocalPort: 8000,
		}

		resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.True(t, resp.Success)
		assert.Empty(t, resp.Error)
		assert.NotEmpty(t, resp.TunnelId)
		assert.NotEmpty(t, resp.Token)
		assert.Contains(t, resp.PublicUrl, resp.TunnelId)
		assert.Greater(t, resp.ExpiresAtUnixMs, time.Now().UnixMilli())
	})

	t.Run("missing session_id", func(t *testing.T) {
		req := &pb.CreateTunnelRequest{
			Type:      "http",
			LocalPort: 8000,
		}

		resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "session_id")
	})

	t.Run("invalid port", func(t *testing.T) {
		req := &pb.CreateTunnelRequest{
			SessionId: "sess_test123",
			Type:      "http",
			LocalPort: 0,
		}

		resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "local_port")
	})

	t.Run("port too high", func(t *testing.T) {
		req := &pb.CreateTunnelRequest{
			SessionId: "sess_test123",
			Type:      "http",
			LocalPort: 70000,
		}

		resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "local_port")
	})

	t.Run("default type to http", func(t *testing.T) {
		req := &pb.CreateTunnelRequest{
			SessionId: "sess_test123",
			LocalPort: 3000,
			// Type is empty, should default to "http"
		}

		resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.True(t, resp.Success)
		assert.NotEmpty(t, resp.TunnelId)
	})
}

func TestTunnelHandler_NoTunnelManager(t *testing.T) {
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create handler without tunnel manager
	handler := NewTunnelHandler(
		WithTHLogger(logger),
	)

	req := &pb.CreateTunnelRequest{
		SessionId: "sess_test123",
		Type:      "http",
		LocalPort: 8000,
	}

	resp, err := handler.HandleCreateTunnelRequest(ctx, "run_test456", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "tunnel manager not configured")
}

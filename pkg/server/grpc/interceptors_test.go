package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestLoggingUnaryInterceptor(t *testing.T) {
	// Create observable logger
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := LoggingUnaryInterceptor(logger)

	// Create mock handler that succeeds
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "response", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	require.NoError(t, err)
	assert.Equal(t, "response", resp)

	// Check logs
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "gRPC unary call completed", entry.Message)
	assert.Equal(t, "/test.Service/TestMethod", entry.ContextMap()["method"])
	assert.Equal(t, "OK", entry.ContextMap()["code"])
}

func TestLoggingUnaryInterceptor_WithError(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := LoggingUnaryInterceptor(logger)

	// Create mock handler that fails
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, status.Error(codes.NotFound, "not found")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	require.Error(t, err)
	assert.Nil(t, resp)

	// Check logs
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "gRPC unary call failed", entry.Message)
	assert.Equal(t, "NotFound", entry.ContextMap()["code"])
}

func TestLoggingUnaryInterceptor_WithRunnerID(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := LoggingUnaryInterceptor(logger)

	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "response", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	// Add runner_id to metadata
	md := metadata.Pairs("x-runner-id", "run_123")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, "request", info, handler)
	require.NoError(t, err)
	assert.Equal(t, "response", resp)

	// Check logs include runner_id
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "run_123", entry.ContextMap()["runner_id"])
}

func TestRecoveryUnaryInterceptor_NoPanic(t *testing.T) {
	logger := zap.NewNop()

	interceptor := RecoveryUnaryInterceptor(logger)

	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		return "response", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	require.NoError(t, err)
	assert.Equal(t, "response", resp)
}

func TestRecoveryUnaryInterceptor_WithPanic(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := RecoveryUnaryInterceptor(logger)

	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		panic("test panic")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/TestMethod",
	}

	resp, err := interceptor(context.Background(), "request", info, handler)
	require.Error(t, err)
	assert.Nil(t, resp)

	// Check error is gRPC Internal error
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())

	// Check panic was logged
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, "gRPC unary handler panicked", entry.Message)
	assert.Contains(t, entry.ContextMap()["stack"], "TestRecoveryUnaryInterceptor_WithPanic")
}

// mockServerStream implements grpc.ServerStream for testing
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func TestLoggingStreamInterceptor(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := LoggingStreamInterceptor(logger)

	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStream",
	}

	mockStream := &mockServerStream{ctx: context.Background()}
	err := interceptor(nil, mockStream, info, handler)
	require.NoError(t, err)

	// Check logs (should have open and close)
	require.Equal(t, 2, logs.Len())
	assert.Equal(t, "gRPC stream opened", logs.All()[0].Message)
	assert.Equal(t, "gRPC stream closed", logs.All()[1].Message)
}

func TestLoggingStreamInterceptor_WithError(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := LoggingStreamInterceptor(logger)

	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return status.Error(codes.Internal, "internal error")
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStream",
	}

	mockStream := &mockServerStream{ctx: context.Background()}
	err := interceptor(nil, mockStream, info, handler)
	require.Error(t, err)

	// Check logs
	require.Equal(t, 2, logs.Len())
	assert.Equal(t, "gRPC stream closed with error", logs.All()[1].Message)
}

func TestRecoveryStreamInterceptor_NoPanic(t *testing.T) {
	logger := zap.NewNop()

	interceptor := RecoveryStreamInterceptor(logger)

	handler := func(_ interface{}, _ grpc.ServerStream) error {
		return nil
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStream",
	}

	stream := &mockServerStream{ctx: context.Background()}
	err := interceptor(nil, stream, info, handler)
	require.NoError(t, err)
}

func TestRecoveryStreamInterceptor_WithPanic(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	interceptor := RecoveryStreamInterceptor(logger)

	handler := func(_ interface{}, _ grpc.ServerStream) error {
		panic("stream panic")
	}

	info := &grpc.StreamServerInfo{
		FullMethod: "/test.Service/TestStream",
	}

	stream := &mockServerStream{ctx: context.Background()}
	err := interceptor(nil, stream, info, handler)
	require.Error(t, err)

	// Check error is gRPC Internal error
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())

	// Check panic was logged
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "gRPC stream handler panicked", logs.All()[0].Message)
}

func TestExtractMetadataValue(t *testing.T) {
	tests := []struct {
		name     string
		md       metadata.MD
		key      string
		expected string
	}{
		{
			name:     "nil metadata",
			md:       nil,
			key:      "key",
			expected: "",
		},
		{
			name:     "key not found",
			md:       metadata.Pairs("other", "value"),
			key:      "key",
			expected: "",
		},
		{
			name:     "key found",
			md:       metadata.Pairs("key", "value"),
			key:      "key",
			expected: "value",
		},
		{
			name:     "multiple values",
			md:       metadata.Pairs("key", "first", "key", "second"),
			key:      "key",
			expected: "first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMetadataValue(tt.md, tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

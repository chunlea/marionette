package metrics

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor returns a gRPC unary server interceptor that records metrics.
func UnaryServerInterceptor(reg *Registry) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Handle request
		resp, err := handler(ctx, req)

		// Record metrics
		duration := time.Since(start).Seconds()
		st, _ := status.FromError(err)
		statusCode := st.Code().String()

		reg.GRPCRequestsTotal.WithLabelValues(info.FullMethod, statusCode).Inc()
		reg.GRPCRequestDuration.WithLabelValues(info.FullMethod).Observe(duration)

		return resp, err
	}
}

// StreamServerInterceptor returns a gRPC stream server interceptor that records metrics.
func StreamServerInterceptor(reg *Registry) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		// Handle stream
		err := handler(srv, ss)

		// Record metrics
		duration := time.Since(start).Seconds()
		st, _ := status.FromError(err)
		statusCode := st.Code().String()

		reg.GRPCRequestsTotal.WithLabelValues(info.FullMethod, statusCode).Inc()
		reg.GRPCRequestDuration.WithLabelValues(info.FullMethod).Observe(duration)

		return err
	}
}

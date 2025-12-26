package grpc

import (
	"context"
	"runtime/debug"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// LoggingUnaryInterceptor returns a unary server interceptor that logs request/response info.
func LoggingUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		// Extract metadata for context
		md, _ := metadata.FromIncomingContext(ctx)
		runnerID := extractMetadataValue(md, "x-runner-id")

		// Call the handler
		resp, err := handler(ctx, req)

		// Determine status code
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		duration := time.Since(start)

		// Log the request
		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.Duration("duration", duration),
			zap.String("code", code.String()),
		}
		if runnerID != "" {
			fields = append(fields, zap.String("runner_id", runnerID))
		}
		if err != nil {
			fields = append(fields, zap.Error(err))
			logger.Warn("gRPC unary call failed", fields...)
		} else {
			logger.Debug("gRPC unary call completed", fields...)
		}

		return resp, err
	}
}

// LoggingStreamInterceptor returns a stream server interceptor that logs stream open/close.
func LoggingStreamInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()

		// Extract metadata for context
		md, _ := metadata.FromIncomingContext(ss.Context())
		runnerID := extractMetadataValue(md, "x-runner-id")

		// Log stream open
		openFields := []zap.Field{
			zap.String("method", info.FullMethod),
		}
		if runnerID != "" {
			openFields = append(openFields, zap.String("runner_id", runnerID))
		}
		logger.Debug("gRPC stream opened", openFields...)

		// Call the handler
		err := handler(srv, ss)

		// Determine status code
		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		duration := time.Since(start)

		// Log stream close
		closeFields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.Duration("duration", duration),
			zap.String("code", code.String()),
		}
		if runnerID != "" {
			closeFields = append(closeFields, zap.String("runner_id", runnerID))
		}
		if err != nil {
			closeFields = append(closeFields, zap.Error(err))
			logger.Warn("gRPC stream closed with error", closeFields...)
		} else {
			logger.Debug("gRPC stream closed", closeFields...)
		}

		return err
	}
}

// RecoveryUnaryInterceptor returns a unary server interceptor that recovers from panics.
func RecoveryUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				// Log the panic with stack trace
				logger.Error("gRPC unary handler panicked",
					zap.Any("panic", r),
					zap.String("method", info.FullMethod),
					zap.String("stack", string(debug.Stack())),
				)
				// Convert panic to gRPC error
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStreamInterceptor returns a stream server interceptor that recovers from panics.
func RecoveryStreamInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		defer func() {
			if r := recover(); r != nil {
				// Log the panic with stack trace
				logger.Error("gRPC stream handler panicked",
					zap.Any("panic", r),
					zap.String("method", info.FullMethod),
					zap.String("stack", string(debug.Stack())),
				)
				// Convert panic to gRPC error
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(srv, ss)
	}
}

// extractMetadataValue extracts a single value from gRPC metadata.
func extractMetadataValue(md metadata.MD, key string) string {
	if md == nil {
		return ""
	}
	values := md.Get(key)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

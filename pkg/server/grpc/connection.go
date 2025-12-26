package grpc

import (
	"go.uber.org/zap"
)

// ConnectionManager manages active runner connections.
// Note: Full implementation will be added in a later commit.
type ConnectionManager struct {
	logger *zap.Logger
}

// NewConnectionManager creates a new ConnectionManager.
func NewConnectionManager(logger *zap.Logger) *ConnectionManager {
	return &ConnectionManager{
		logger: logger,
	}
}

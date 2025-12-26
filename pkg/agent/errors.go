// Package agent provides the marionette-agent implementation.
package agent

import (
	"errors"
	"fmt"
)

// Sentinel errors.
var (
	// ErrNotConnected indicates the client is not connected to the server.
	ErrNotConnected = errors.New("client not connected")

	// ErrAlreadyConnected indicates the client is already connected.
	ErrAlreadyConnected = errors.New("client already connected")

	// ErrShuttingDown indicates the agent is shutting down.
	ErrShuttingDown = errors.New("agent is shutting down")

	// ErrInvalidConfig indicates invalid configuration.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrMaxRetriesExceeded indicates maximum retry attempts were exceeded.
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
)

// ErrRegistrationRejected indicates the server rejected runner registration.
type ErrRegistrationRejected struct {
	Message string
}

func (e *ErrRegistrationRejected) Error() string {
	return fmt.Sprintf("registration rejected: %s", e.Message)
}

// ErrConnectionFailed indicates connection to the server failed.
type ErrConnectionFailed struct {
	Addr  string
	Cause error
}

func (e *ErrConnectionFailed) Error() string {
	return fmt.Sprintf("failed to connect to %s: %v", e.Addr, e.Cause)
}

func (e *ErrConnectionFailed) Unwrap() error {
	return e.Cause
}

// ErrHeartbeatFailed indicates a heartbeat send failure.
type ErrHeartbeatFailed struct {
	Cause error
}

func (e *ErrHeartbeatFailed) Error() string {
	return fmt.Sprintf("heartbeat failed: %v", e.Cause)
}

func (e *ErrHeartbeatFailed) Unwrap() error {
	return e.Cause
}

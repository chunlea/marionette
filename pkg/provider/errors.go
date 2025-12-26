package provider

import "fmt"

// ErrProviderNotFound is returned when a provider is not registered.
type ErrProviderNotFound struct {
	Name string
}

func (e *ErrProviderNotFound) Error() string {
	return fmt.Sprintf("provider not found: %s", e.Name)
}

// ErrRunnerNotFound is returned when a runner is not found.
type ErrRunnerNotFound struct {
	RunnerID string
}

func (e *ErrRunnerNotFound) Error() string {
	return fmt.Sprintf("runner not found: %s", e.RunnerID)
}

// ErrSpawnFailed is returned when spawning a runner fails.
type ErrSpawnFailed struct {
	Reason string
	Cause  error
}

func (e *ErrSpawnFailed) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("spawn failed: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("spawn failed: %s", e.Reason)
}

func (e *ErrSpawnFailed) Unwrap() error {
	return e.Cause
}

// ErrDestroyFailed is returned when destroying a runner fails.
type ErrDestroyFailed struct {
	RunnerID string
	Cause    error
}

func (e *ErrDestroyFailed) Error() string {
	return fmt.Sprintf("destroy failed for runner %s: %v", e.RunnerID, e.Cause)
}

func (e *ErrDestroyFailed) Unwrap() error {
	return e.Cause
}

// ErrPauseFailed is returned when pausing a runner fails.
type ErrPauseFailed struct {
	RunnerID string
	Cause    error
}

func (e *ErrPauseFailed) Error() string {
	return fmt.Sprintf("pause failed for runner %s: %v", e.RunnerID, e.Cause)
}

func (e *ErrPauseFailed) Unwrap() error {
	return e.Cause
}

// ErrUnpauseFailed is returned when unpausing a runner fails.
type ErrUnpauseFailed struct {
	RunnerID string
	Cause    error
}

func (e *ErrUnpauseFailed) Error() string {
	return fmt.Sprintf("unpause failed for runner %s: %v", e.RunnerID, e.Cause)
}

func (e *ErrUnpauseFailed) Unwrap() error {
	return e.Cause
}

// ErrInvalidConfig is returned when provider configuration is invalid.
type ErrInvalidConfig struct {
	Field  string
	Reason string
}

func (e *ErrInvalidConfig) Error() string {
	return fmt.Sprintf("invalid config: %s: %s", e.Field, e.Reason)
}

// ErrNetworkNotFound is returned when a Docker network is not found.
type ErrNetworkNotFound struct {
	Network string
}

func (e *ErrNetworkNotFound) Error() string {
	return fmt.Sprintf("network not found: %s", e.Network)
}

// ErrSuspendFailed is returned when suspending a runner fails.
type ErrSuspendFailed struct {
	RunnerID string
	Strategy SuspendStrategy
	Cause    error
}

func (e *ErrSuspendFailed) Error() string {
	return fmt.Sprintf("suspend failed for runner %s (strategy: %s): %v", e.RunnerID, e.Strategy, e.Cause)
}

func (e *ErrSuspendFailed) Unwrap() error {
	return e.Cause
}

// ErrResumeFailed is returned when resuming a runner fails.
type ErrResumeFailed struct {
	SessionID string
	Cause     error
}

func (e *ErrResumeFailed) Error() string {
	return fmt.Sprintf("resume failed for session %s: %v", e.SessionID, e.Cause)
}

func (e *ErrResumeFailed) Unwrap() error {
	return e.Cause
}

// ErrStrategyNotSupported is returned when a suspend strategy is not supported.
type ErrStrategyNotSupported struct {
	Strategy SuspendStrategy
	Provider string
}

func (e *ErrStrategyNotSupported) Error() string {
	return fmt.Sprintf("strategy %s not supported by provider %s", e.Strategy, e.Provider)
}

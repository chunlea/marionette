package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrProviderNotFound_Error(t *testing.T) {
	err := &ErrProviderNotFound{Name: "docker"}
	assert.Equal(t, "provider not found: docker", err.Error())
}

func TestErrRunnerNotFound_Error(t *testing.T) {
	err := &ErrRunnerNotFound{RunnerID: "run_123"}
	assert.Equal(t, "runner not found: run_123", err.Error())
}

func TestErrSpawnFailed_Error(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := &ErrSpawnFailed{Reason: "container create failed", Cause: cause}
		assert.Equal(t, "spawn failed: container create failed: connection refused", err.Error())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &ErrSpawnFailed{Reason: "invalid image"}
		assert.Equal(t, "spawn failed: invalid image", err.Error())
	})
}

func TestErrSpawnFailed_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &ErrSpawnFailed{Reason: "test", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestErrDestroyFailed_Error(t *testing.T) {
	cause := errors.New("container not found")
	err := &ErrDestroyFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, "destroy failed for runner run_123: container not found", err.Error())
}

func TestErrDestroyFailed_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &ErrDestroyFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestErrPauseFailed_Error(t *testing.T) {
	cause := errors.New("container not running")
	err := &ErrPauseFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, "pause failed for runner run_123: container not running", err.Error())
}

func TestErrPauseFailed_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &ErrPauseFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestErrUnpauseFailed_Error(t *testing.T) {
	cause := errors.New("container not paused")
	err := &ErrUnpauseFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, "unpause failed for runner run_123: container not paused", err.Error())
}

func TestErrUnpauseFailed_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &ErrUnpauseFailed{RunnerID: "run_123", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestErrInvalidConfig_Error(t *testing.T) {
	err := &ErrInvalidConfig{Field: "host", Reason: "must not be empty"}
	assert.Equal(t, "invalid config: host: must not be empty", err.Error())
}

func TestErrNetworkNotFound_Error(t *testing.T) {
	err := &ErrNetworkNotFound{Network: "marionette-net"}
	assert.Equal(t, "network not found: marionette-net", err.Error())
}

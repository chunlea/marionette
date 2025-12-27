package agent

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrRegistrationRejected(t *testing.T) {
	err := &ErrRegistrationRejected{Message: "invalid token"}
	assert.Equal(t, "registration rejected: invalid token", err.Error())
}

func TestErrConnectionFailed(t *testing.T) {
	cause := errors.New("connection refused")
	err := &ErrConnectionFailed{Addr: "localhost:9090", Cause: cause}

	assert.Equal(t, "failed to connect to localhost:9090: connection refused", err.Error())
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestErrHeartbeatFailed(t *testing.T) {
	cause := errors.New("stream closed")
	err := &ErrHeartbeatFailed{Cause: cause}

	assert.Equal(t, "heartbeat failed: stream closed", err.Error())
	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestSentinelErrors(t *testing.T) {
	assert.NotNil(t, ErrNotConnected)
	assert.NotNil(t, ErrAlreadyConnected)
	assert.NotNil(t, ErrShuttingDown)
	assert.NotNil(t, ErrInvalidConfig)
	assert.NotNil(t, ErrMaxRetriesExceeded)
}

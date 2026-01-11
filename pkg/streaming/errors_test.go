package streaming

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamErrors(t *testing.T) {
	// Verify all stream errors are defined and have messages
	streamErrors := []error{
		ErrStreamNotFound,
		ErrStreamExists,
		ErrStreamClosed,
		ErrInvalidStreamType,
		ErrInvalidStreamState,
		ErrInvalidStateTransition,
	}

	for _, err := range streamErrors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

func TestProviderErrors(t *testing.T) {
	providerErrors := []error{
		ErrProviderNotFound,
		ErrProviderExists,
		ErrNoProviderForType,
	}

	for _, err := range providerErrors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

func TestSessionErrors(t *testing.T) {
	sessionErrors := []error{
		ErrSessionRequired,
		ErrSessionNotFound,
		ErrRunnerNotAttached,
	}

	for _, err := range sessionErrors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

func TestSFUErrors(t *testing.T) {
	sfuErrors := []error{
		ErrRoomNotFound,
		ErrRoomClosed,
		ErrPublisherExists,
		ErrPublisherNotFound,
		ErrSubscriberExists,
		ErrSubscriberNotFound,
		ErrPeerClosed,
		ErrTrackNotFound,
		ErrTrackExists,
		ErrTrackRouterClosed,
		ErrInputChannelClosed,
	}

	for _, err := range sfuErrors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

func TestSignalingErrors(t *testing.T) {
	signalingErrors := []error{
		ErrInvalidSignalingMessage,
		ErrUnknownSignalingType,
		ErrSignalingFailed,
	}

	for _, err := range signalingErrors {
		assert.NotNil(t, err)
		assert.NotEmpty(t, err.Error())
	}
}

func TestErrorsAreDistinct(t *testing.T) {
	allErrors := []error{
		ErrStreamNotFound,
		ErrStreamExists,
		ErrStreamClosed,
		ErrInvalidStreamType,
		ErrInvalidStreamState,
		ErrInvalidStateTransition,
		ErrProviderNotFound,
		ErrProviderExists,
		ErrNoProviderForType,
		ErrSessionRequired,
		ErrSessionNotFound,
		ErrRunnerNotAttached,
		ErrRoomNotFound,
		ErrRoomClosed,
		ErrPublisherExists,
		ErrPublisherNotFound,
		ErrSubscriberExists,
		ErrSubscriberNotFound,
		ErrPeerClosed,
		ErrTrackNotFound,
		ErrTrackExists,
		ErrTrackRouterClosed,
		ErrInputChannelClosed,
		ErrInvalidSignalingMessage,
		ErrUnknownSignalingType,
		ErrSignalingFailed,
	}

	// Verify all errors are distinct
	seen := make(map[string]bool)
	for _, err := range allErrors {
		msg := err.Error()
		assert.False(t, seen[msg], "duplicate error message: %s", msg)
		seen[msg] = true
	}
}

func TestErrorsIsComparison(t *testing.T) {
	// Verify errors.Is works correctly
	wrapped := errors.Join(ErrStreamNotFound, errors.New("additional context"))
	assert.True(t, errors.Is(wrapped, ErrStreamNotFound))
	assert.False(t, errors.Is(wrapped, ErrStreamExists))
}

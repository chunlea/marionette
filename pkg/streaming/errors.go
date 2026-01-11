package streaming

import "errors"

// Stream errors.
var (
	// ErrStreamNotFound is returned when a stream is not found.
	ErrStreamNotFound = errors.New("stream not found")

	// ErrStreamExists is returned when a stream already exists.
	ErrStreamExists = errors.New("stream already exists")

	// ErrStreamClosed is returned when an operation is attempted on a closed stream.
	ErrStreamClosed = errors.New("stream is closed")

	// ErrInvalidStreamType is returned when an invalid stream type is provided.
	ErrInvalidStreamType = errors.New("invalid stream type")

	// ErrInvalidStreamState is returned when an invalid stream state is provided.
	ErrInvalidStreamState = errors.New("invalid stream state")

	// ErrInvalidStateTransition is returned when an invalid state transition is attempted.
	ErrInvalidStateTransition = errors.New("invalid state transition")
)

// Provider errors.
var (
	// ErrProviderNotFound is returned when a provider is not found.
	ErrProviderNotFound = errors.New("provider not found")

	// ErrProviderExists is returned when a provider with the same name already exists.
	ErrProviderExists = errors.New("provider already exists")

	// ErrNoProviderForType is returned when no provider supports the requested stream type.
	ErrNoProviderForType = errors.New("no provider supports this stream type")
)

// Session errors.
var (
	// ErrSessionRequired is returned when a session ID is required but not provided.
	ErrSessionRequired = errors.New("session ID is required")

	// ErrSessionNotFound is returned when a session is not found.
	ErrSessionNotFound = errors.New("session not found")

	// ErrRunnerNotAttached is returned when no runner is attached to the session.
	ErrRunnerNotAttached = errors.New("no runner attached to session")
)

// SFU errors.
var (
	// ErrRoomNotFound is returned when a room is not found.
	ErrRoomNotFound = errors.New("room not found")

	// ErrRoomExists is returned when a room with the same ID already exists.
	ErrRoomExists = errors.New("room already exists")

	// ErrRoomClosed is returned when an operation is attempted on a closed room.
	ErrRoomClosed = errors.New("room is closed")

	// ErrPublisherExists is returned when trying to set a publisher when one already exists.
	ErrPublisherExists = errors.New("publisher already exists")

	// ErrPublisherNotFound is returned when no publisher exists in the room.
	ErrPublisherNotFound = errors.New("publisher not found")

	// ErrSubscriberExists is returned when a subscriber with the same ID already exists.
	ErrSubscriberExists = errors.New("subscriber already exists")

	// ErrSubscriberNotFound is returned when a subscriber is not found.
	ErrSubscriberNotFound = errors.New("subscriber not found")

	// ErrPeerClosed is returned when an operation is attempted on a closed peer.
	ErrPeerClosed = errors.New("peer is closed")

	// ErrTrackNotFound is returned when a track is not found.
	ErrTrackNotFound = errors.New("track not found")

	// ErrTrackExists is returned when a track with the same ID already exists.
	ErrTrackExists = errors.New("track already exists")

	// ErrTrackRouterClosed is returned when an operation is attempted on a closed track router.
	ErrTrackRouterClosed = errors.New("track router is closed")

	// ErrInputChannelClosed is returned when an operation is attempted on a closed input channel.
	ErrInputChannelClosed = errors.New("input channel is closed")
)

// Signaling errors.
var (
	// ErrInvalidSignalingMessage is returned when an invalid signaling message is received.
	ErrInvalidSignalingMessage = errors.New("invalid signaling message")

	// ErrUnknownSignalingType is returned when an unknown signaling message type is received.
	ErrUnknownSignalingType = errors.New("unknown signaling message type")

	// ErrSignalingFailed is returned when signaling fails.
	ErrSignalingFailed = errors.New("signaling failed")
)

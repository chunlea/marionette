package sfu

import (
	"encoding/json"
	"fmt"

	"github.com/chunlea/marionette/pkg/streaming"
)

// SignalingMessageType defines the type of signaling message.
type SignalingMessageType string

const (
	// SignalingTypeOffer is sent when creating an SDP offer.
	SignalingTypeOffer SignalingMessageType = "offer"

	// SignalingTypeAnswer is sent in response to an offer.
	SignalingTypeAnswer SignalingMessageType = "answer"

	// SignalingTypeCandidate is sent when an ICE candidate is discovered.
	SignalingTypeCandidate SignalingMessageType = "candidate"

	// SignalingTypePing is a keepalive message.
	SignalingTypePing SignalingMessageType = "ping"

	// SignalingTypePong is a response to ping.
	SignalingTypePong SignalingMessageType = "pong"

	// SignalingTypeError indicates an error occurred.
	SignalingTypeError SignalingMessageType = "error"

	// SignalingTypeJoin is sent when a peer wants to join a room.
	SignalingTypeJoin SignalingMessageType = "join"

	// SignalingTypeLeave is sent when a peer leaves a room.
	SignalingTypeLeave SignalingMessageType = "leave"

	// SignalingTypeReady indicates the peer is ready to receive media.
	SignalingTypeReady SignalingMessageType = "ready"
)

// SignalingMessage represents a signaling message exchanged between
// clients and the SFU server.
type SignalingMessage struct {
	// Type is the message type.
	Type SignalingMessageType `json:"type"`

	// StreamID identifies the stream/room.
	StreamID string `json:"stream_id,omitempty"`

	// PeerID identifies the peer sending/receiving the message.
	PeerID string `json:"peer_id,omitempty"`

	// Role is the peer's role (publisher or subscriber).
	Role PeerRole `json:"role,omitempty"`

	// SDP contains the Session Description Protocol data for offer/answer.
	SDP string `json:"sdp,omitempty"`

	// Candidate contains ICE candidate information.
	Candidate *ICECandidateJSON `json:"candidate,omitempty"`

	// Error contains error information when Type is SignalingTypeError.
	Error string `json:"error,omitempty"`

	// ErrorCode is a machine-readable error code.
	ErrorCode string `json:"error_code,omitempty"`
}

// ICECandidateJSON represents an ICE candidate in JSON format.
// This matches the WebRTC ICECandidateInit structure.
type ICECandidateJSON struct {
	// Candidate is the candidate string.
	Candidate string `json:"candidate"`

	// SDPMid is the media stream identification tag.
	SDPMid *string `json:"sdpMid,omitempty"`

	// SDPMLineIndex is the index of the media description.
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`

	// UsernameFragment is the ICE username fragment.
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// ParseSignalingMessage parses a JSON-encoded signaling message.
func ParseSignalingMessage(data []byte) (*SignalingMessage, error) {
	var msg SignalingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("%w: %v", streaming.ErrInvalidSignalingMessage, err)
	}

	if err := msg.Validate(); err != nil {
		return nil, err
	}

	return &msg, nil
}

// ToJSON serializes the signaling message to JSON.
func (m *SignalingMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// Validate validates the signaling message.
func (m *SignalingMessage) Validate() error {
	if m.Type == "" {
		return fmt.Errorf("%w: missing type", streaming.ErrInvalidSignalingMessage)
	}

	switch m.Type {
	case SignalingTypeOffer, SignalingTypeAnswer:
		if m.SDP == "" {
			return fmt.Errorf("%w: missing SDP for %s", streaming.ErrInvalidSignalingMessage, m.Type)
		}
	case SignalingTypeCandidate:
		if m.Candidate == nil {
			return fmt.Errorf("%w: missing candidate", streaming.ErrInvalidSignalingMessage)
		}
	case SignalingTypeJoin:
		if m.StreamID == "" {
			return fmt.Errorf("%w: missing stream_id for join", streaming.ErrInvalidSignalingMessage)
		}
		if m.Role == "" {
			return fmt.Errorf("%w: missing role for join", streaming.ErrInvalidSignalingMessage)
		}
	case SignalingTypePing, SignalingTypePong, SignalingTypeError, SignalingTypeLeave, SignalingTypeReady:
		// These don't require additional fields
	default:
		return fmt.Errorf("%w: %s", streaming.ErrUnknownSignalingType, m.Type)
	}

	return nil
}

// NewOfferMessage creates a new offer signaling message.
func NewOfferMessage(streamID, peerID, sdp string) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeOffer,
		StreamID: streamID,
		PeerID:   peerID,
		SDP:      sdp,
	}
}

// NewAnswerMessage creates a new answer signaling message.
func NewAnswerMessage(streamID, peerID, sdp string) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeAnswer,
		StreamID: streamID,
		PeerID:   peerID,
		SDP:      sdp,
	}
}

// NewCandidateMessage creates a new ICE candidate signaling message.
func NewCandidateMessage(streamID, peerID string, candidate *ICECandidateJSON) *SignalingMessage {
	return &SignalingMessage{
		Type:      SignalingTypeCandidate,
		StreamID:  streamID,
		PeerID:    peerID,
		Candidate: candidate,
	}
}

// NewJoinMessage creates a new join signaling message.
func NewJoinMessage(streamID, peerID string, role PeerRole) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeJoin,
		StreamID: streamID,
		PeerID:   peerID,
		Role:     role,
	}
}

// NewLeaveMessage creates a new leave signaling message.
func NewLeaveMessage(streamID, peerID string) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeLeave,
		StreamID: streamID,
		PeerID:   peerID,
	}
}

// NewReadyMessage creates a new ready signaling message.
func NewReadyMessage(streamID, peerID string) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeReady,
		StreamID: streamID,
		PeerID:   peerID,
	}
}

// NewPingMessage creates a new ping signaling message.
func NewPingMessage() *SignalingMessage {
	return &SignalingMessage{
		Type: SignalingTypePing,
	}
}

// NewPongMessage creates a new pong signaling message.
func NewPongMessage() *SignalingMessage {
	return &SignalingMessage{
		Type: SignalingTypePong,
	}
}

// NewErrorMessage creates a new error signaling message.
func NewErrorMessage(streamID, peerID, errorCode, errorMsg string) *SignalingMessage {
	return &SignalingMessage{
		Type:      SignalingTypeError,
		StreamID:  streamID,
		PeerID:    peerID,
		ErrorCode: errorCode,
		Error:     errorMsg,
	}
}

// Error codes for signaling errors.
const (
	ErrorCodeInvalidMessage  = "invalid_message"
	ErrorCodeRoomNotFound    = "room_not_found"
	ErrorCodeRoomFull        = "room_full"
	ErrorCodePublisherExists = "publisher_exists"
	ErrorCodePeerNotFound    = "peer_not_found"
	ErrorCodeInternalError   = "internal_error"
	ErrorCodeUnauthorized    = "unauthorized"
)

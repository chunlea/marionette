package webrtc

import (
	"encoding/json"
	"errors"

	"github.com/pion/webrtc/v3"
)

// SignalMessageType identifies the type of signaling message.
type SignalMessageType string

const (
	// SignalTypeOffer is an SDP offer message.
	SignalTypeOffer SignalMessageType = "offer"
	// SignalTypeAnswer is an SDP answer message.
	SignalTypeAnswer SignalMessageType = "answer"
	// SignalTypeCandidate is an ICE candidate message.
	SignalTypeCandidate SignalMessageType = "candidate"
	// SignalTypeStreamInfo provides stream configuration.
	SignalTypeStreamInfo SignalMessageType = "stream-info"
	// SignalTypeError indicates an error.
	SignalTypeError SignalMessageType = "error"
	// SignalTypePing is a keepalive ping.
	SignalTypePing SignalMessageType = "ping"
	// SignalTypePong is a keepalive pong.
	SignalTypePong SignalMessageType = "pong"
)

// SignalMessage is the envelope for all signaling messages.
type SignalMessage struct {
	Type SignalMessageType `json:"type"`
	// Payload contains type-specific data
	Payload json.RawMessage `json:"payload,omitempty"`
}

// OfferPayload contains an SDP offer.
type OfferPayload struct {
	SDP string `json:"sdp"`
}

// AnswerPayload contains an SDP answer.
type AnswerPayload struct {
	SDP string `json:"sdp"`
}

// CandidatePayload contains an ICE candidate.
type CandidatePayload struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// StreamInfoPayload contains stream configuration.
type StreamInfoPayload struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	HasAudio   bool   `json:"hasAudio"`
	DeviceName string `json:"deviceName,omitempty"`
	StreamID   string `json:"streamId,omitempty"`
}

// ErrorPayload contains error information.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewOfferMessage creates an offer message.
func NewOfferMessage(sdp string) (*SignalMessage, error) {
	payload := OfferPayload{SDP: sdp}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &SignalMessage{
		Type:    SignalTypeOffer,
		Payload: data,
	}, nil
}

// NewAnswerMessage creates an answer message.
func NewAnswerMessage(sdp string) (*SignalMessage, error) {
	payload := AnswerPayload{SDP: sdp}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &SignalMessage{
		Type:    SignalTypeAnswer,
		Payload: data,
	}, nil
}

// NewCandidateMessage creates an ICE candidate message.
func NewCandidateMessage(candidate *webrtc.ICECandidate) (*SignalMessage, error) {
	if candidate == nil {
		return nil, errors.New("candidate is nil")
	}

	init := candidate.ToJSON()
	payload := CandidatePayload{
		Candidate:        init.Candidate,
		SDPMid:           init.SDPMid,
		SDPMLineIndex:    init.SDPMLineIndex,
		UsernameFragment: init.UsernameFragment,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &SignalMessage{
		Type:    SignalTypeCandidate,
		Payload: data,
	}, nil
}

// NewStreamInfoMessage creates a stream info message.
func NewStreamInfoMessage(info StreamInfoPayload) (*SignalMessage, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	return &SignalMessage{
		Type:    SignalTypeStreamInfo,
		Payload: data,
	}, nil
}

// NewErrorMessage creates an error message.
func NewErrorMessage(code, message string) (*SignalMessage, error) {
	payload := ErrorPayload{Code: code, Message: message}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &SignalMessage{
		Type:    SignalTypeError,
		Payload: data,
	}, nil
}

// NewPingMessage creates a ping message.
func NewPingMessage() *SignalMessage {
	return &SignalMessage{Type: SignalTypePing}
}

// NewPongMessage creates a pong message.
func NewPongMessage() *SignalMessage {
	return &SignalMessage{Type: SignalTypePong}
}

// ParseOffer extracts an offer payload from a message.
func (m *SignalMessage) ParseOffer() (*OfferPayload, error) {
	if m.Type != SignalTypeOffer {
		return nil, errors.New("not an offer message")
	}
	var payload OfferPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseAnswer extracts an answer payload from a message.
func (m *SignalMessage) ParseAnswer() (*AnswerPayload, error) {
	if m.Type != SignalTypeAnswer {
		return nil, errors.New("not an answer message")
	}
	var payload AnswerPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseCandidate extracts a candidate payload from a message.
func (m *SignalMessage) ParseCandidate() (*CandidatePayload, error) {
	if m.Type != SignalTypeCandidate {
		return nil, errors.New("not a candidate message")
	}
	var payload CandidatePayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseStreamInfo extracts a stream info payload from a message.
func (m *SignalMessage) ParseStreamInfo() (*StreamInfoPayload, error) {
	if m.Type != SignalTypeStreamInfo {
		return nil, errors.New("not a stream-info message")
	}
	var payload StreamInfoPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseError extracts an error payload from a message.
func (m *SignalMessage) ParseError() (*ErrorPayload, error) {
	if m.Type != SignalTypeError {
		return nil, errors.New("not an error message")
	}
	var payload ErrorPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ToICECandidateInit converts a CandidatePayload to webrtc.ICECandidateInit.
func (c *CandidatePayload) ToICECandidateInit() webrtc.ICECandidateInit {
	return webrtc.ICECandidateInit{
		Candidate:        c.Candidate,
		SDPMid:           c.SDPMid,
		SDPMLineIndex:    c.SDPMLineIndex,
		UsernameFragment: c.UsernameFragment,
	}
}

// ToSessionDescription converts an OfferPayload to webrtc.SessionDescription.
func (o *OfferPayload) ToSessionDescription() webrtc.SessionDescription {
	return webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  o.SDP,
	}
}

// ToSessionDescription converts an AnswerPayload to webrtc.SessionDescription.
func (a *AnswerPayload) ToSessionDescription() webrtc.SessionDescription {
	return webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  a.SDP,
	}
}

// Marshal serializes a SignalMessage to JSON.
func (m *SignalMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// UnmarshalSignalMessage deserializes a SignalMessage from JSON.
func UnmarshalSignalMessage(data []byte) (*SignalMessage, error) {
	var msg SignalMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ErrorCodes for signaling errors.
const (
	ErrorCodeInvalidMessage   = "invalid_message"
	ErrorCodeInvalidSDP       = "invalid_sdp"
	ErrorCodeInvalidCandidate = "invalid_candidate"
	ErrorCodeStreamNotFound   = "stream_not_found"
	ErrorCodePeerFailed       = "peer_failed"
	ErrorCodeInternalError    = "internal_error"
)

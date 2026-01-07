package sfu

import (
	"encoding/json"
	"fmt"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

// SignalingMessageType defines the type of signaling message.
type SignalingMessageType string

const (
	// SignalingTypeOffer is an SDP offer message.
	SignalingTypeOffer SignalingMessageType = "offer"

	// SignalingTypeAnswer is an SDP answer message.
	SignalingTypeAnswer SignalingMessageType = "answer"

	// SignalingTypeCandidate is an ICE candidate message.
	SignalingTypeCandidate SignalingMessageType = "candidate"

	// SignalingTypeError is an error message.
	SignalingTypeError SignalingMessageType = "error"
)

// SignalingMessage represents a WebRTC signaling message.
type SignalingMessage struct {
	Type      SignalingMessageType `json:"type"`
	StreamID  string               `json:"stream_id,omitempty"`
	PeerID    string               `json:"peer_id,omitempty"`
	SDP       string               `json:"sdp,omitempty"`
	Candidate *ICECandidateJSON    `json:"candidate,omitempty"`
	Error     string               `json:"error,omitempty"`
}

// ICECandidateJSON represents an ICE candidate in JSON format.
type ICECandidateJSON struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// ToWebRTC converts the JSON representation to a webrtc.ICECandidateInit.
func (c *ICECandidateJSON) ToWebRTC() webrtc.ICECandidateInit {
	init := webrtc.ICECandidateInit{
		Candidate: c.Candidate,
	}
	if c.SDPMid != nil {
		init.SDPMid = c.SDPMid
	}
	if c.SDPMLineIndex != nil {
		init.SDPMLineIndex = c.SDPMLineIndex
	}
	if c.UsernameFragment != nil {
		init.UsernameFragment = c.UsernameFragment
	}
	return init
}

// ICECandidateFromWebRTC converts a webrtc.ICECandidate to JSON format.
func ICECandidateFromWebRTC(c *webrtc.ICECandidate) *ICECandidateJSON {
	if c == nil {
		return nil
	}
	json := c.ToJSON()
	return &ICECandidateJSON{
		Candidate:        json.Candidate,
		SDPMid:           json.SDPMid,
		SDPMLineIndex:    json.SDPMLineIndex,
		UsernameFragment: json.UsernameFragment,
	}
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
func NewCandidateMessage(streamID, peerID string, candidate *webrtc.ICECandidate) *SignalingMessage {
	return &SignalingMessage{
		Type:      SignalingTypeCandidate,
		StreamID:  streamID,
		PeerID:    peerID,
		Candidate: ICECandidateFromWebRTC(candidate),
	}
}

// NewErrorMessage creates a new error signaling message.
func NewErrorMessage(streamID, peerID, errMsg string) *SignalingMessage {
	return &SignalingMessage{
		Type:     SignalingTypeError,
		StreamID: streamID,
		PeerID:   peerID,
		Error:    errMsg,
	}
}

// ParseSignalingMessage parses a JSON signaling message.
func ParseSignalingMessage(data []byte) (*SignalingMessage, error) {
	var msg SignalingMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("parsing signaling message: %w", err)
	}
	return &msg, nil
}

// ToJSON serializes the signaling message to JSON.
func (m *SignalingMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// SignalingHandler handles signaling messages for WebRTC connections.
type SignalingHandler struct {
	sfu *SFU

	// OnSend is called when a signaling message needs to be sent to a peer.
	// The implementation should route this to the appropriate WebSocket connection.
	OnSend func(peerID string, msg *SignalingMessage) error
}

// NewSignalingHandler creates a new SignalingHandler.
func NewSignalingHandler(sfu *SFU) *SignalingHandler {
	return &SignalingHandler{
		sfu: sfu,
	}
}

// HandleMessage processes an incoming signaling message.
func (h *SignalingHandler) HandleMessage(msg *SignalingMessage) error {
	switch msg.Type {
	case SignalingTypeOffer:
		return h.handleOffer(msg)
	case SignalingTypeAnswer:
		return h.handleAnswer(msg)
	case SignalingTypeCandidate:
		return h.handleCandidate(msg)
	default:
		return fmt.Errorf("unknown signaling message type: %s", msg.Type)
	}
}

// handleOffer processes an SDP offer from a peer.
// For publishers (Selkies): We receive their offer and send an answer.
// For subscribers (browsers): This shouldn't happen in SFU mode (SFU creates offers).
func (h *SignalingHandler) handleOffer(msg *SignalingMessage) error {
	room, ok := h.sfu.GetRoom(msg.StreamID)
	if !ok {
		return fmt.Errorf("room not found: %s", msg.StreamID)
	}

	// Get the peer (should be the publisher for offers)
	publisher := room.GetPublisher()
	if publisher == nil || publisher.ID != msg.PeerID {
		return fmt.Errorf("unexpected offer from peer %s (not the publisher)", msg.PeerID)
	}

	// Set remote description (the offer)
	sdp := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	}
	if err := publisher.SetRemoteDescription(sdp); err != nil {
		return fmt.Errorf("setting remote description: %w", err)
	}

	// Create answer
	answer, err := publisher.CreateAnswer()
	if err != nil {
		return fmt.Errorf("creating answer: %w", err)
	}

	// Send answer back
	if h.OnSend != nil {
		answerMsg := NewAnswerMessage(msg.StreamID, msg.PeerID, answer.SDP)
		if err := h.OnSend(msg.PeerID, answerMsg); err != nil {
			return fmt.Errorf("sending answer: %w", err)
		}
	}

	return nil
}

// handleAnswer processes an SDP answer from a peer.
// For subscribers (browsers): They respond to our offer with an answer.
func (h *SignalingHandler) handleAnswer(msg *SignalingMessage) error {
	room, ok := h.sfu.GetRoom(msg.StreamID)
	if !ok {
		return fmt.Errorf("room not found: %s", msg.StreamID)
	}

	// Get the subscriber peer
	peer, ok := room.GetSubscriber(msg.PeerID)
	if !ok {
		// Could also be publisher responding to our offer in some flows
		publisher := room.GetPublisher()
		if publisher != nil && publisher.ID == msg.PeerID {
			peer = publisher
		} else {
			return fmt.Errorf("peer not found: %s", msg.PeerID)
		}
	}

	// Set remote description (the answer)
	sdp := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.SDP,
	}
	if err := peer.SetRemoteDescription(sdp); err != nil {
		return fmt.Errorf("setting remote description: %w", err)
	}

	return nil
}

// handleCandidate processes an ICE candidate from a peer.
func (h *SignalingHandler) handleCandidate(msg *SignalingMessage) error {
	if msg.Candidate == nil {
		return nil // End of candidates
	}

	room, ok := h.sfu.GetRoom(msg.StreamID)
	if !ok {
		return fmt.Errorf("room not found: %s", msg.StreamID)
	}

	// Find the peer
	var peer *Peer
	if sub, ok := room.GetSubscriber(msg.PeerID); ok {
		peer = sub
	} else if pub := room.GetPublisher(); pub != nil && pub.ID == msg.PeerID {
		peer = pub
	} else {
		return fmt.Errorf("peer not found: %s", msg.PeerID)
	}

	// Add ICE candidate
	candidate := msg.Candidate.ToWebRTC()
	if err := peer.AddICECandidate(candidate); err != nil {
		return fmt.Errorf("adding ICE candidate: %w", err)
	}

	return nil
}

// SetupPeerCallbacks sets up the ICE candidate callback for a peer.
func (h *SignalingHandler) SetupPeerCallbacks(streamID string, peer *Peer) {
	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil || h.OnSend == nil {
			return
		}
		msg := NewCandidateMessage(streamID, peer.ID, candidate)
		if err := h.OnSend(peer.ID, msg); err != nil {
			h.sfu.logger.Error("failed to send ICE candidate",
				zap.String("peer_id", peer.ID),
				zap.Error(err),
			)
		}
	})
}

// CreateOfferForSubscriber creates an SDP offer for a new subscriber.
// The subscriber should respond with an answer.
func (h *SignalingHandler) CreateOfferForSubscriber(streamID string, peer *Peer) (*SignalingMessage, error) {
	// Set up ICE candidate forwarding
	h.SetupPeerCallbacks(streamID, peer)

	// Create offer
	offer, err := peer.CreateOffer()
	if err != nil {
		return nil, fmt.Errorf("creating offer: %w", err)
	}

	return NewOfferMessage(streamID, peer.ID, offer.SDP), nil
}

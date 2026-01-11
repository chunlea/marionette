package sfu

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// SignalingHandler handles signaling messages for WebRTC connections.
type SignalingHandler struct {
	sfu    *SFU
	logger *zap.Logger

	mu       sync.RWMutex
	onSend   func(streamID, peerID string, msg *SignalingMessage)
	sessions map[string]*peerSession // peerID -> session
}

// peerSession tracks the state of a peer's signaling session.
type peerSession struct {
	streamID string
	peerID   string
	role     PeerRole
	peer     *Peer
}

// NewSignalingHandler creates a new SignalingHandler.
func NewSignalingHandler(sfu *SFU, logger *zap.Logger) *SignalingHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &SignalingHandler{
		sfu:      sfu,
		logger:   logger.Named("signaling"),
		sessions: make(map[string]*peerSession),
	}
}

// OnSend sets the callback for sending messages to peers.
// The callback is invoked when the handler needs to send a message
// to a specific peer (identified by streamID and peerID).
func (h *SignalingHandler) OnSend(callback func(streamID, peerID string, msg *SignalingMessage)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSend = callback
}

// send sends a signaling message to a peer.
func (h *SignalingHandler) send(streamID, peerID string, msg *SignalingMessage) {
	h.mu.RLock()
	callback := h.onSend
	h.mu.RUnlock()

	if callback != nil {
		callback(streamID, peerID, msg)
	}
}

// HandleMessage processes a signaling message.
func (h *SignalingHandler) HandleMessage(ctx context.Context, msg *SignalingMessage) error {
	h.logger.Debug("handling message",
		zap.String("type", string(msg.Type)),
		zap.String("stream_id", msg.StreamID),
		zap.String("peer_id", msg.PeerID),
	)

	switch msg.Type {
	case SignalingTypeJoin:
		return h.handleJoin(ctx, msg)
	case SignalingTypeLeave:
		return h.handleLeave(ctx, msg)
	case SignalingTypeOffer:
		return h.handleOffer(ctx, msg)
	case SignalingTypeAnswer:
		return h.handleAnswer(ctx, msg)
	case SignalingTypeCandidate:
		return h.handleCandidate(ctx, msg)
	case SignalingTypePing:
		return h.handlePing(ctx, msg)
	default:
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInvalidMessage,
			fmt.Sprintf("unknown message type: %s", msg.Type))
		return fmt.Errorf("%w: %s", streaming.ErrUnknownSignalingType, msg.Type)
	}
}

// handleJoin handles a join request from a peer.
func (h *SignalingHandler) handleJoin(ctx context.Context, msg *SignalingMessage) error {
	h.logger.Info("peer joining",
		zap.String("stream_id", msg.StreamID),
		zap.String("peer_id", msg.PeerID),
		zap.String("role", string(msg.Role)),
	)

	// Get or create the room
	room, err := h.sfu.GetOrCreateRoom(msg.StreamID)
	if err != nil {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
		return err
	}

	var peer *Peer

	switch msg.Role {
	case PeerRolePublisher:
		peer, err = room.SetPublisher(ctx, msg.PeerID)
		if err != nil {
			if err == streaming.ErrPublisherExists {
				h.sendError(msg.StreamID, msg.PeerID, ErrorCodePublisherExists, "publisher already exists")
			} else {
				h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
			}
			return err
		}

	case PeerRoleSubscriber:
		peer, err = room.AddSubscriber(ctx, msg.PeerID)
		if err != nil {
			if err == streaming.ErrSubscriberExists {
				h.sendError(msg.StreamID, msg.PeerID, ErrorCodeRoomFull, "subscriber already exists")
			} else {
				h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
			}
			return err
		}

	default:
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInvalidMessage,
			fmt.Sprintf("invalid role: %s", msg.Role))
		return fmt.Errorf("invalid role: %s", msg.Role)
	}

	// Set up callbacks
	h.setupPeerCallbacks(msg.StreamID, peer)

	// Track the session
	h.mu.Lock()
	h.sessions[msg.PeerID] = &peerSession{
		streamID: msg.StreamID,
		peerID:   msg.PeerID,
		role:     msg.Role,
		peer:     peer,
	}
	h.mu.Unlock()

	// For subscribers, create an offer
	if msg.Role == PeerRoleSubscriber {
		if err := h.createOfferForSubscriber(ctx, msg.StreamID, msg.PeerID, peer); err != nil {
			h.logger.Error("failed to create offer for subscriber",
				zap.String("peer_id", msg.PeerID),
				zap.Error(err),
			)
		}
	}

	// Send ready message
	h.send(msg.StreamID, msg.PeerID, NewReadyMessage(msg.StreamID, msg.PeerID))

	return nil
}

// handleLeave handles a leave request from a peer.
func (h *SignalingHandler) handleLeave(ctx context.Context, msg *SignalingMessage) error {
	h.logger.Info("peer leaving",
		zap.String("stream_id", msg.StreamID),
		zap.String("peer_id", msg.PeerID),
	)

	h.mu.Lock()
	session, ok := h.sessions[msg.PeerID]
	if ok {
		delete(h.sessions, msg.PeerID)
	}
	h.mu.Unlock()

	if !ok {
		return nil // Already left or never joined
	}

	room, ok := h.sfu.GetRoom(session.streamID)
	if !ok {
		return nil // Room already closed
	}

	if session.role == PeerRoleSubscriber {
		return room.RemoveSubscriber(ctx, msg.PeerID)
	}

	// For publishers, close the entire room
	return h.sfu.RemoveRoom(ctx, session.streamID)
}

// handleOffer handles an SDP offer from a publisher.
func (h *SignalingHandler) handleOffer(ctx context.Context, msg *SignalingMessage) error {
	h.mu.RLock()
	session, ok := h.sessions[msg.PeerID]
	h.mu.RUnlock()

	if !ok {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodePeerNotFound, "peer not found, join first")
		return streaming.ErrSubscriberNotFound
	}

	// Set remote description
	sdp := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  msg.SDP,
	}
	if err := session.peer.SetRemoteDescription(sdp); err != nil {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
		return err
	}

	// Create answer
	answer, err := session.peer.CreateAnswer()
	if err != nil {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
		return err
	}

	// Send answer
	h.send(msg.StreamID, msg.PeerID, NewAnswerMessage(msg.StreamID, msg.PeerID, answer.SDP))

	return nil
}

// handleAnswer handles an SDP answer from a subscriber.
func (h *SignalingHandler) handleAnswer(ctx context.Context, msg *SignalingMessage) error {
	h.mu.RLock()
	session, ok := h.sessions[msg.PeerID]
	h.mu.RUnlock()

	if !ok {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodePeerNotFound, "peer not found")
		return streaming.ErrSubscriberNotFound
	}

	// Set remote description
	sdp := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  msg.SDP,
	}
	if err := session.peer.SetRemoteDescription(sdp); err != nil {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodeInternalError, err.Error())
		return err
	}

	return nil
}

// handleCandidate handles an ICE candidate from a peer.
func (h *SignalingHandler) handleCandidate(ctx context.Context, msg *SignalingMessage) error {
	h.mu.RLock()
	session, ok := h.sessions[msg.PeerID]
	h.mu.RUnlock()

	if !ok {
		h.sendError(msg.StreamID, msg.PeerID, ErrorCodePeerNotFound, "peer not found")
		return streaming.ErrSubscriberNotFound
	}

	if msg.Candidate == nil {
		return nil // End of candidates
	}

	candidate := webrtc.ICECandidateInit{
		Candidate:        msg.Candidate.Candidate,
		SDPMid:           msg.Candidate.SDPMid,
		SDPMLineIndex:    msg.Candidate.SDPMLineIndex,
		UsernameFragment: msg.Candidate.UsernameFragment,
	}

	if err := session.peer.AddICECandidate(candidate); err != nil {
		h.logger.Warn("failed to add ICE candidate",
			zap.String("peer_id", msg.PeerID),
			zap.Error(err),
		)
		// Don't send error for ICE candidate failures - they can be normal
		return err
	}

	return nil
}

// handlePing handles a ping message.
func (h *SignalingHandler) handlePing(ctx context.Context, msg *SignalingMessage) error {
	h.send(msg.StreamID, msg.PeerID, NewPongMessage())
	return nil
}

// setupPeerCallbacks sets up the necessary callbacks on a peer.
func (h *SignalingHandler) setupPeerCallbacks(streamID string, peer *Peer) {
	// ICE candidate callback
	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}

		candidateJSON := candidate.ToJSON()
		h.send(streamID, peer.ID, NewCandidateMessage(streamID, peer.ID, &ICECandidateJSON{
			Candidate:        candidateJSON.Candidate,
			SDPMid:           candidateJSON.SDPMid,
			SDPMLineIndex:    candidateJSON.SDPMLineIndex,
			UsernameFragment: candidateJSON.UsernameFragment,
		}))
	})

	// State change callback
	peer.OnStateChange(func(state PeerState) {
		h.logger.Debug("peer state changed",
			zap.String("stream_id", streamID),
			zap.String("peer_id", peer.ID),
			zap.String("state", string(state)),
		)

		if state == PeerStateFailed || state == PeerStateDisconnected || state == PeerStateClosed {
			h.cleanupPeer(peer.ID)
		}
	})
}

// createOfferForSubscriber creates an SDP offer for a new subscriber.
func (h *SignalingHandler) createOfferForSubscriber(ctx context.Context, streamID, peerID string, peer *Peer) error {
	offer, err := peer.CreateOffer()
	if err != nil {
		return err
	}

	h.send(streamID, peerID, NewOfferMessage(streamID, peerID, offer.SDP))
	return nil
}

// CreateOfferForSubscriber creates an offer for a subscriber.
// This is useful when tracks are added after the initial connection.
func (h *SignalingHandler) CreateOfferForSubscriber(ctx context.Context, streamID, peerID string) (*SignalingMessage, error) {
	h.mu.RLock()
	session, ok := h.sessions[peerID]
	h.mu.RUnlock()

	if !ok {
		return nil, streaming.ErrSubscriberNotFound
	}

	offer, err := session.peer.CreateOffer()
	if err != nil {
		return nil, err
	}

	return NewOfferMessage(streamID, peerID, offer.SDP), nil
}

// sendError sends an error message to a peer.
func (h *SignalingHandler) sendError(streamID, peerID, errorCode, errorMsg string) {
	h.send(streamID, peerID, NewErrorMessage(streamID, peerID, errorCode, errorMsg))
}

// cleanupPeer removes a peer from tracking.
func (h *SignalingHandler) cleanupPeer(peerID string) {
	h.mu.Lock()
	delete(h.sessions, peerID)
	h.mu.Unlock()
}

// GetSession returns the session for a peer.
func (h *SignalingHandler) GetSession(peerID string) (*peerSession, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	session, ok := h.sessions[peerID]
	return session, ok
}

// SessionCount returns the number of active sessions.
func (h *SignalingHandler) SessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// Close closes all sessions.
func (h *SignalingHandler) Close() {
	h.mu.Lock()
	h.sessions = make(map[string]*peerSession)
	h.mu.Unlock()
}

package webrtc

import (
	"context"
	"errors"
	"sync"
	"time"

	pionwebrtc "github.com/pion/webrtc/v3"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/id"
)

// SessionState represents the state of a streaming session.
type SessionState string

const (
	// SessionStateNew indicates a newly created session.
	SessionStateNew SessionState = "new"
	// SessionStateConnecting indicates the session is connecting.
	SessionStateConnecting SessionState = "connecting"
	// SessionStateActive indicates the session is actively streaming.
	SessionStateActive SessionState = "active"
	// SessionStateClosed indicates the session has been closed.
	SessionStateClosed SessionState = "closed"
)

// Session manages a WebRTC streaming session including peer connection
// and signaling state.
type Session struct {
	id       string
	streamID string
	config   Config
	peer     *Peer
	relay    *Relay
	logger   *zap.Logger

	// State
	state   SessionState
	stateMu sync.RWMutex

	// Signaling
	pendingCandidates []pionwebrtc.ICECandidateInit
	candidatesMu      sync.Mutex
	remoteDescSet     bool

	// Callbacks
	onSignal      func(*SignalMessage)
	onStateChange func(SessionState)

	// Lifecycle
	closed  bool
	closeMu sync.RWMutex
	doneCh  chan struct{}

	createdAt time.Time
}

// SessionOption configures a Session.
type SessionOption func(*Session)

// WithStreamID sets the stream ID for the session.
func WithStreamID(streamID string) SessionOption {
	return func(s *Session) {
		s.streamID = streamID
	}
}

// WithOnSignal sets the callback for outgoing signaling messages.
func WithOnSignal(fn func(*SignalMessage)) SessionOption {
	return func(s *Session) {
		s.onSignal = fn
	}
}

// WithSessionStateChange sets the callback for session state changes.
func WithSessionStateChange(fn func(SessionState)) SessionOption {
	return func(s *Session) {
		s.onStateChange = fn
	}
}

// NewSession creates a new WebRTC streaming session.
func NewSession(config Config, relay *Relay, opts ...SessionOption) (*Session, error) {
	config = config.WithDefaults()

	sessionID := id.New("wsess")

	session := &Session{
		id:                sessionID,
		config:            config,
		relay:             relay,
		logger:            config.Logger.Named("session").With(zap.String("session_id", sessionID)),
		state:             SessionStateNew,
		pendingCandidates: make([]pionwebrtc.ICECandidateInit, 0),
		doneCh:            make(chan struct{}),
		createdAt:         time.Now(),
	}

	// Apply options
	for _, opt := range opts {
		opt(session)
	}

	// Create peer connection
	peer, err := NewPeer(sessionID, config,
		WithOnStateChange(session.handlePeerStateChange),
		WithOnICECandidate(session.handleICECandidate),
	)
	if err != nil {
		return nil, err
	}

	session.peer = peer

	// Add peer to relay
	if relay != nil {
		if err := relay.AddPeer(peer); err != nil {
			_ = peer.Close()
			return nil, err
		}
	}

	session.logger.Info("session created")
	return session, nil
}

// ID returns the session ID.
func (s *Session) ID() string {
	return s.id
}

// StreamID returns the associated stream ID.
func (s *Session) StreamID() string {
	return s.streamID
}

// State returns the current session state.
func (s *Session) State() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

// Peer returns the underlying peer connection.
func (s *Session) Peer() *Peer {
	return s.peer
}

// CreatedAt returns the session creation time.
func (s *Session) CreatedAt() time.Time {
	return s.createdAt
}

// HandleSignal processes an incoming signaling message.
func (s *Session) HandleSignal(msg *SignalMessage) error {
	s.closeMu.RLock()
	if s.closed {
		s.closeMu.RUnlock()
		return errors.New("session is closed")
	}
	s.closeMu.RUnlock()

	s.logger.Debug("handling signal", zap.String("type", string(msg.Type)))

	switch msg.Type {
	case SignalTypeOffer:
		return s.handleOffer(msg)
	case SignalTypeAnswer:
		return s.handleAnswer(msg)
	case SignalTypeCandidate:
		return s.handleCandidate(msg)
	case SignalTypePing:
		s.sendSignal(NewPongMessage())
		return nil
	default:
		return errors.New("unknown signal type")
	}
}

// handleOffer processes an SDP offer from the client.
func (s *Session) handleOffer(msg *SignalMessage) error {
	offer, err := msg.ParseOffer()
	if err != nil {
		s.sendError(ErrorCodeInvalidSDP, "failed to parse offer")
		return err
	}

	// Set remote description
	if err := s.peer.SetRemoteDescription(offer.ToSessionDescription()); err != nil {
		s.sendError(ErrorCodeInvalidSDP, "failed to set remote description")
		return err
	}

	s.candidatesMu.Lock()
	s.remoteDescSet = true
	pending := s.pendingCandidates
	s.pendingCandidates = nil
	s.candidatesMu.Unlock()

	// Add any pending candidates
	for _, candidate := range pending {
		if err := s.peer.AddICECandidate(candidate); err != nil {
			s.logger.Warn("failed to add pending candidate", zap.Error(err))
		}
	}

	// Create answer
	answer, err := s.peer.CreateAnswer()
	if err != nil {
		s.sendError(ErrorCodeInternalError, "failed to create answer")
		return err
	}

	// Send answer
	answerMsg, err := NewAnswerMessage(answer.SDP)
	if err != nil {
		return err
	}
	s.sendSignal(answerMsg)

	// Send stream info if available
	if s.relay != nil {
		width, height, videoCodec, _ := s.relay.VideoConfig()
		_, _, audioCodec, _ := s.relay.AudioConfig()

		if width > 0 && height > 0 {
			info := StreamInfoPayload{
				Width:      width,
				Height:     height,
				VideoCodec: videoCodec,
				AudioCodec: audioCodec,
				HasAudio:   audioCodec != "",
				StreamID:   s.streamID,
			}
			if infoMsg, err := NewStreamInfoMessage(info); err == nil {
				s.sendSignal(infoMsg)
			}
		}
	}

	s.setState(SessionStateConnecting)
	return nil
}

// handleAnswer processes an SDP answer from the client.
func (s *Session) handleAnswer(msg *SignalMessage) error {
	answer, err := msg.ParseAnswer()
	if err != nil {
		s.sendError(ErrorCodeInvalidSDP, "failed to parse answer")
		return err
	}

	if err := s.peer.SetRemoteDescription(answer.ToSessionDescription()); err != nil {
		s.sendError(ErrorCodeInvalidSDP, "failed to set remote description")
		return err
	}

	s.candidatesMu.Lock()
	s.remoteDescSet = true
	pending := s.pendingCandidates
	s.pendingCandidates = nil
	s.candidatesMu.Unlock()

	// Add any pending candidates
	for _, candidate := range pending {
		if err := s.peer.AddICECandidate(candidate); err != nil {
			s.logger.Warn("failed to add pending candidate", zap.Error(err))
		}
	}

	s.setState(SessionStateConnecting)
	return nil
}

// handleCandidate processes an ICE candidate from the client.
func (s *Session) handleCandidate(msg *SignalMessage) error {
	candidate, err := msg.ParseCandidate()
	if err != nil {
		s.sendError(ErrorCodeInvalidCandidate, "failed to parse candidate")
		return err
	}

	init := candidate.ToICECandidateInit()

	s.candidatesMu.Lock()
	if !s.remoteDescSet {
		// Queue candidate until remote description is set
		s.pendingCandidates = append(s.pendingCandidates, init)
		s.candidatesMu.Unlock()
		return nil
	}
	s.candidatesMu.Unlock()

	if err := s.peer.AddICECandidate(init); err != nil {
		s.logger.Warn("failed to add ICE candidate", zap.Error(err))
		return err
	}

	return nil
}

// CreateOffer creates an SDP offer for server-initiated signaling.
func (s *Session) CreateOffer() error {
	s.closeMu.RLock()
	if s.closed {
		s.closeMu.RUnlock()
		return errors.New("session is closed")
	}
	s.closeMu.RUnlock()

	offer, err := s.peer.CreateOffer()
	if err != nil {
		return err
	}

	offerMsg, err := NewOfferMessage(offer.SDP)
	if err != nil {
		return err
	}
	s.sendSignal(offerMsg)

	s.setState(SessionStateConnecting)
	return nil
}

// WaitForConnection waits for the session to reach active state.
func (s *Session) WaitForConnection(ctx context.Context) error {
	return s.peer.WaitForConnection(ctx)
}

// Close closes the session.
func (s *Session) Close() error {
	s.closeMu.Lock()
	if s.closed {
		s.closeMu.Unlock()
		return nil
	}
	s.closed = true
	close(s.doneCh)
	s.closeMu.Unlock()

	// Remove peer from relay
	if s.relay != nil {
		s.relay.RemovePeer(s.id)
	}

	// Close peer connection
	if err := s.peer.Close(); err != nil {
		s.logger.Error("error closing peer", zap.Error(err))
	}

	s.setState(SessionStateClosed)
	s.logger.Info("session closed")

	return nil
}

// Done returns a channel that's closed when the session is closed.
func (s *Session) Done() <-chan struct{} {
	return s.doneCh
}

// handlePeerStateChange handles peer connection state changes.
func (s *Session) handlePeerStateChange(state PeerState) {
	s.logger.Debug("peer state changed", zap.String("state", string(state)))

	switch state {
	case PeerStateConnected:
		s.setState(SessionStateActive)
	case PeerStateDisconnected, PeerStateFailed:
		s.setState(SessionStateClosed)
		_ = s.Close()
	case PeerStateClosed:
		s.setState(SessionStateClosed)
	}
}

// handleICECandidate handles local ICE candidates.
func (s *Session) handleICECandidate(candidate *pionwebrtc.ICECandidate) {
	msg, err := NewCandidateMessage(candidate)
	if err != nil {
		s.logger.Warn("failed to create candidate message", zap.Error(err))
		return
	}
	s.sendSignal(msg)
}

// sendSignal sends a signaling message to the client.
func (s *Session) sendSignal(msg *SignalMessage) {
	if s.onSignal != nil {
		s.onSignal(msg)
	}
}

// sendError sends an error message to the client.
func (s *Session) sendError(code, message string) {
	msg, err := NewErrorMessage(code, message)
	if err != nil {
		s.logger.Error("failed to create error message", zap.Error(err))
		return
	}
	s.sendSignal(msg)
}

// setState updates the session state.
func (s *Session) setState(state SessionState) {
	s.stateMu.Lock()
	if s.state == state {
		s.stateMu.Unlock()
		return
	}
	oldState := s.state
	s.state = state
	s.stateMu.Unlock()

	s.logger.Info("session state changed",
		zap.String("old_state", string(oldState)),
		zap.String("new_state", string(state)),
	)

	if s.onStateChange != nil {
		s.onStateChange(state)
	}
}

// SessionManager manages multiple WebRTC sessions.
type SessionManager struct {
	config   Config
	relay    *Relay
	logger   *zap.Logger
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager(config Config, relay *Relay) *SessionManager {
	config = config.WithDefaults()

	return &SessionManager{
		config:   config,
		relay:    relay,
		logger:   config.Logger.Named("session-manager"),
		sessions: make(map[string]*Session),
	}
}

// CreateSession creates a new session.
func (m *SessionManager) CreateSession(opts ...SessionOption) (*Session, error) {
	session, err := NewSession(m.config, m.relay, opts...)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	m.logger.Info("session created",
		zap.String("session_id", session.ID()),
		zap.Int("total_sessions", m.SessionCount()),
	)

	return session, nil
}

// GetSession returns a session by ID.
func (m *SessionManager) GetSession(sessionID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[sessionID]
}

// RemoveSession removes and closes a session.
func (m *SessionManager) RemoveSession(sessionID string) {
	m.mu.Lock()
	session, exists := m.sessions[sessionID]
	if exists {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()

	if session != nil {
		_ = session.Close()
		m.logger.Info("session removed",
			zap.String("session_id", sessionID),
			zap.Int("total_sessions", m.SessionCount()),
		)
	}
}

// SessionCount returns the number of active sessions.
func (m *SessionManager) SessionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Close closes all sessions.
func (m *SessionManager) Close() error {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, session := range sessions {
		_ = session.Close()
	}

	m.logger.Info("session manager closed")
	return nil
}

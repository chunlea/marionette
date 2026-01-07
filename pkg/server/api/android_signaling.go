package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	rtcwebrtc "github.com/chunlea/marionette/pkg/streaming/webrtc"
)

// AndroidSignalingService defines operations for WebRTC signaling.
type AndroidSignalingService interface {
	// GetSession returns a WebRTC session for the given stream ID.
	GetSession(ctx context.Context, streamID string) (*rtcwebrtc.Session, error)

	// CreatePeer creates a new WebRTC peer for the given stream.
	CreatePeer(ctx context.Context, streamID, peerID string) error

	// RemovePeer removes a WebRTC peer from the stream.
	RemovePeer(ctx context.Context, streamID, peerID string) error

	// HandleOffer processes an SDP offer from a client.
	HandleOffer(ctx context.Context, streamID, peerID string, sdp string) (string, error)

	// HandleAnswer processes an SDP answer from a client.
	HandleAnswer(ctx context.Context, streamID, peerID string, sdp string) error

	// HandleICECandidate processes an ICE candidate from a client.
	HandleICECandidate(ctx context.Context, streamID, peerID string, candidate rtcwebrtc.CandidatePayload) error

	// GetStreamInfo returns stream info for the given stream ID.
	GetStreamInfo(ctx context.Context, streamID string) (*rtcwebrtc.StreamInfoPayload, error)
}

// handleAndroidSignaling handles WebSocket connections for WebRTC signaling.
func (s *Server) handleAndroidSignaling(w http.ResponseWriter, r *http.Request) {
	if s.androidSignaling == nil {
		http.Error(w, "signaling service not configured", http.StatusNotImplemented)
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		http.Error(w, "stream ID is required", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket", zap.Error(err))
		return
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Generate a unique peer ID for this connection
	peerID := generatePeerID()

	// Create peer for this connection
	if err := s.androidSignaling.CreatePeer(ctx, streamID, peerID); err != nil {
		s.logger.Error("failed to create peer", zap.Error(err), zap.String("stream_id", streamID))
		writeSignalingError(conn, "peer_creation_failed", err.Error())
		return
	}

	defer func() {
		if err := s.androidSignaling.RemovePeer(ctx, streamID, peerID); err != nil {
			s.logger.Warn("failed to remove peer", zap.Error(err), zap.String("peer_id", peerID))
		}
	}()

	// Send stream info to the client
	streamInfo, err := s.androidSignaling.GetStreamInfo(ctx, streamID)
	if err != nil {
		s.logger.Error("failed to get stream info", zap.Error(err), zap.String("stream_id", streamID))
		writeSignalingError(conn, "stream_not_found", err.Error())
		return
	}

	infoMsg, err := rtcwebrtc.NewStreamInfoMessage(*streamInfo)
	if err != nil {
		s.logger.Error("failed to create stream info message", zap.Error(err))
		writeSignalingError(conn, "internal_error", "failed to create stream info")
		return
	}
	if err := conn.WriteJSON(infoMsg); err != nil {
		s.logger.Error("failed to send stream info", zap.Error(err))
		return
	}

	// Handle ping/pong for keep-alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Handle ICE candidates from server to client
	iceCandidateCh := make(chan *rtcwebrtc.SignalMessage, 10)
	defer close(iceCandidateCh)

	// Read and write loops
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer goroutine
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-iceCandidateCh:
				if !ok {
					return
				}
				if err := conn.WriteJSON(msg); err != nil {
					s.logger.Debug("failed to write ICE candidate", zap.Error(err))
					cancel()
					return
				}
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Reader goroutine
	go func() {
		defer wg.Done()
		defer cancel()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					s.logger.Debug("websocket read error", zap.Error(err))
				}
				return
			}

			var msg rtcwebrtc.SignalMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				s.logger.Debug("invalid signaling message", zap.Error(err))
				continue
			}

			s.handleSignalingMessage(ctx, conn, streamID, peerID, &msg)
		}
	}()

	wg.Wait()
}

// handleSignalingMessage processes a signaling message from a client.
func (s *Server) handleSignalingMessage(ctx context.Context, conn *websocket.Conn, streamID, peerID string, msg *rtcwebrtc.SignalMessage) {
	switch msg.Type {
	case rtcwebrtc.SignalTypeOffer:
		offer, err := msg.ParseOffer()
		if err != nil {
			s.logger.Debug("invalid offer payload", zap.Error(err))
			writeSignalingError(conn, "invalid_sdp", "invalid offer payload")
			return
		}

		answerSDP, err := s.androidSignaling.HandleOffer(ctx, streamID, peerID, offer.SDP)
		if err != nil {
			s.logger.Error("failed to handle offer", zap.Error(err))
			writeSignalingError(conn, "offer_failed", err.Error())
			return
		}

		answer, err := rtcwebrtc.NewAnswerMessage(answerSDP)
		if err != nil {
			s.logger.Error("failed to create answer message", zap.Error(err))
			writeSignalingError(conn, "internal_error", "failed to create answer")
			return
		}

		if err := conn.WriteJSON(answer); err != nil {
			s.logger.Debug("failed to send answer", zap.Error(err))
		}

	case rtcwebrtc.SignalTypeAnswer:
		answer, err := msg.ParseAnswer()
		if err != nil {
			s.logger.Debug("invalid answer payload", zap.Error(err))
			writeSignalingError(conn, "invalid_sdp", "invalid answer payload")
			return
		}

		if err := s.androidSignaling.HandleAnswer(ctx, streamID, peerID, answer.SDP); err != nil {
			s.logger.Error("failed to handle answer", zap.Error(err))
			writeSignalingError(conn, "answer_failed", err.Error())
			return
		}

	case rtcwebrtc.SignalTypeCandidate:
		candidate, err := msg.ParseCandidate()
		if err != nil {
			s.logger.Debug("invalid candidate payload", zap.Error(err))
			writeSignalingError(conn, "invalid_candidate", "invalid candidate payload")
			return
		}

		if err := s.androidSignaling.HandleICECandidate(ctx, streamID, peerID, *candidate); err != nil {
			s.logger.Error("failed to handle ICE candidate", zap.Error(err))
			writeSignalingError(conn, "candidate_failed", err.Error())
			return
		}

	case rtcwebrtc.SignalTypePing:
		pong := rtcwebrtc.NewPongMessage()
		if err := conn.WriteJSON(pong); err != nil {
			s.logger.Debug("failed to send pong", zap.Error(err))
		}

	default:
		s.logger.Debug("unknown message type", zap.String("type", string(msg.Type)))
	}
}

func writeSignalingError(conn *websocket.Conn, code, message string) {
	errMsg, _ := rtcwebrtc.NewErrorMessage(code, message)
	_ = conn.WriteJSON(errMsg)
}

var peerIDCounter uint64
var peerIDMu sync.Mutex

func generatePeerID() string {
	peerIDMu.Lock()
	defer peerIDMu.Unlock()
	peerIDCounter++
	return "peer_" + time.Now().Format("20060102150405") + "_" + string(rune(peerIDCounter+'0'))
}

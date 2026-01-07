package admin

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming/sfu"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Implement proper origin checking for production
		return true
	},
}

// SignalingService provides access to signaling operations.
type SignalingService interface {
	GetStreamManager() StreamManagerForSignaling
}

// StreamManagerForSignaling provides stream manager operations for signaling.
type StreamManagerForSignaling interface {
	GetRoom(streamID string) (*sfu.Room, bool)
	GetSFU() *sfu.SFU
}

// handleSignaling handles WebSocket connections for WebRTC signaling.
// GET /admin/api/v1/streams/{streamID}/signaling
func (s *Server) handleSignaling(w http.ResponseWriter, r *http.Request) {
	if s.signaling == nil {
		http.Error(w, "signaling service not configured", http.StatusServiceUnavailable)
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		http.Error(w, "stream_id is required", http.StatusBadRequest)
		return
	}

	// Get stream manager
	streamMgr := s.signaling.GetStreamManager()
	if streamMgr == nil {
		http.Error(w, "stream manager not configured", http.StatusServiceUnavailable)
		return
	}

	// Check if room exists
	room, ok := streamMgr.GetRoom(streamID)
	if !ok {
		http.Error(w, "stream not found", http.StatusNotFound)
		return
	}

	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		return
	}

	// Create signaling session
	peerID := id.New("peer")
	session := &signalingSession{
		streamID:  streamID,
		peerID:    peerID,
		conn:      conn,
		room:      room,
		sfu:       streamMgr.GetSFU(),
		logger:    s.logger.Named("signaling"),
		sendCh:    make(chan []byte, 16),
		closeCh:   make(chan struct{}),
	}

	s.logger.Info("signaling connection established",
		zap.String("stream_id", streamID),
		zap.String("peer_id", peerID),
	)

	// Run the signaling session
	session.run(r.Context())
}

// signalingSession manages a WebSocket signaling connection.
type signalingSession struct {
	streamID string
	peerID   string
	conn     *websocket.Conn
	room     *sfu.Room
	sfu      *sfu.SFU
	logger   *zap.Logger

	sendCh  chan []byte
	closeCh chan struct{}

	mu     sync.Mutex
	peer   *sfu.Peer
	closed bool
}

func (s *signalingSession) run(ctx context.Context) {
	defer s.cleanup()

	// Set up ping/pong
	s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Create signaling handler
	handler := sfu.NewSignalingHandler(s.sfu)
	handler.OnSend = func(peerID string, msg *sfu.SignalingMessage) error {
		data, err := msg.ToJSON()
		if err != nil {
			return err
		}
		select {
		case s.sendCh <- data:
		default:
			s.logger.Warn("send channel full, dropping message")
		}
		return nil
	}

	// Create subscriber peer for this browser connection
	peer, err := s.room.AddSubscriber(ctx, s.peerID)
	if err != nil {
		s.logger.Error("failed to add subscriber",
			zap.String("peer_id", s.peerID),
			zap.Error(err),
		)
		s.sendError("failed to create peer connection")
		return
	}

	s.mu.Lock()
	s.peer = peer
	s.mu.Unlock()

	// Set up ICE candidate forwarding
	handler.SetupPeerCallbacks(s.streamID, peer)

	// Create offer for subscriber
	offerMsg, err := handler.CreateOfferForSubscriber(s.streamID, peer)
	if err != nil {
		s.logger.Error("failed to create offer",
			zap.String("peer_id", s.peerID),
			zap.Error(err),
		)
		s.sendError("failed to create offer")
		return
	}

	// Send offer to browser
	offerData, _ := offerMsg.ToJSON()
	if err := s.conn.WriteMessage(websocket.TextMessage, offerData); err != nil {
		s.logger.Error("failed to send offer",
			zap.String("peer_id", s.peerID),
			zap.Error(err),
		)
		return
	}

	// Start goroutines
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.writeLoop()
	}()

	// Reader goroutine (blocks until connection closes)
	s.readLoop(handler)

	// Signal writer to stop
	close(s.closeCh)
	wg.Wait()
}

func (s *signalingSession) readLoop(handler *sfu.SignalingHandler) {
	for {
		_, data, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Error("websocket read error",
					zap.String("peer_id", s.peerID),
					zap.Error(err),
				)
			}
			return
		}

		// Parse signaling message
		msg, err := sfu.ParseSignalingMessage(data)
		if err != nil {
			s.logger.Warn("failed to parse signaling message",
				zap.String("peer_id", s.peerID),
				zap.Error(err),
			)
			continue
		}

		// Set peer ID (browser doesn't know it)
		msg.PeerID = s.peerID
		msg.StreamID = s.streamID

		// Handle message
		if err := handler.HandleMessage(msg); err != nil {
			s.logger.Error("failed to handle signaling message",
				zap.String("peer_id", s.peerID),
				zap.String("type", string(msg.Type)),
				zap.Error(err),
			)
			s.sendError(err.Error())
		}
	}
}

func (s *signalingSession) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return

		case data := <-s.sendCh:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				s.logger.Error("failed to write message",
					zap.String("peer_id", s.peerID),
					zap.Error(err),
				)
				return
			}

		case <-ticker.C:
			s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *signalingSession) sendError(message string) {
	errMsg := sfu.NewErrorMessage(s.streamID, s.peerID, message)
	data, _ := errMsg.ToJSON()
	s.conn.WriteMessage(websocket.TextMessage, data)
}

func (s *signalingSession) cleanup() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	peer := s.peer
	s.mu.Unlock()

	// Remove subscriber from room
	if peer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.room.RemoveSubscriber(ctx, s.peerID); err != nil {
			s.logger.Debug("failed to remove subscriber",
				zap.String("peer_id", s.peerID),
				zap.Error(err),
			)
		}
	}

	// Close WebSocket
	s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	s.conn.Close()

	s.logger.Info("signaling connection closed",
		zap.String("stream_id", s.streamID),
		zap.String("peer_id", s.peerID),
	)
}

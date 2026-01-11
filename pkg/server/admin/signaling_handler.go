package admin

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming/sfu"
)

// SignalingHandler handles WebSocket connections for WebRTC signaling.
type SignalingHandler struct {
	handler  *sfu.SignalingHandler
	upgrader websocket.Upgrader
	logger   *zap.Logger

	mu    sync.RWMutex
	conns map[string]*signalingConn // peerID -> connection
}

// signalingConn represents a WebSocket connection for signaling.
type signalingConn struct {
	conn     *websocket.Conn
	streamID string
	peerID   string
	mu       sync.Mutex
}

// SignalingConfig contains configuration for the signaling handler.
type SignalingConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	WriteWait       time.Duration
	PongWait        time.Duration
	PingPeriod      time.Duration
	MaxMessageSize  int64
	AllowedOrigins  []string
}

// DefaultSignalingConfig returns default signaling configuration.
func DefaultSignalingConfig() SignalingConfig {
	return SignalingConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		WriteWait:       10 * time.Second,
		PongWait:        60 * time.Second,
		PingPeriod:      54 * time.Second, // Must be less than PongWait
		MaxMessageSize:  65536,
		AllowedOrigins:  []string{"*"},
	}
}

// NewSignalingHandler creates a new SignalingHandler.
func NewSignalingHandler(handler *sfu.SignalingHandler, config SignalingConfig, logger *zap.Logger) *SignalingHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	h := &SignalingHandler{
		handler: handler,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  config.ReadBufferSize,
			WriteBufferSize: config.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				for _, allowed := range config.AllowedOrigins {
					if allowed == "*" || allowed == origin {
						return true
					}
				}
				return false
			},
		},
		logger: logger.Named("signaling_ws"),
		conns:  make(map[string]*signalingConn),
	}

	// Set up the send callback
	handler.OnSend(func(streamID, peerID string, msg *sfu.SignalingMessage) {
		h.sendMessage(peerID, msg)
	})

	return h
}

// ServeHTTP handles WebSocket upgrade and signaling messages.
func (h *SignalingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	config := DefaultSignalingConfig()

	// Upgrade connection to WebSocket
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade connection", zap.Error(err))
		return
	}

	// Get stream ID and peer ID from query params
	streamID := r.URL.Query().Get("stream_id")
	peerID := r.URL.Query().Get("peer_id")

	if streamID == "" || peerID == "" {
		h.logger.Warn("missing stream_id or peer_id")
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseProtocolError, "missing stream_id or peer_id"))
		_ = conn.Close()
		return
	}

	// Create connection wrapper
	sc := &signalingConn{
		conn:     conn,
		streamID: streamID,
		peerID:   peerID,
	}

	// Register connection
	h.mu.Lock()
	h.conns[peerID] = sc
	h.mu.Unlock()

	h.logger.Debug("signaling connection opened",
		zap.String("stream_id", streamID),
		zap.String("peer_id", peerID),
	)

	defer func() {
		h.mu.Lock()
		delete(h.conns, peerID)
		h.mu.Unlock()
		_ = conn.Close()

		h.logger.Debug("signaling connection closed",
			zap.String("stream_id", streamID),
			zap.String("peer_id", peerID),
		)
	}()

	// Configure connection
	conn.SetReadLimit(config.MaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(config.PongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(config.PongWait))
	})

	// Start ping goroutine
	go h.writePings(sc, config.PingPeriod, config.WriteWait)

	// Read messages
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Error("read error", zap.Error(err))
			}
			break
		}

		// Parse message
		var msg sfu.SignalingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			h.logger.Warn("failed to parse message", zap.Error(err))
			continue
		}

		// Set stream ID and peer ID from connection (for security)
		msg.StreamID = streamID
		msg.PeerID = peerID

		// Handle message
		if err := h.handler.HandleMessage(r.Context(), &msg); err != nil {
			h.logger.Error("failed to handle message",
				zap.String("type", string(msg.Type)),
				zap.Error(err),
			)
		}
	}
}

// sendMessage sends a message to a peer.
func (h *SignalingHandler) sendMessage(peerID string, msg *sfu.SignalingMessage) {
	h.mu.RLock()
	sc, ok := h.conns[peerID]
	h.mu.RUnlock()

	if !ok {
		h.logger.Warn("peer not found for message",
			zap.String("peer_id", peerID),
		)
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal message", zap.Error(err))
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	if err := sc.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		h.logger.Error("failed to write message",
			zap.String("peer_id", peerID),
			zap.Error(err),
		)
	}
}

// writePings sends periodic pings to keep the connection alive.
func (h *SignalingHandler) writePings(sc *signalingConn, period, writeWait time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		<-ticker.C
		sc.mu.Lock()
		_ = sc.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := sc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
			sc.mu.Unlock()
			return
		}
		sc.mu.Unlock()
	}
}

// Close closes all connections.
func (h *SignalingHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, sc := range h.conns {
		_ = sc.conn.Close()
	}
	h.conns = make(map[string]*signalingConn)
}

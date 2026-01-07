package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
)

// Browser stream message types
const (
	StreamMessageTypeFrame   = "frame"
	StreamMessageTypeInput   = "input"
	StreamMessageTypeControl = "control"
	StreamMessageTypeState   = "state"
	StreamMessageTypeStats   = "stats"
)

// WSFrameMessage represents a frame sent over WebSocket.
type WSFrameMessage struct {
	Type      string `json:"type"`
	Data      string `json:"data"`   // base64 encoded
	Format    string `json:"format"` // jpeg, png, webp
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Sequence  uint64 `json:"sequence"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
}

// WSInputMessage represents an input event from WebSocket client.
type WSInputMessage struct {
	Type  string       `json:"type"`
	Event WSInputEvent `json:"event"`
}

// WSInputEvent represents an input event (mouse or keyboard).
type WSInputEvent struct {
	EventType   string           `json:"event_type"` // mouseDown, mouseUp, mouseMove, keyDown, keyUp, etc.
	Mouse       *WSMouseEvent    `json:"mouse,omitempty"`
	Keyboard    *WSKeyboardEvent `json:"keyboard,omitempty"`
	TimestampMs int64            `json:"timestamp_ms,omitempty"`
}

// WSMouseEvent represents a mouse event.
type WSMouseEvent struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Button string  `json:"button,omitempty"` // left, right, middle
	DeltaX float64 `json:"delta_x,omitempty"`
	DeltaY float64 `json:"delta_y,omitempty"`
}

// WSKeyboardEvent represents a keyboard event.
type WSKeyboardEvent struct {
	Key  string `json:"key"`
	Code string `json:"code"`
	Text string `json:"text,omitempty"`
}

// WSControlMessage represents a control command from WebSocket client.
type WSControlMessage struct {
	Type    string          `json:"type"`
	Command string          `json:"command"` // pause, resume, navigate, switchTab
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WSStateMessage represents a state update sent to WebSocket client.
type WSStateMessage struct {
	Type    string `json:"type"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

// WSStatsMessage represents statistics sent to WebSocket client.
type WSStatsMessage struct {
	Type            string  `json:"type"`
	FramesReceived  uint64  `json:"frames_received"`
	FramesDelivered uint64  `json:"frames_delivered"`
	FramesDropped   uint64  `json:"frames_dropped"`
	CurrentFPS      float64 `json:"current_fps"`
	SubscriberCount int     `json:"subscriber_count"`
}

// BrowserStreamService defines the interface for browser streaming.
type BrowserStreamService interface {
	// ValidateTunnelToken validates a tunnel token and returns tunnel info.
	ValidateTunnelToken(ctx context.Context, token string) (*core.TunnelInfo, error)
	// Subscribe subscribes to frames for a tunnel.
	Subscribe(subscriber *core.FrameSubscriber)
	// Unsubscribe removes a subscriber.
	Unsubscribe(subscriber *core.FrameSubscriber)
	// SendInput sends an input event to the agent.
	SendInput(ctx context.Context, tunnelID string, event *pb.BrowserInputEvent) error
	// SendControl sends a control message to the agent.
	SendControl(ctx context.Context, tunnelID string, msg *pb.ServerBrowserMessage) error
	// GetStats returns statistics for a tunnel.
	GetStats(tunnelID string) *core.FrameHubStats
	// IsConnected checks if a tunnel has an active stream.
	IsConnected(tunnelID string) bool
}

// handleBrowserStream handles WebSocket connections for browser streaming.
// Route: GET /api/v1/streams/{tunnelID}/connect?token=ttok_xxx
func (s *Server) handleBrowserStream(w http.ResponseWriter, r *http.Request) {
	if s.browserStream == nil {
		http.Error(w, "browser streaming not configured", http.StatusNotImplemented)
		return
	}

	tunnelID := chi.URLParam(r, "tunnelID")
	if tunnelID == "" {
		http.Error(w, "tunnel ID is required", http.StatusBadRequest)
		return
	}

	// Get token from query parameter
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token is required", http.StatusUnauthorized)
		return
	}

	// Validate tunnel token
	tunnelInfo, err := s.browserStream.ValidateTunnelToken(r.Context(), token)
	if err != nil {
		s.logger.Warn("invalid tunnel token",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	// Verify tunnel ID matches
	if tunnelInfo.TunnelID != tunnelID {
		http.Error(w, "tunnel ID mismatch", http.StatusForbidden)
		return
	}

	// Check if stream is connected
	if !s.browserStream.IsConnected(tunnelID) {
		http.Error(w, "stream not connected", http.StatusServiceUnavailable)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		return
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	s.logger.Info("browser stream WebSocket connected",
		zap.String("tunnel_id", tunnelID),
		zap.String("session_id", tunnelInfo.SessionID),
	)

	// Create subscriber
	subscriberID := id.New("sub")
	frameCh := make(chan *pb.BrowserFrame, 30) // Buffer for frame backpressure
	doneCh := make(chan struct{})

	subscriber := &core.FrameSubscriber{
		ID:        subscriberID,
		TunnelID:  tunnelID,
		FrameCh:   frameCh,
		Done:      doneCh,
		CreatedAt: time.Now(),
	}

	// Register subscriber
	s.browserStream.Subscribe(subscriber)
	defer s.browserStream.Unsubscribe(subscriber)

	// Handle ping/pong for keep-alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Start goroutine to read messages from client (input events, control commands)
	go s.handleBrowserStreamInput(ctx, cancel, conn, tunnelID)

	// Send frames to the client
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	statsTicker := time.NewTicker(5 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-doneCh:
			// Subscriber done channel closed
			return

		case frame, ok := <-frameCh:
			if !ok {
				return
			}
			// Convert frame to WebSocket message
			wsFrame := WSFrameMessage{
				Type:      StreamMessageTypeFrame,
				Data:      base64.StdEncoding.EncodeToString(frame.GetData()),
				Format:    frame.GetFormat(),
				Width:     int(frame.GetWidth()),
				Height:    int(frame.GetHeight()),
				Sequence:  frame.GetSequence(),
				Timestamp: frame.GetTimestampUnixMs(),
			}
			if err := conn.WriteJSON(wsFrame); err != nil {
				s.logger.Debug("failed to write frame",
					zap.String("tunnel_id", tunnelID),
					zap.Error(err),
				)
				return
			}
			subscriber.FramesDelivered++

		case <-statsTicker.C:
			// Send periodic stats
			stats := s.browserStream.GetStats(tunnelID)
			if stats != nil {
				wsStats := WSStatsMessage{
					Type:            StreamMessageTypeStats,
					FramesReceived:  stats.FramesReceived,
					FramesDelivered: stats.TotalFramesDelivered,
					FramesDropped:   stats.TotalFramesDropped,
					SubscriberCount: stats.SubscriberCount,
				}
				if err := conn.WriteJSON(wsStats); err != nil {
					s.logger.Debug("failed to write stats",
						zap.String("tunnel_id", tunnelID),
						zap.Error(err),
					)
					// Don't return on stats error, continue streaming
				}
			}

		case <-ticker.C:
			// Send ping for keep-alive
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleBrowserStreamInput reads input events and control commands from WebSocket client.
func (s *Server) handleBrowserStreamInput(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, tunnelID string) {
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Debug("browser stream websocket read error",
					zap.String("tunnel_id", tunnelID),
					zap.Error(err),
				)
			}
			return
		}

		// Parse message type
		var baseMsg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &baseMsg); err != nil {
			s.logger.Debug("failed to parse message type",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
			continue
		}

		switch baseMsg.Type {
		case StreamMessageTypeInput:
			s.handleInputMessage(ctx, tunnelID, message)
		case StreamMessageTypeControl:
			s.handleControlMessage(ctx, tunnelID, message)
		case MessageTypePing:
			// Respond with pong
			_ = conn.WriteJSON(map[string]string{"type": MessageTypePong})
		}
	}
}

// handleInputMessage processes an input event from the WebSocket client.
func (s *Server) handleInputMessage(ctx context.Context, tunnelID string, message []byte) {
	var inputMsg WSInputMessage
	if err := json.Unmarshal(message, &inputMsg); err != nil {
		s.logger.Debug("failed to parse input message",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		return
	}

	// Convert to protobuf
	pbEvent := &pb.BrowserInputEvent{
		Type:            inputMsg.Event.EventType,
		TimestampUnixMs: inputMsg.Event.TimestampMs,
	}

	if inputMsg.Event.Mouse != nil {
		pbEvent.Event = &pb.BrowserInputEvent_Mouse{
			Mouse: &pb.BrowserMouseEvent{
				X:      inputMsg.Event.Mouse.X,
				Y:      inputMsg.Event.Mouse.Y,
				Button: inputMsg.Event.Mouse.Button,
				DeltaX: inputMsg.Event.Mouse.DeltaX,
				DeltaY: inputMsg.Event.Mouse.DeltaY,
			},
		}
	}

	if inputMsg.Event.Keyboard != nil {
		pbEvent.Event = &pb.BrowserInputEvent_Keyboard{
			Keyboard: &pb.BrowserKeyboardEvent{
				Key:  inputMsg.Event.Keyboard.Key,
				Code: inputMsg.Event.Keyboard.Code,
				Text: inputMsg.Event.Keyboard.Text,
			},
		}
	}

	// Send to agent via FrameHub
	if err := s.browserStream.SendInput(ctx, tunnelID, pbEvent); err != nil {
		s.logger.Debug("failed to send input event",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
	}
}

// handleControlMessage processes a control command from the WebSocket client.
func (s *Server) handleControlMessage(ctx context.Context, tunnelID string, message []byte) {
	var ctrlMsg WSControlMessage
	if err := json.Unmarshal(message, &ctrlMsg); err != nil {
		s.logger.Debug("failed to parse control message",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		return
	}

	var serverMsg *pb.ServerBrowserMessage

	switch ctrlMsg.Command {
	case "pause":
		serverMsg = &pb.ServerBrowserMessage{
			Payload: &pb.ServerBrowserMessage_Control{
				Control: &pb.BrowserStreamControl{
					Command: &pb.BrowserStreamControl_Pause{
						Pause: true,
					},
				},
			},
		}

	case "resume":
		serverMsg = &pb.ServerBrowserMessage{
			Payload: &pb.ServerBrowserMessage_Start{
				Start: &pb.BrowserStreamStart{},
			},
		}

	case "navigate":
		var payload struct {
			URL      string `json:"url"`
			Referrer string `json:"referrer,omitempty"`
		}
		if err := json.Unmarshal(ctrlMsg.Payload, &payload); err != nil {
			s.logger.Debug("failed to parse navigate payload",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
			return
		}
		serverMsg = &pb.ServerBrowserMessage{
			Payload: &pb.ServerBrowserMessage_Control{
				Control: &pb.BrowserStreamControl{
					Command: &pb.BrowserStreamControl_Navigate{
						Navigate: &pb.BrowserNavigateRequest{
							Url:      payload.URL,
							Referrer: payload.Referrer,
						},
					},
				},
			},
		}

	case "switchTab":
		var payload struct {
			TabID string `json:"tab_id"`
		}
		if err := json.Unmarshal(ctrlMsg.Payload, &payload); err != nil {
			s.logger.Debug("failed to parse switchTab payload",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
			return
		}
		serverMsg = &pb.ServerBrowserMessage{
			Payload: &pb.ServerBrowserMessage_Control{
				Control: &pb.BrowserStreamControl{
					Command: &pb.BrowserStreamControl_SwitchTab{
						SwitchTab: payload.TabID,
					},
				},
			},
		}

	default:
		s.logger.Debug("unknown control command",
			zap.String("tunnel_id", tunnelID),
			zap.String("command", ctrlMsg.Command),
		)
		return
	}

	// Send control message to agent
	if serverMsg != nil {
		if err := s.browserStream.SendControl(ctx, tunnelID, serverMsg); err != nil {
			s.logger.Debug("failed to send control message",
				zap.String("tunnel_id", tunnelID),
				zap.Error(err),
			)
		}
	}
}

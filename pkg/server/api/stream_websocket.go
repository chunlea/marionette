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

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming/browser"
)

// BrowserStreamService defines the interface for browser stream operations.
type BrowserStreamService interface {
	// GetFrameHub returns the FrameHub for subscribing to frames.
	GetFrameHub() *browser.FrameHub

	// ValidateStreamAccess validates that the caller can access the stream.
	// Returns the stream info if valid.
	ValidateStreamAccess(ctx context.Context, streamID, token string) error
}

// BrowserStreamMessage types for WebSocket communication.
const (
	BrowserMsgTypeFrame   = "frame"
	BrowserMsgTypeInput   = "input"
	BrowserMsgTypeControl = "control"
	BrowserMsgTypeError   = "error"
	BrowserMsgTypeClose   = "close"
)

// BrowserFrameMessage represents a frame message sent over WebSocket.
type BrowserFrameMessage struct {
	Type            string `json:"type"`
	Data            []byte `json:"data"`
	Format          string `json:"format"`
	Width           int32  `json:"width"`
	Height          int32  `json:"height"`
	Sequence        uint64 `json:"sequence"`
	TimestampUnixMs int64  `json:"timestamp_unix_ms"`
}

// BrowserInputMessage represents an input event sent from client.
type BrowserInputMessage struct {
	Type     string          `json:"type"`
	Event    string          `json:"event"` // "mouseMove", "mouseDown", "mouseUp", "keyDown", "keyUp", "mouseWheel"
	Mouse    *MouseEventData `json:"mouse,omitempty"`
	Keyboard *KeyEventData   `json:"keyboard,omitempty"`
}

// MouseEventData contains mouse event data.
type MouseEventData struct {
	X          float64       `json:"x"`
	Y          float64       `json:"y"`
	Button     string        `json:"button,omitempty"`
	ClickCount int           `json:"click_count,omitempty"`
	DeltaX     float64       `json:"delta_x,omitempty"`
	DeltaY     float64       `json:"delta_y,omitempty"`
	Modifiers  *ModifierData `json:"modifiers,omitempty"`
}

// KeyEventData contains keyboard event data.
type KeyEventData struct {
	Key       string        `json:"key"`
	Code      string        `json:"code,omitempty"`
	Text      string        `json:"text,omitempty"`
	Modifiers *ModifierData `json:"modifiers,omitempty"`
}

// ModifierData contains keyboard modifier state.
type ModifierData struct {
	Alt   bool `json:"alt,omitempty"`
	Ctrl  bool `json:"ctrl,omitempty"`
	Meta  bool `json:"meta,omitempty"`
	Shift bool `json:"shift,omitempty"`
}

// handleBrowserStreamWS handles WebSocket connections for browser streaming.
// URL: GET /api/v1/streams/{streamID}/ws?token=xxx
func (s *Server) handleBrowserStreamWS(w http.ResponseWriter, r *http.Request) {
	if s.browserStream == nil {
		http.Error(w, "browser streaming not configured", http.StatusNotImplemented)
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		http.Error(w, "stream ID is required", http.StatusBadRequest)
		return
	}

	// Get token from query parameter
	token := r.URL.Query().Get("token")

	// Validate stream access
	if err := s.browserStream.ValidateStreamAccess(r.Context(), streamID, token); err != nil {
		s.logger.Warn("stream access denied",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		http.Error(w, "stream access denied", http.StatusForbidden)
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("failed to upgrade websocket", zap.Error(err))
		return
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(r.Context())

	// Get FrameHub and create subscriber
	frameHub := s.browserStream.GetFrameHub()
	if frameHub == nil {
		cancel()
		s.logger.Error("frame hub not available")
		writeWSError(conn, "frame hub not available")
		return
	}

	// Create subscriber
	subscriberID := id.New("sub")
	subscriber := &browser.FrameSubscriber{
		ID:        subscriberID,
		StreamID:  streamID,
		FrameCh:   make(chan *pb.BrowserFrame, 30), // Buffered for smooth streaming
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}

	// Subscribe to frames
	frameHub.Subscribe(subscriber)
	defer frameHub.Unsubscribe(subscriber)

	s.logger.Info("browser stream WebSocket connected",
		zap.String("stream_id", streamID),
		zap.String("subscriber_id", subscriberID),
	)

	// Handle ping/pong for keep-alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Start goroutine to read input events from client
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.handleBrowserWSInput(ctx, conn, streamID, frameHub, cancel)
	}()
	// Ensure goroutine finishes before returning. Cancel context first to signal exit.
	defer func() {
		cancel()
		wg.Wait()
	}()

	// Send frames to client
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("browser stream WebSocket closing",
				zap.String("stream_id", streamID),
				zap.String("subscriber_id", subscriberID),
			)
			return

		case <-subscriber.Done:
			s.logger.Debug("subscriber done signal received",
				zap.String("stream_id", streamID),
				zap.String("subscriber_id", subscriberID),
			)
			return

		case frame, ok := <-subscriber.FrameCh:
			if !ok {
				return
			}
			// Convert proto frame to WebSocket message
			msg := BrowserFrameMessage{
				Type:            BrowserMsgTypeFrame,
				Data:            frame.Data,
				Format:          frame.Format,
				Width:           frame.Width,
				Height:          frame.Height,
				Sequence:        frame.Sequence,
				TimestampUnixMs: frame.TimestampUnixMs,
			}
			if err := conn.WriteJSON(msg); err != nil {
				s.logger.Debug("failed to write frame message", zap.Error(err))
				return
			}

		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleBrowserWSInput reads input events from the WebSocket client.
func (s *Server) handleBrowserWSInput(
	ctx context.Context,
	conn *websocket.Conn,
	streamID string,
	frameHub *browser.FrameHub,
	cancel context.CancelFunc,
) {
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read message
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				s.logger.Debug("browser WebSocket read error", zap.Error(err))
			}
			return
		}

		// Parse message
		var msg BrowserInputMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			s.logger.Debug("failed to parse input message", zap.Error(err))
			continue
		}

		// Only handle input messages
		if msg.Type != BrowserMsgTypeInput {
			continue
		}

		// Convert to proto event
		event := convertInputToProto(&msg)
		if event == nil {
			continue
		}

		// Send input to agent via FrameHub
		if err := frameHub.SendInput(ctx, streamID, event); err != nil {
			s.logger.Debug("failed to send input to agent",
				zap.String("stream_id", streamID),
				zap.Error(err),
			)
		}
	}
}

// convertInputToProto converts WebSocket input message to proto event.
func convertInputToProto(msg *BrowserInputMessage) *pb.BrowserInputEvent {
	event := &pb.BrowserInputEvent{
		Type:            msg.Event,
		TimestampUnixMs: time.Now().UnixMilli(),
	}

	switch msg.Event {
	case "mouseMove", "mouseDown", "mouseUp", "mouseWheel":
		if msg.Mouse == nil {
			return nil
		}
		event.Event = &pb.BrowserInputEvent_Mouse{
			Mouse: &pb.BrowserMouseEvent{
				X:          msg.Mouse.X,
				Y:          msg.Mouse.Y,
				Button:     msg.Mouse.Button,
				ClickCount: int32(msg.Mouse.ClickCount),
				DeltaX:     msg.Mouse.DeltaX,
				DeltaY:     msg.Mouse.DeltaY,
				Modifiers:  convertModifiersToProto(msg.Mouse.Modifiers),
			},
		}

	case "keyDown", "keyUp":
		if msg.Keyboard == nil {
			return nil
		}
		event.Event = &pb.BrowserInputEvent_Keyboard{
			Keyboard: &pb.BrowserKeyboardEvent{
				Key:       msg.Keyboard.Key,
				Code:      msg.Keyboard.Code,
				Text:      msg.Keyboard.Text,
				Modifiers: convertModifiersToProto(msg.Keyboard.Modifiers),
			},
		}

	default:
		return nil
	}

	return event
}

// convertModifiersToProto converts modifier data to proto.
func convertModifiersToProto(m *ModifierData) *pb.BrowserModifiers {
	if m == nil {
		return nil
	}
	return &pb.BrowserModifiers{
		Alt:   m.Alt,
		Ctrl:  m.Ctrl,
		Meta:  m.Meta,
		Shift: m.Shift,
	}
}

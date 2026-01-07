package core

import (
	"context"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

// FrameHub routes frames between gRPC streams (from agents) and WebSocket subscribers (clients).
// It acts as a pub/sub system for browser streaming.
type FrameHub struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// streams maps tunnelID to the gRPC stream connection
	streams map[string]*StreamConnection

	// subscribers maps tunnelID to a list of frame subscribers (WebSocket clients)
	subscribers map[string]map[*FrameSubscriber]struct{}

	// inputQueues maps tunnelID to an input event channel
	// Input events from WebSocket clients are sent here
	inputQueues map[string]chan *pb.BrowserInputEvent
}

// StreamConnection represents a gRPC stream connection from an agent.
type StreamConnection struct {
	TunnelID  string
	RunnerID  string
	SessionID string

	// SendInput sends an input event to the agent
	// This is called when WebSocket clients send input
	SendInput func(event *pb.BrowserInputEvent) error

	// SendControl sends a control message to the agent
	SendControl func(msg *pb.ServerBrowserMessage) error

	// Connected indicates if the stream is still active
	Connected   bool
	ConnectedAt time.Time

	// Stats
	FramesReceived uint64
	InputsSent     uint64
}

// FrameSubscriber represents a WebSocket client subscribing to frames.
type FrameSubscriber struct {
	ID        string
	TunnelID  string
	FrameCh   chan *pb.BrowserFrame
	Done      chan struct{}
	CreatedAt time.Time

	// Stats
	FramesDelivered uint64
	FramesDropped   uint64
}

// NewFrameHub creates a new FrameHub.
func NewFrameHub(logger *zap.Logger) *FrameHub {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &FrameHub{
		logger:      logger,
		streams:     make(map[string]*StreamConnection),
		subscribers: make(map[string]map[*FrameSubscriber]struct{}),
		inputQueues: make(map[string]chan *pb.BrowserInputEvent),
	}
}

// RegisterStream registers a gRPC stream for a tunnel.
// Returns a channel for receiving input events from WebSocket clients.
func (h *FrameHub) RegisterStream(tunnelID, runnerID, sessionID string, sendInput func(*pb.BrowserInputEvent) error, sendControl func(*pb.ServerBrowserMessage) error) (<-chan *pb.BrowserInputEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if stream already exists
	if existing, ok := h.streams[tunnelID]; ok && existing.Connected {
		h.logger.Warn("replacing existing stream",
			zap.String("tunnel_id", tunnelID),
			zap.String("old_runner_id", existing.RunnerID),
			zap.String("new_runner_id", runnerID),
		)
	}

	// Create input queue for this tunnel
	inputCh := make(chan *pb.BrowserInputEvent, 100) // Buffered to prevent blocking
	h.inputQueues[tunnelID] = inputCh

	// Register stream
	h.streams[tunnelID] = &StreamConnection{
		TunnelID:    tunnelID,
		RunnerID:    runnerID,
		SessionID:   sessionID,
		SendInput:   sendInput,
		SendControl: sendControl,
		Connected:   true,
		ConnectedAt: time.Now(),
	}

	h.logger.Info("stream registered",
		zap.String("tunnel_id", tunnelID),
		zap.String("runner_id", runnerID),
	)

	return inputCh, nil
}

// UnregisterStream removes a gRPC stream for a tunnel.
func (h *FrameHub) UnregisterStream(tunnelID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if stream, ok := h.streams[tunnelID]; ok {
		stream.Connected = false
		delete(h.streams, tunnelID)
	}

	// Close and remove input queue
	if ch, ok := h.inputQueues[tunnelID]; ok {
		close(ch)
		delete(h.inputQueues, tunnelID)
	}

	h.logger.Info("stream unregistered", zap.String("tunnel_id", tunnelID))
}

// GetStream returns the stream connection for a tunnel.
func (h *FrameHub) GetStream(tunnelID string) *StreamConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if stream, ok := h.streams[tunnelID]; ok && stream.Connected {
		return stream
	}
	return nil
}

// Subscribe registers a WebSocket client to receive frames for a tunnel.
func (h *FrameHub) Subscribe(subscriber *FrameSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tunnelID := subscriber.TunnelID

	// Create subscriber map if needed
	if h.subscribers[tunnelID] == nil {
		h.subscribers[tunnelID] = make(map[*FrameSubscriber]struct{})
	}

	h.subscribers[tunnelID][subscriber] = struct{}{}

	h.logger.Debug("subscriber added",
		zap.String("tunnel_id", tunnelID),
		zap.String("subscriber_id", subscriber.ID),
		zap.Int("total_subscribers", len(h.subscribers[tunnelID])),
	)
}

// Unsubscribe removes a WebSocket client from receiving frames.
func (h *FrameHub) Unsubscribe(subscriber *FrameSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tunnelID := subscriber.TunnelID

	if subs, ok := h.subscribers[tunnelID]; ok {
		delete(subs, subscriber)
		if len(subs) == 0 {
			delete(h.subscribers, tunnelID)
		}
	}

	h.logger.Debug("subscriber removed",
		zap.String("tunnel_id", tunnelID),
		zap.String("subscriber_id", subscriber.ID),
	)
}

// BroadcastFrame sends a frame to all subscribers for a tunnel.
// This is called when the agent sends a frame via gRPC.
func (h *FrameHub) BroadcastFrame(tunnelID string, frame *pb.BrowserFrame) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Update stream stats
	if stream, ok := h.streams[tunnelID]; ok {
		stream.FramesReceived++
	}

	subs, ok := h.subscribers[tunnelID]
	if !ok || len(subs) == 0 {
		return // No subscribers
	}

	// Send to all subscribers
	for sub := range subs {
		select {
		case sub.FrameCh <- frame:
			sub.FramesDelivered++
		default:
			// Channel full, drop frame
			sub.FramesDropped++
			h.logger.Debug("frame dropped for subscriber",
				zap.String("tunnel_id", tunnelID),
				zap.String("subscriber_id", sub.ID),
				zap.Uint64("sequence", frame.Sequence),
			)
		}
	}
}

// SendInput sends an input event to the agent for a tunnel.
// This is called when a WebSocket client sends an input event.
func (h *FrameHub) SendInput(ctx context.Context, tunnelID string, event *pb.BrowserInputEvent) error {
	h.mu.RLock()
	stream, ok := h.streams[tunnelID]
	h.mu.RUnlock()

	if !ok || !stream.Connected {
		return ErrStreamNotConnected
	}

	// Call the stream's SendInput function
	if stream.SendInput != nil {
		if err := stream.SendInput(event); err != nil {
			return err
		}
		stream.InputsSent++
	}

	return nil
}

// SendControl sends a control message to the agent for a tunnel.
func (h *FrameHub) SendControl(ctx context.Context, tunnelID string, msg *pb.ServerBrowserMessage) error {
	h.mu.RLock()
	stream, ok := h.streams[tunnelID]
	h.mu.RUnlock()

	if !ok || !stream.Connected {
		return ErrStreamNotConnected
	}

	if stream.SendControl != nil {
		return stream.SendControl(msg)
	}

	return nil
}

// QueueInput queues an input event for delivery to the agent.
// This is an alternative to SendInput that uses a channel-based approach.
func (h *FrameHub) QueueInput(tunnelID string, event *pb.BrowserInputEvent) bool {
	h.mu.RLock()
	ch, ok := h.inputQueues[tunnelID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case ch <- event:
		return true
	default:
		// Queue full
		return false
	}
}

// GetSubscriberCount returns the number of subscribers for a tunnel.
func (h *FrameHub) GetSubscriberCount(tunnelID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if subs, ok := h.subscribers[tunnelID]; ok {
		return len(subs)
	}
	return 0
}

// GetStats returns statistics for a tunnel.
func (h *FrameHub) GetStats(tunnelID string) *FrameHubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := &FrameHubStats{
		TunnelID: tunnelID,
	}

	if stream, ok := h.streams[tunnelID]; ok {
		stats.StreamConnected = stream.Connected
		stats.StreamConnectedAt = stream.ConnectedAt
		stats.FramesReceived = stream.FramesReceived
		stats.InputsSent = stream.InputsSent
	}

	if subs, ok := h.subscribers[tunnelID]; ok {
		stats.SubscriberCount = len(subs)
		for sub := range subs {
			stats.TotalFramesDelivered += sub.FramesDelivered
			stats.TotalFramesDropped += sub.FramesDropped
		}
	}

	return stats
}

// FrameHubStats contains statistics for a tunnel in the hub.
type FrameHubStats struct {
	TunnelID             string
	StreamConnected      bool
	StreamConnectedAt    time.Time
	FramesReceived       uint64
	InputsSent           uint64
	SubscriberCount      int
	TotalFramesDelivered uint64
	TotalFramesDropped   uint64
}

// ListActiveTunnels returns all tunnel IDs with active streams.
func (h *FrameHub) ListActiveTunnels() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]string, 0, len(h.streams))
	for tunnelID, stream := range h.streams {
		if stream.Connected {
			result = append(result, tunnelID)
		}
	}
	return result
}

// Close closes the FrameHub and all connections.
func (h *FrameHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close all input queues
	for tunnelID, ch := range h.inputQueues {
		close(ch)
		delete(h.inputQueues, tunnelID)
	}

	// Mark all streams as disconnected
	for _, stream := range h.streams {
		stream.Connected = false
	}
	h.streams = make(map[string]*StreamConnection)

	// Close all subscriber channels
	for tunnelID, subs := range h.subscribers {
		for sub := range subs {
			close(sub.Done)
		}
		delete(h.subscribers, tunnelID)
	}

	h.logger.Info("frame hub closed")
}

// Errors
var (
	ErrStreamNotConnected = &HubError{Code: "stream_not_connected", Message: "stream not connected"}
	ErrTunnelNotFound     = &HubError{Code: "tunnel_not_found", Message: "tunnel not found"}
)

// HubError represents a FrameHub error.
type HubError struct {
	Code    string
	Message string
}

func (e *HubError) Error() string {
	return e.Message
}

package browser

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

	// streams maps streamID to the gRPC stream connection
	streams map[string]*StreamConnection

	// subscribers maps streamID to a list of frame subscribers (WebSocket clients)
	subscribers map[string]map[*FrameSubscriber]struct{}

	// inputQueues maps streamID to an input event channel
	// Input events from WebSocket clients are sent here
	inputQueues map[string]chan *pb.BrowserInputEvent
}

// StreamConnection represents a gRPC stream connection from an agent.
type StreamConnection struct {
	StreamID  string
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
	StreamID  string
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

// RegisterStream registers a gRPC stream for a stream ID.
// Returns a channel for receiving input events from WebSocket clients.
func (h *FrameHub) RegisterStream(streamID, runnerID, sessionID string, sendInput func(*pb.BrowserInputEvent) error, sendControl func(*pb.ServerBrowserMessage) error) (<-chan *pb.BrowserInputEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if stream already exists
	if existing, ok := h.streams[streamID]; ok && existing.Connected {
		h.logger.Warn("replacing existing stream",
			zap.String("stream_id", streamID),
			zap.String("old_runner_id", existing.RunnerID),
			zap.String("new_runner_id", runnerID),
		)
	}

	// Create input queue for this stream
	inputCh := make(chan *pb.BrowserInputEvent, 100) // Buffered to prevent blocking
	h.inputQueues[streamID] = inputCh

	// Register stream
	h.streams[streamID] = &StreamConnection{
		StreamID:    streamID,
		RunnerID:    runnerID,
		SessionID:   sessionID,
		SendInput:   sendInput,
		SendControl: sendControl,
		Connected:   true,
		ConnectedAt: time.Now(),
	}

	h.logger.Info("stream registered",
		zap.String("stream_id", streamID),
		zap.String("runner_id", runnerID),
	)

	return inputCh, nil
}

// UnregisterStream removes a gRPC stream for a stream ID.
func (h *FrameHub) UnregisterStream(streamID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if stream, ok := h.streams[streamID]; ok {
		stream.Connected = false
		delete(h.streams, streamID)
	}

	// Close and remove input queue
	if ch, ok := h.inputQueues[streamID]; ok {
		close(ch)
		delete(h.inputQueues, streamID)
	}

	h.logger.Info("stream unregistered", zap.String("stream_id", streamID))
}

// GetStream returns the stream connection for a stream ID.
func (h *FrameHub) GetStream(streamID string) *StreamConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if stream, ok := h.streams[streamID]; ok && stream.Connected {
		return stream
	}
	return nil
}

// Subscribe registers a WebSocket client to receive frames for a stream.
func (h *FrameHub) Subscribe(subscriber *FrameSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	streamID := subscriber.StreamID

	// Create subscriber map if needed
	if h.subscribers[streamID] == nil {
		h.subscribers[streamID] = make(map[*FrameSubscriber]struct{})
	}

	h.subscribers[streamID][subscriber] = struct{}{}

	h.logger.Debug("subscriber added",
		zap.String("stream_id", streamID),
		zap.String("subscriber_id", subscriber.ID),
		zap.Int("total_subscribers", len(h.subscribers[streamID])),
	)
}

// Unsubscribe removes a WebSocket client from receiving frames.
func (h *FrameHub) Unsubscribe(subscriber *FrameSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	streamID := subscriber.StreamID

	if subs, ok := h.subscribers[streamID]; ok {
		delete(subs, subscriber)
		if len(subs) == 0 {
			delete(h.subscribers, streamID)
		}
	}

	h.logger.Debug("subscriber removed",
		zap.String("stream_id", streamID),
		zap.String("subscriber_id", subscriber.ID),
	)
}

// BroadcastFrame sends a frame to all subscribers for a stream.
// This is called when the agent sends a frame via gRPC.
func (h *FrameHub) BroadcastFrame(streamID string, frame *pb.BrowserFrame) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Update stream stats
	if stream, ok := h.streams[streamID]; ok {
		stream.FramesReceived++
	}

	subs, ok := h.subscribers[streamID]
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
				zap.String("stream_id", streamID),
				zap.String("subscriber_id", sub.ID),
				zap.Uint64("sequence", frame.Sequence),
			)
		}
	}
}

// SendInput sends an input event to the agent for a stream.
// This is called when a WebSocket client sends an input event.
func (h *FrameHub) SendInput(ctx context.Context, streamID string, event *pb.BrowserInputEvent) error {
	h.mu.RLock()
	stream, ok := h.streams[streamID]
	h.mu.RUnlock()

	if !ok || !stream.Connected {
		return ErrHubStreamNotConnected
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

// SendControl sends a control message to the agent for a stream.
func (h *FrameHub) SendControl(ctx context.Context, streamID string, msg *pb.ServerBrowserMessage) error {
	h.mu.RLock()
	stream, ok := h.streams[streamID]
	h.mu.RUnlock()

	if !ok || !stream.Connected {
		return ErrHubStreamNotConnected
	}

	if stream.SendControl != nil {
		return stream.SendControl(msg)
	}

	return nil
}

// QueueInput queues an input event for delivery to the agent.
// This is an alternative to SendInput that uses a channel-based approach.
func (h *FrameHub) QueueInput(streamID string, event *pb.BrowserInputEvent) bool {
	h.mu.RLock()
	ch, ok := h.inputQueues[streamID]
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

// GetSubscriberCount returns the number of subscribers for a stream.
func (h *FrameHub) GetSubscriberCount(streamID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if subs, ok := h.subscribers[streamID]; ok {
		return len(subs)
	}
	return 0
}

// GetStats returns statistics for a stream.
func (h *FrameHub) GetStats(streamID string) *FrameHubStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := &FrameHubStats{
		StreamID: streamID,
	}

	if stream, ok := h.streams[streamID]; ok {
		stats.StreamConnected = stream.Connected
		stats.StreamConnectedAt = stream.ConnectedAt
		stats.FramesReceived = stream.FramesReceived
		stats.InputsSent = stream.InputsSent
	}

	if subs, ok := h.subscribers[streamID]; ok {
		stats.SubscriberCount = len(subs)
		for sub := range subs {
			stats.TotalFramesDelivered += sub.FramesDelivered
			stats.TotalFramesDropped += sub.FramesDropped
		}
	}

	return stats
}

// FrameHubStats contains statistics for a stream in the hub.
type FrameHubStats struct {
	StreamID             string
	StreamConnected      bool
	StreamConnectedAt    time.Time
	FramesReceived       uint64
	InputsSent           uint64
	SubscriberCount      int
	TotalFramesDelivered uint64
	TotalFramesDropped   uint64
}

// ListActiveStreams returns all stream IDs with active connections.
func (h *FrameHub) ListActiveStreams() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]string, 0, len(h.streams))
	for streamID, stream := range h.streams {
		if stream.Connected {
			result = append(result, streamID)
		}
	}
	return result
}

// Close closes the FrameHub and all connections.
func (h *FrameHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Close all input queues
	for streamID, ch := range h.inputQueues {
		close(ch)
		delete(h.inputQueues, streamID)
	}

	// Mark all streams as disconnected
	for _, stream := range h.streams {
		stream.Connected = false
	}
	h.streams = make(map[string]*StreamConnection)

	// Close all subscriber channels
	for streamID, subs := range h.subscribers {
		for sub := range subs {
			close(sub.Done)
		}
		delete(h.subscribers, streamID)
	}

	h.logger.Info("frame hub closed")
}

// Hub-specific errors
var (
	ErrHubStreamNotConnected = &HubError{Code: "stream_not_connected", Message: "stream not connected"}
	ErrHubStreamNotFound     = &HubError{Code: "stream_not_found", Message: "stream not found"}
)

// HubError represents a FrameHub error.
type HubError struct {
	Code    string
	Message string
}

func (e *HubError) Error() string {
	return e.Message
}

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
)

// WebSocket message types
const (
	MessageTypeLog   = "log"
	MessageTypeEvent = "event"
	MessageTypePing  = "ping"
	MessageTypePong  = "pong"
	MessageTypeError = "error"
)

// LogMessage represents a log message sent over WebSocket.
type LogMessage struct {
	Type      string    `json:"type"`
	TaskID    string    `json:"task_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Stream    string    `json:"stream,omitempty"`
	Level     string    `json:"level,omitempty"`
	Content   string    `json:"content,omitempty"`
	Sequence  int64     `json:"sequence,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// EventMessage represents an event message sent over WebSocket.
type EventMessage struct {
	Type      string         `json:"type"`
	EventType string         `json:"event_type,omitempty"`
	Resource  string         `json:"resource,omitempty"`
	ID        string         `json:"id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitempty"`
}

// LogStreamService defines the interface for log streaming.
type LogStreamService interface {
	// Subscribe subscribes to log messages for a task.
	// Returns a channel that receives log messages and a function to unsubscribe.
	Subscribe(ctx context.Context, taskID string) (<-chan LogMessage, func(), error)
}

// EventStreamService defines the interface for event streaming.
type EventStreamService interface {
	// Subscribe subscribes to events matching the given filters.
	// Returns a channel that receives events and a function to unsubscribe.
	Subscribe(ctx context.Context, opts EventSubscribeOptions) (<-chan EventMessage, func(), error)
}

// EventSubscribeOptions defines options for subscribing to events.
type EventSubscribeOptions struct {
	EventTypes []string          `json:"event_types,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		// TODO: Implement proper origin checking based on config
		return true
	},
}

// handleLogStream handles WebSocket connections for log streaming.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if s.logStream == nil {
		http.Error(w, "log streaming not configured", http.StatusNotImplemented)
		return
	}

	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		http.Error(w, "task ID is required", http.StatusBadRequest)
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

	logs, unsubscribe, err := s.logStream.Subscribe(ctx, taskID)
	if err != nil {
		s.logger.Error("failed to subscribe to logs", zap.Error(err), zap.String("task_id", taskID))
		writeWSError(conn, "failed to subscribe to logs")
		return
	}
	defer unsubscribe()

	// Handle ping/pong for keep-alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Start a goroutine to read messages (for ping/pong and close handling)
	go func() {
		defer cancel()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					s.logger.Debug("websocket read error", zap.Error(err))
				}
				return
			}
		}
	}()

	// Send logs to the client
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case log, ok := <-logs:
			if !ok {
				return
			}
			log.Type = MessageTypeLog
			if err := conn.WriteJSON(log); err != nil {
				s.logger.Debug("failed to write log message", zap.Error(err))
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleEventStream handles WebSocket connections for event streaming.
func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	if s.eventStream == nil {
		http.Error(w, "event streaming not configured", http.StatusNotImplemented)
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

	// Parse subscription options from query parameters
	opts := EventSubscribeOptions{
		EventTypes: r.URL.Query()["event_type"],
		Labels:     parseLabelsJSON(r.URL.Query().Get("labels")),
	}

	events, unsubscribe, err := s.eventStream.Subscribe(ctx, opts)
	if err != nil {
		s.logger.Error("failed to subscribe to events", zap.Error(err))
		writeWSError(conn, "failed to subscribe to events")
		return
	}
	defer unsubscribe()

	// Handle ping/pong for keep-alive
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	// Start a goroutine to read messages
	go func() {
		defer cancel()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					s.logger.Debug("websocket read error", zap.Error(err))
				}
				return
			}
		}
	}()

	// Send events to the client
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			event.Type = MessageTypeEvent
			if err := conn.WriteJSON(event); err != nil {
				s.logger.Debug("failed to write event message", zap.Error(err))
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func writeWSError(conn *websocket.Conn, message string) {
	_ = conn.WriteJSON(map[string]string{
		"type":    MessageTypeError,
		"message": message,
	})
}

func parseLabelsJSON(s string) map[string]string {
	if s == "" {
		return nil
	}
	var labels map[string]string
	_ = json.Unmarshal([]byte(s), &labels)
	return labels
}

// MockLogStreamService is a mock implementation for testing.
type MockLogStreamService struct {
	mu          sync.Mutex
	subscribers map[string][]chan LogMessage
}

// NewMockLogStreamService creates a new mock log stream service.
func NewMockLogStreamService() *MockLogStreamService {
	return &MockLogStreamService{
		subscribers: make(map[string][]chan LogMessage),
	}
}

// Subscribe subscribes to log messages for a task.
func (m *MockLogStreamService) Subscribe(_ context.Context, taskID string) (<-chan LogMessage, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan LogMessage, 100)
	m.subscribers[taskID] = append(m.subscribers[taskID], ch)

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		subs := m.subscribers[taskID]
		for i, sub := range subs {
			if sub == ch {
				m.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}

	return ch, unsubscribe, nil
}

// Publish publishes a log message to subscribers.
func (m *MockLogStreamService) Publish(taskID string, msg LogMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ch := range m.subscribers[taskID] {
		select {
		case ch <- msg:
		default:
			// Drop if channel is full (backpressure)
		}
	}
}

// MockEventStreamService is a mock implementation for testing.
type MockEventStreamService struct {
	mu          sync.Mutex
	subscribers []chan EventMessage
}

// NewMockEventStreamService creates a new mock event stream service.
func NewMockEventStreamService() *MockEventStreamService {
	return &MockEventStreamService{
		subscribers: make([]chan EventMessage, 0),
	}
}

// Subscribe subscribes to events.
func (m *MockEventStreamService) Subscribe(_ context.Context, _ EventSubscribeOptions) (<-chan EventMessage, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan EventMessage, 100)
	m.subscribers = append(m.subscribers, ch)

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		for i, sub := range m.subscribers {
			if sub == ch {
				m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
				close(ch)
				break
			}
		}
	}

	return ch, unsubscribe, nil
}

// Publish publishes an event to all subscribers.
func (m *MockEventStreamService) Publish(msg EventMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ch := range m.subscribers {
		select {
		case ch <- msg:
		default:
			// Drop if channel is full (backpressure)
		}
	}
}

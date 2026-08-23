package api

// In-memory stand-ins for the log and event stream services.
//
// These used to live in websocket.go and so were compiled into the server
// binary. They are test doubles; nothing outside a test has ever constructed
// one.

import (
	"context"
	"sync"
)

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

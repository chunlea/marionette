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
	waiters     map[string][]chan struct{}
}

// NewMockLogStreamService creates a new mock log stream service.
func NewMockLogStreamService() *MockLogStreamService {
	return &MockLogStreamService{
		subscribers: make(map[string][]chan LogMessage),
		waiters:     make(map[string][]chan struct{}),
	}
}

// Subscribe subscribes to log messages for a task.
func (m *MockLogStreamService) Subscribe(_ context.Context, taskID string) (<-chan LogMessage, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan LogMessage, 100)
	m.subscribers[taskID] = append(m.subscribers[taskID], ch)

	for _, ready := range m.waiters[taskID] {
		close(ready)
	}
	delete(m.waiters, taskID)

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

// WaitForSubscriber blocks until at least one subscriber is registered for
// taskID, or ctx is done.
//
// Publish drops messages that have no subscriber, and the WebSocket handler only
// calls Subscribe *after* the HTTP upgrade response has been written. A test
// that publishes straight after Dial can therefore win the race, lose its
// message, and then sit on the read deadline. Waiting on the subscription closes
// that window: it returns as soon as the handler subscribes (microseconds on a
// healthy run) and leaves the read deadline as a ceiling for a genuine hang
// rather than a wall clock the test races against.
func (m *MockLogStreamService) WaitForSubscriber(ctx context.Context, taskID string) error {
	m.mu.Lock()
	if len(m.subscribers[taskID]) > 0 {
		m.mu.Unlock()
		return nil
	}
	ready := make(chan struct{})
	m.waiters[taskID] = append(m.waiters[taskID], ready)
	m.mu.Unlock()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	waiters     []chan struct{}
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

	for _, ready := range m.waiters {
		close(ready)
	}
	m.waiters = nil

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

// WaitForSubscriber blocks until at least one subscriber is registered, or ctx
// is done. See MockLogStreamService.WaitForSubscriber for why tests need it.
func (m *MockEventStreamService) WaitForSubscriber(ctx context.Context) error {
	m.mu.Lock()
	if len(m.subscribers) > 0 {
		m.mu.Unlock()
		return nil
	}
	ready := make(chan struct{})
	m.waiters = append(m.waiters, ready)
	m.mu.Unlock()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

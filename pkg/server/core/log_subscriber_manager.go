package core

import (
	"sync"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// LogSubscriberManager manages real-time log subscribers.
// Full WebSocket integration will be implemented in G6.
type LogSubscriberManager struct {
	logger *zap.Logger
	mu     sync.RWMutex
	subs   map[string][]chan *store.Log // sessionID -> channels
}

// NewLogSubscriberManager creates a new LogSubscriberManager.
func NewLogSubscriberManager(logger *zap.Logger) *LogSubscriberManager {
	return &LogSubscriberManager{
		logger: logger,
		subs:   make(map[string][]chan *store.Log),
	}
}

// Broadcast sends a log entry to all subscribers for the session.
// In G4, this is a stub that logs the broadcast. Full implementation in G6.
func (m *LogSubscriberManager) Broadcast(log *store.Log) {
	if log == nil {
		return
	}

	m.mu.RLock()
	subscribers := m.subs[log.SessionID]
	m.mu.RUnlock()

	if len(subscribers) == 0 {
		// Just log at debug level when no subscribers
		m.logger.Debug("log broadcast (no subscribers)",
			zap.String("session_id", log.SessionID),
			zap.String("task_id", log.TaskID),
			zap.Int64("sequence", log.Sequence),
		)
		return
	}

	// Send to all subscribers
	for _, ch := range subscribers {
		select {
		case ch <- log:
			// Sent successfully
		default:
			// Channel full, drop the log for this subscriber
			m.logger.Warn("dropping log for slow subscriber",
				zap.String("session_id", log.SessionID),
				zap.Int64("sequence", log.Sequence),
			)
		}
	}

	m.logger.Debug("log broadcast",
		zap.String("session_id", log.SessionID),
		zap.String("task_id", log.TaskID),
		zap.Int64("sequence", log.Sequence),
		zap.Int("subscriber_count", len(subscribers)),
	)
}

// Subscribe registers a channel to receive logs for a session.
func (m *LogSubscriberManager) Subscribe(sessionID string, ch chan *store.Log) {
	if ch == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.subs[sessionID] = append(m.subs[sessionID], ch)

	m.logger.Debug("subscriber added",
		zap.String("session_id", sessionID),
		zap.Int("total_subscribers", len(m.subs[sessionID])),
	)
}

// Unsubscribe removes a channel from session subscriptions.
func (m *LogSubscriberManager) Unsubscribe(sessionID string, ch chan *store.Log) {
	if ch == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	subscribers := m.subs[sessionID]
	for i, sub := range subscribers {
		if sub == ch {
			// Remove the subscriber by swapping with last and truncating
			m.subs[sessionID] = append(subscribers[:i], subscribers[i+1:]...)
			break
		}
	}

	// Clean up empty session entries
	if len(m.subs[sessionID]) == 0 {
		delete(m.subs, sessionID)
	}

	m.logger.Debug("subscriber removed",
		zap.String("session_id", sessionID),
	)
}

// SubscriberCount returns the number of subscribers for a session.
// Used for testing and monitoring.
func (m *LogSubscriberManager) SubscriberCount(sessionID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.subs[sessionID])
}

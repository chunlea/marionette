package core

import (
	"sync"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// logRelay publishes a log batch to the other replicas. LiveFanout implements
// it; a single-process deployment leaves it nil and pays nothing.
type logRelay interface {
	PublishLogs(logs []*store.Log)
}

// LogSubscriberManager fans real-time logs out to in-process subscribers: the
// websocket log stream, and the relay's delivery of another replica's logs.
type LogSubscriberManager struct {
	logger *zap.Logger
	mu     sync.RWMutex
	subs   map[string][]chan *store.Log // sessionID -> channels
	relay  logRelay
}

// NewLogSubscriberManager creates a new LogSubscriberManager.
func NewLogSubscriberManager(logger *zap.Logger) *LogSubscriberManager {
	return &LogSubscriberManager{
		logger: logger,
		subs:   make(map[string][]chan *store.Log),
	}
}

// setRelay injects the cross-replica relay. Package-private: production wiring
// happens once, in Wire.
func (m *LogSubscriberManager) setRelay(relay logRelay) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.relay = relay
}

// BroadcastBatch delivers a freshly written batch to local subscribers and
// announces it to the other replicas.
//
// Batch rather than line by line because the announcement is per batch: a
// notification per log line would be one database round trip per line, and the
// receiving side reads the rows back by sequence range anyway.
func (m *LogSubscriberManager) BroadcastBatch(logs []*store.Log) {
	for _, log := range logs {
		m.Broadcast(log)
	}

	m.mu.RLock()
	relay := m.relay
	m.mu.RUnlock()

	if relay != nil {
		relay.PublishLogs(logs)
	}
}

// Broadcast sends a log entry to all subscribers for the session.
//
// Local delivery only: this is also the path the relay delivers a peer's logs
// through, so publishing from here would echo them straight back out.
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

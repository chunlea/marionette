package audit

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
)

// MemoryStore implements Store interface using in-memory storage.
// This is primarily intended for testing.
type MemoryStore struct {
	mu     sync.RWMutex
	events []StoredEvent
}

// NewMemoryStore creates a new in-memory audit store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		events: make([]StoredEvent, 0),
	}
}

// CreateActionLog stores a new action log entry.
func (m *MemoryStore) CreateActionLog(_ context.Context, log *StoredEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Set ID if not provided.
	if log.ID == "" {
		log.ID = id.ActionLog()
	}

	// Set timestamp if not provided.
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}

	// Make a copy to avoid external mutation.
	event := *log
	m.events = append(m.events, event)
	return nil
}

// ListActionLogs retrieves action logs matching the filter.
func (m *MemoryStore) ListActionLogs(_ context.Context, filter Filter) (*QueryResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []StoredEvent

	for _, e := range m.events {
		if matchesFilter(e, filter) {
			matched = append(matched, e)
		}
	}

	// Sort by timestamp descending (most recent first).
	sortByTimestampDesc(matched)

	totalCount := len(matched)

	// Apply pagination.
	if filter.Offset > 0 {
		if filter.Offset >= len(matched) {
			matched = nil
		} else {
			matched = matched[filter.Offset:]
		}
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100 // Default limit.
	}

	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}

	return &QueryResult{
		Events:     matched,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// Clear removes all events from the store.
// This is useful for testing.
func (m *MemoryStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = make([]StoredEvent, 0)
}

// Count returns the number of events in the store.
func (m *MemoryStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events)
}

// matchesFilter checks if an event matches the given filter.
func matchesFilter(e StoredEvent, f Filter) bool {
	// Actor filters.
	if f.ActorType != "" && e.Actor.Type != f.ActorType {
		return false
	}
	if f.ActorID != "" && e.Actor.ID != f.ActorID {
		return false
	}

	// Action filters.
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if f.ActionPrefix != "" && !strings.HasPrefix(e.Action, f.ActionPrefix) {
		return false
	}

	// Resource filters.
	if f.ResourceType != "" && e.ResourceType != f.ResourceType {
		return false
	}
	if f.ResourceID != "" && e.ResourceID != f.ResourceID {
		return false
	}

	// Context filters.
	if f.SessionID != "" && e.SessionID != f.SessionID {
		return false
	}
	if f.TaskID != "" && e.TaskID != f.TaskID {
		return false
	}
	if f.TenantID != "" && e.TenantID != f.TenantID {
		return false
	}

	// Result filters.
	if f.SuccessOnly && !e.Success {
		return false
	}
	if f.FailureOnly && e.Success {
		return false
	}

	// Time range filters.
	if !f.StartTime.IsZero() && e.Timestamp.Before(f.StartTime) {
		return false
	}
	if !f.EndTime.IsZero() && e.Timestamp.After(f.EndTime) {
		return false
	}

	return true
}

// sortByTimestampDesc sorts events by timestamp in descending order.
func sortByTimestampDesc(events []StoredEvent) {
	// Simple bubble sort for clarity (fine for test usage).
	for i := 0; i < len(events); i++ {
		for j := i + 1; j < len(events); j++ {
			if events[j].Timestamp.After(events[i].Timestamp) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
}

// Ensure MemoryStore implements Store.
var _ Store = (*MemoryStore)(nil)

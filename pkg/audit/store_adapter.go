package audit

import (
	"context"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// ActionLogStore is the interface for action log storage operations.
// This is implemented by postgres.Store.
type ActionLogStore interface {
	// CreateActionLog stores a new action log entry.
	CreateActionLog(ctx context.Context, log *store.ActionLog) error

	// ListActionLogs retrieves action logs matching the filter.
	ListActionLogs(ctx context.Context, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error)
}

// StoreAdapter adapts an ActionLogStore to the audit.Store interface.
type StoreAdapter struct {
	store ActionLogStore
}

// NewStoreAdapter creates a new store adapter.
func NewStoreAdapter(s ActionLogStore) *StoreAdapter {
	return &StoreAdapter{store: s}
}

// CreateActionLog stores a new action log entry.
func (a *StoreAdapter) CreateActionLog(ctx context.Context, event *StoredEvent) error {
	log := eventToActionLog(event)
	return a.store.CreateActionLog(ctx, log)
}

// ListActionLogs retrieves action logs matching the filter.
func (a *StoreAdapter) ListActionLogs(ctx context.Context, filter Filter) (*QueryResult, error) {
	opts := filterToOptions(filter)

	result, err := a.store.ListActionLogs(ctx, opts)
	if err != nil {
		return nil, err
	}

	events := make([]StoredEvent, len(result.Items))
	for i, log := range result.Items {
		events[i] = actionLogToEvent(log)
	}

	return &QueryResult{
		Events:     events,
		TotalCount: int(result.TotalCount),
		HasMore:    result.HasMore,
	}, nil
}

// eventToActionLog converts an audit.StoredEvent to a store.ActionLog.
func eventToActionLog(event *StoredEvent) *store.ActionLog {
	log := &store.ActionLog{
		ID:           event.ID,
		ActorType:    event.Actor.Type.String(),
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		Details:      event.Details,
		Success:      event.Success,
		CreatedAt:    event.Timestamp,
	}

	// Set ID if not provided.
	if log.ID == "" {
		log.ID = id.ActionLog()
	}

	// Set timestamp if not provided.
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}

	// Convert optional string fields.
	if event.Actor.ID != "" {
		log.ActorID = &event.Actor.ID
	}
	if event.Actor.Name != "" {
		log.ActorName = &event.Actor.Name
	}
	if event.SessionID != "" {
		log.SessionID = &event.SessionID
	}
	if event.TaskID != "" {
		log.TaskID = &event.TaskID
	}
	if event.IPAddress != "" {
		log.IPAddress = &event.IPAddress
	}
	if event.UserAgent != "" {
		log.UserAgent = &event.UserAgent
	}
	if event.ErrorMessage != "" {
		log.ErrorMessage = &event.ErrorMessage
	}
	if event.TenantID != "" {
		log.TenantID = &event.TenantID
	}

	return log
}

// actionLogToEvent converts a store.ActionLog to an audit.StoredEvent.
func actionLogToEvent(log *store.ActionLog) StoredEvent {
	event := StoredEvent{
		ID: log.ID,
		Event: Event{
			Actor: Actor{
				Type: ActorType(log.ActorType),
				ID:   derefString(log.ActorID),
				Name: derefString(log.ActorName),
			},
			Action:       log.Action,
			ResourceType: log.ResourceType,
			ResourceID:   log.ResourceID,
			SessionID:    derefString(log.SessionID),
			TaskID:       derefString(log.TaskID),
			Details:      log.Details,
			IPAddress:    derefString(log.IPAddress),
			UserAgent:    derefString(log.UserAgent),
			Success:      log.Success,
			ErrorMessage: derefString(log.ErrorMessage),
			TenantID:     derefString(log.TenantID),
			Timestamp:    log.CreatedAt,
		},
	}
	return event
}

// filterToOptions converts an audit.Filter to store.ListActionLogsOptions.
func filterToOptions(filter Filter) store.ListActionLogsOptions {
	opts := store.ListActionLogsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:     filter.Limit,
			OrderDesc: true, // Default to newest first.
		},
	}

	if filter.Limit <= 0 {
		opts.Limit = 100 // Default limit.
	}

	// Convert filter fields to pointers.
	if filter.ActorType != "" {
		s := filter.ActorType.String()
		opts.ActorType = &s
	}
	if filter.ActorID != "" {
		opts.ActorID = &filter.ActorID
	}
	if filter.Action != "" {
		opts.Action = &filter.Action
	}
	if filter.ResourceType != "" {
		opts.ResourceType = &filter.ResourceType
	}
	if filter.ResourceID != "" {
		opts.ResourceID = &filter.ResourceID
	}
	if filter.SessionID != "" {
		opts.SessionID = &filter.SessionID
	}
	if filter.TaskID != "" {
		opts.TaskID = &filter.TaskID
	}
	if filter.SuccessOnly {
		t := true
		opts.Success = &t
	}
	if filter.FailureOnly {
		f := false
		opts.Success = &f
	}

	// Action prefix filter
	if filter.ActionPrefix != "" {
		opts.ActionPrefix = &filter.ActionPrefix
	}

	// Time range filters
	if !filter.StartTime.IsZero() {
		opts.From = &filter.StartTime
	}
	if !filter.EndTime.IsZero() {
		opts.To = &filter.EndTime
	}

	return opts
}

// derefString returns the value of a string pointer or empty string if nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Ensure StoreAdapter implements Store.
var _ Store = (*StoreAdapter)(nil)

package audit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActorType_String(t *testing.T) {
	tests := []struct {
		name     string
		actor    ActorType
		expected string
	}{
		{"user", ActorTypeUser, "user"},
		{"api_key", ActorTypeAPIKey, "api_key"},
		{"system", ActorTypeSystem, "system"},
		{"runner", ActorTypeRunner, "runner"},
		{"empty", ActorType(""), ""},
		{"custom", ActorType("custom"), "custom"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.actor.String())
		})
	}
}

func TestActorType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		actor    ActorType
		expected bool
	}{
		{"user is valid", ActorTypeUser, true},
		{"api_key is valid", ActorTypeAPIKey, true},
		{"system is valid", ActorTypeSystem, true},
		{"runner is valid", ActorTypeRunner, true},
		{"empty is invalid", ActorType(""), false},
		{"custom is invalid", ActorType("custom"), false},
		{"unknown is invalid", ActorType("unknown"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.actor.IsValid())
		})
	}
}

func TestActor_Fields(t *testing.T) {
	actor := Actor{
		Type: ActorTypeUser,
		ID:   "user-123",
		Name: "John Doe",
	}

	assert.Equal(t, ActorTypeUser, actor.Type)
	assert.Equal(t, "user-123", actor.ID)
	assert.Equal(t, "John Doe", actor.Name)
}

func TestEvent_Fields(t *testing.T) {
	event := Event{
		Actor: Actor{
			Type: ActorTypeAPIKey,
			ID:   "key-456",
			Name: "CI Key",
		},
		Action:       "session.created",
		ResourceType: "session",
		ResourceID:   "sess_123",
		SessionID:    "sess_123",
		TaskID:       "task_456",
		Details:      []byte(`{"reason":"test"}`),
		IPAddress:    "192.168.1.1",
		UserAgent:    "Mozilla/5.0",
		Success:      true,
		ErrorMessage: "",
		TenantID:     "tenant-1",
	}

	assert.Equal(t, ActorTypeAPIKey, event.Actor.Type)
	assert.Equal(t, "session.created", event.Action)
	assert.Equal(t, "session", event.ResourceType)
	assert.Equal(t, "sess_123", event.ResourceID)
	assert.Equal(t, "sess_123", event.SessionID)
	assert.Equal(t, "task_456", event.TaskID)
	assert.Equal(t, "192.168.1.1", event.IPAddress)
	assert.Equal(t, "Mozilla/5.0", event.UserAgent)
	assert.True(t, event.Success)
	assert.Empty(t, event.ErrorMessage)
	assert.Equal(t, "tenant-1", event.TenantID)
}

func TestFilter_Fields(t *testing.T) {
	filter := Filter{
		ActorType:    ActorTypeUser,
		ActorID:      "user-123",
		Action:       "session.created",
		ActionPrefix: "session.",
		ResourceType: "session",
		ResourceID:   "sess_123",
		SessionID:    "sess_123",
		TaskID:       "task_456",
		TenantID:     "tenant-1",
		SuccessOnly:  true,
		FailureOnly:  false,
		Limit:        50,
		Offset:       10,
	}

	assert.Equal(t, ActorTypeUser, filter.ActorType)
	assert.Equal(t, "user-123", filter.ActorID)
	assert.Equal(t, "session.created", filter.Action)
	assert.Equal(t, "session.", filter.ActionPrefix)
	assert.Equal(t, "session", filter.ResourceType)
	assert.Equal(t, "sess_123", filter.ResourceID)
	assert.Equal(t, "sess_123", filter.SessionID)
	assert.Equal(t, "task_456", filter.TaskID)
	assert.Equal(t, "tenant-1", filter.TenantID)
	assert.True(t, filter.SuccessOnly)
	assert.False(t, filter.FailureOnly)
	assert.Equal(t, 50, filter.Limit)
	assert.Equal(t, 10, filter.Offset)
}

func TestQueryResult_Fields(t *testing.T) {
	result := QueryResult{
		Events: []StoredEvent{
			{ID: "alog_1"},
			{ID: "alog_2"},
		},
		TotalCount: 100,
		HasMore:    true,
	}

	assert.Len(t, result.Events, 2)
	assert.Equal(t, 100, result.TotalCount)
	assert.True(t, result.HasMore)
}

func TestStoredEvent_EmbeddedEvent(t *testing.T) {
	stored := StoredEvent{
		ID: "alog_123",
		Event: Event{
			Action:       "permission.approved",
			ResourceType: "permission_request",
			ResourceID:   "perm_456",
			Success:      true,
		},
	}

	assert.Equal(t, "alog_123", stored.ID)
	assert.Equal(t, "permission.approved", stored.Action)
	assert.Equal(t, "permission_request", stored.ResourceType)
	assert.Equal(t, "perm_456", stored.ResourceID)
	assert.True(t, stored.Success)
}

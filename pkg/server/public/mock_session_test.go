package public

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockSessionService_Create(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    CreateSessionOptions
		check   func(*testing.T, *store.Session)
		wantErr bool
	}{
		{
			name: "create basic session",
			opts: CreateSessionOptions{
				Agent: "claude",
			},
			check: func(t *testing.T, s *store.Session) {
				assert.NotEmpty(t, s.ID)
				assert.Equal(t, "claude", s.Agent)
				assert.Equal(t, "pending", s.Status)
				assert.Equal(t, "on_demand", s.LifecycleMode)
				assert.Equal(t, "allow_list", s.NetworkPolicy)
				assert.False(t, s.IsBYOK)
			},
		},
		{
			name: "create session with name",
			opts: CreateSessionOptions{
				Name:  "my-session",
				Agent: "claude",
			},
			check: func(t *testing.T, s *store.Session) {
				require.NotNil(t, s.Name)
				assert.Equal(t, "my-session", *s.Name)
			},
		},
		{
			name: "create session with BYOK",
			opts: CreateSessionOptions{
				Agent:  "claude",
				APIKey: "sk-xxx",
			},
			check: func(t *testing.T, s *store.Session) {
				assert.True(t, s.IsBYOK)
			},
		},
		{
			name: "create session with labels",
			opts: CreateSessionOptions{
				Agent:  "claude",
				Labels: map[string]string{"env": "prod", "team": "backend"},
			},
			check: func(t *testing.T, s *store.Session) {
				var labels map[string]string
				err := json.Unmarshal(s.Labels, &labels)
				require.NoError(t, err)
				assert.Equal(t, "prod", labels["env"])
				assert.Equal(t, "backend", labels["team"])
			},
		},
		{
			name: "create session with custom timeout",
			opts: CreateSessionOptions{
				Agent:              "claude",
				IdleTimeoutSeconds: 600,
			},
			check: func(t *testing.T, s *store.Session) {
				require.NotNil(t, s.IdleTimeoutSeconds)
				assert.Equal(t, 600, *s.IdleTimeoutSeconds)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, err := svc.Create(ctx, tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, session)
			tt.check(t, session)
		})
	}
}

func TestMockSessionService_Get(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create a session
	session, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)

	// Get existing session
	got, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, got.ID)

	// Get non-existent session
	_, err = svc.Get(ctx, "sess_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockSessionService_List(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create sessions
	_, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, CreateSessionOptions{Agent: "codex"})
	require.NoError(t, err)

	// List all
	result, err := svc.List(ctx, ListSessionsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List by agent
	result, err = svc.List(ctx, ListSessionsOptions{Agent: "claude"})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "claude", result.Items[0].Agent)

	// List by status
	result, err = svc.List(ctx, ListSessionsOptions{Status: []string{"pending"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List with limit
	result, err = svc.List(ctx, ListSessionsOptions{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
}

func TestMockSessionService_Suspend(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create and activate a session
	session, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)
	session.Status = "active"
	svc.AddSession(session)

	// Suspend the session
	err = svc.Suspend(ctx, session.ID)
	require.NoError(t, err)

	got, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "suspended", got.Status)
	assert.NotNil(t, got.SuspendedAt)

	// Try to suspend again - should fail
	err = svc.Suspend(ctx, session.ID)
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Suspend non-existent session
	err = svc.Suspend(ctx, "sess_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockSessionService_Resume(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create and suspend a session
	session, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)

	now := time.Now()
	session.Status = "suspended"
	session.SuspendedAt = &now
	svc.AddSession(session)

	// Resume the session
	err = svc.Resume(ctx, session.ID)
	require.NoError(t, err)

	got, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "resuming", got.Status)
	assert.NotNil(t, got.ResumedAt)

	// Try to resume again - should fail
	err = svc.Resume(ctx, session.ID)
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Resume non-existent session
	err = svc.Resume(ctx, "sess_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockSessionService_Terminate(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create a session
	session, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)

	// Terminate the session
	err = svc.Terminate(ctx, session.ID)
	require.NoError(t, err)

	got, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "terminated", got.Status)

	// Try to terminate again - should fail
	err = svc.Terminate(ctx, session.ID)
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Terminate non-existent session
	err = svc.Terminate(ctx, "sess_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockSessionService_FunctionStubs(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Test custom CreateFunc
	customSession := &store.Session{ID: "sess_custom", Agent: "custom"}
	svc.CreateFunc = func(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
		return customSession, nil
	}

	session, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)
	assert.Equal(t, "sess_custom", session.ID)
	assert.Equal(t, "custom", session.Agent)
}

func TestMockSessionService_Reset(t *testing.T) {
	svc := NewMockSessionService()
	ctx := context.Background()

	// Create sessions
	_, err := svc.Create(ctx, CreateSessionOptions{Agent: "claude"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, CreateSessionOptions{Agent: "codex"})
	require.NoError(t, err)

	assert.Len(t, svc.GetAllSessions(), 2)

	// Reset
	svc.Reset()
	assert.Len(t, svc.GetAllSessions(), 0)
}

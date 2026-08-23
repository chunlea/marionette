package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// Session Tests
// =============================================================================

func TestSessionCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace first (required for session)
	workspace := &store.Workspace{
		Name:        "session-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create session
	session := &store.Session{
		Name:          strPtr("test-session"),
		Status:        "pending",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{}, // Required, NOT NULL
		LifecycleMode: "on_demand",
	}

	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)

	// Get
	got, err := testStore.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "claude", got.Agent)
	assert.Equal(t, workspace.ID, got.WorkspaceID)

	// Update
	newStatus := "active"
	err = testStore.UpdateSession(ctx, session.ID, store.SessionUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.Status)

	// List
	list, err := testStore.ListSessions(ctx, store.ListSessionsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteSession(ctx, session.ID)
	require.NoError(t, err)

	_, err = testStore.GetSession(ctx, session.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// Helper Functions
// =============================================================================

// TestUpdateSession_ExpectedStatusIsACompareAndSet: session status transitions
// are otherwise read-then-write, so two servers deciding the same transition -
// the scheduled session activator on two replicas, say - both perform it, and
// both then advance next_scheduled_at past a run nobody made.
func TestUpdateSession_ExpectedStatusIsACompareAndSet(t *testing.T) {
	ctx := context.Background()
	session := createTestSession(ctx, t)

	suspended := "suspended"
	require.NoError(t, testStore.UpdateSession(ctx, session.ID, store.SessionUpdates{
		Status: &suspended,
	}))

	resuming := "resuming"
	require.NoError(t, testStore.UpdateSession(ctx, session.ID, store.SessionUpdates{
		Status:         &resuming,
		ExpectedStatus: &suspended,
	}), "the first transition must win")

	err := testStore.UpdateSession(ctx, session.ID, store.SessionUpdates{
		Status:         &resuming,
		ExpectedStatus: &suspended,
	})
	require.Error(t, err, "a second transition from the same status must fail")
	assert.ErrorIs(t, err, store.ErrConflict)

	stored, err := testStore.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, resuming, stored.Status)
}

// TestUpdateSession_WithoutExpectedStatusStillReportsMissingRows keeps the
// non-conditional path reporting a missing session rather than a conflict.
func TestUpdateSession_WithoutExpectedStatusStillReportsMissingRows(t *testing.T) {
	status := "suspended"
	err := testStore.UpdateSession(context.Background(), "sess_does_not_exist", store.SessionUpdates{
		Status: &status,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

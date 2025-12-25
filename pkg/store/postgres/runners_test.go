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
// Runner Tests
// =============================================================================

func TestRunnerCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	runner := &store.Runner{
		Name:         "test-runner-" + time.Now().Format("150405"),
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{}, // Required, NOT NULL
		SandboxTypes: []string{}, // Required
	}

	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)
	assert.NotEmpty(t, runner.ID)
	assert.NotZero(t, runner.CreatedAt)

	// Get
	got, err := testStore.GetRunner(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, runner.Name, got.Name)
	assert.Equal(t, runner.Hostname, got.Hostname)

	// Update
	newStatus := "idle"
	err = testStore.UpdateRunner(ctx, runner.ID, store.RunnerUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetRunner(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, "idle", got.Status)

	// List
	list, err := testStore.ListRunners(ctx, store.ListRunnersOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteRunner(ctx, runner.ID)
	require.NoError(t, err)

	_, err = testStore.GetRunner(ctx, runner.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestRunnerNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetRunner(ctx, "run_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "runner", notFoundErr.Resource)
}

func TestGetRunnerByName(t *testing.T) {
	ctx := context.Background()

	// Create a runner with a unique name
	name := "test-runner-byname-" + time.Now().Format("150405.000")
	runner := &store.Runner{
		Name:         name,
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}

	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)
	assert.NotEmpty(t, runner.ID)

	// Retrieve by name
	got, err := testStore.GetRunnerByName(ctx, name)
	require.NoError(t, err)
	assert.Equal(t, runner.ID, got.ID)
	assert.Equal(t, runner.Name, got.Name)
	assert.Equal(t, runner.Hostname, got.Hostname)
	assert.Equal(t, runner.Status, got.Status)
	assert.Equal(t, runner.SandboxMode, got.SandboxMode)

	// Test non-existent name
	_, err = testStore.GetRunnerByName(ctx, "nonexistent-runner-name")
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	err = testStore.DeleteRunner(ctx, runner.ID)
	require.NoError(t, err)
}

// =============================================================================
// Transaction Tests
// =============================================================================

func TestTransaction(t *testing.T) {
	ctx := context.Background()

	// Test successful transaction
	t.Run("commit", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		runner := &store.Runner{
			Name:         "tx-test-runner-" + time.Now().Format("150405.000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err = tx.CreateRunner(ctx, runner)
		require.NoError(t, err)

		err = tx.Commit(ctx)
		require.NoError(t, err)

		// Verify runner exists
		got, err := testStore.GetRunner(ctx, runner.ID)
		require.NoError(t, err)
		assert.Equal(t, runner.Name, got.Name)

		// Cleanup
		_ = testStore.DeleteRunner(ctx, runner.ID)
	})

	// Test rollback
	t.Run("rollback", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		runner := &store.Runner{
			Name:         "tx-rollback-runner-" + time.Now().Format("150405.000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err = tx.CreateRunner(ctx, runner)
		require.NoError(t, err)

		// Rollback instead of commit
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Verify runner does not exist
		_, err = testStore.GetRunner(ctx, runner.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// =============================================================================
// Pagination Tests
// =============================================================================

func TestPagination(t *testing.T) {
	ctx := context.Background()

	// Create multiple runners
	var createdIDs []string
	for i := 0; i < 5; i++ {
		runner := &store.Runner{
			Name:         "pagination-test-" + time.Now().Format("150405.000000"),
			Hostname:     "localhost",
			Status:       "offline",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		err := testStore.CreateRunner(ctx, runner)
		require.NoError(t, err)
		createdIDs = append(createdIDs, runner.ID)
		time.Sleep(time.Millisecond) // Ensure different names
	}

	// Test limit
	list, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{
			Limit: 2,
		},
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 2)
	assert.True(t, list.HasMore)

	// Cleanup
	for _, id := range createdIDs {
		_ = testStore.DeleteRunner(ctx, id)
	}
}

// =============================================================================
// Error Tests
// =============================================================================

func TestAlreadyExistsError(t *testing.T) {
	ctx := context.Background()

	name := "duplicate-runner-" + time.Now().Format("150405")

	// Create first runner
	runner1 := &store.Runner{
		Name:         name,
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err := testStore.CreateRunner(ctx, runner1)
	require.NoError(t, err)

	// Try to create duplicate
	runner2 := &store.Runner{
		Name:         name,
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err = testStore.CreateRunner(ctx, runner2)
	assert.ErrorIs(t, err, store.ErrAlreadyExists)

	// Cleanup
	_ = testStore.DeleteRunner(ctx, runner1.ID)
}

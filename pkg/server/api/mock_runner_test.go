package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockRunnerService_Get(t *testing.T) {
	svc := NewMockRunnerService()
	ctx := context.Background()

	// Add a runner
	now := time.Now()
	runner := &store.Runner{
		ID:          id.Runner(),
		Name:        "runner-1",
		Hostname:    "localhost",
		Status:      "idle",
		SandboxMode: "runner-is-sandbox",
		Labels:      json.RawMessage(`{"env": "prod"}`),
		Annotations: json.RawMessage("{}"),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	svc.AddRunner(runner)

	// Get existing runner
	got, err := svc.Get(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, runner.ID, got.ID)
	assert.Equal(t, "runner-1", got.Name)
	assert.Equal(t, "idle", got.Status)

	// Get non-existent runner
	_, err = svc.Get(ctx, "run_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockRunnerService_List(t *testing.T) {
	svc := NewMockRunnerService()
	ctx := context.Background()

	now := time.Now()
	poolName := "macos-pool"

	// Add runners
	svc.AddRunner(&store.Runner{
		ID:          id.Runner(),
		Name:        "runner-1",
		Hostname:    "host-1",
		Status:      "idle",
		SandboxMode: "runner-is-sandbox",
		Labels:      json.RawMessage(`{"env": "prod"}`),
		Annotations: json.RawMessage("{}"),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	svc.AddRunner(&store.Runner{
		ID:          id.Runner(),
		Name:        "runner-2",
		Hostname:    "host-2",
		Status:      "busy",
		SandboxMode: "runner-is-sandbox",
		PoolName:    &poolName,
		Labels:      json.RawMessage(`{"env": "dev"}`),
		Annotations: json.RawMessage("{}"),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	svc.AddRunner(&store.Runner{
		ID:          id.Runner(),
		Name:        "runner-3",
		Hostname:    "host-3",
		Status:      "offline",
		SandboxMode: "runner-is-sandbox",
		Labels:      json.RawMessage(`{"env": "prod"}`),
		Annotations: json.RawMessage("{}"),
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	// List all
	result, err := svc.List(ctx, ListRunnersOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)

	// List by status
	result, err = svc.List(ctx, ListRunnersOptions{Status: []string{"idle"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "idle", result.Items[0].Status)

	// List by multiple statuses
	result, err = svc.List(ctx, ListRunnersOptions{Status: []string{"idle", "busy"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List by pool name
	result, err = svc.List(ctx, ListRunnersOptions{PoolName: "macos-pool"})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "runner-2", result.Items[0].Name)

	// List by labels
	result, err = svc.List(ctx, ListRunnersOptions{Labels: map[string]string{"env": "prod"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List with limit
	result, err = svc.List(ctx, ListRunnersOptions{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
}

func TestMockRunnerService_FunctionStubs(t *testing.T) {
	svc := NewMockRunnerService()
	ctx := context.Background()

	// Test custom GetFunc
	customRunner := &store.Runner{ID: "run_custom", Name: "custom"}
	svc.GetFunc = func(_ context.Context, _ string) (*store.Runner, error) {
		return customRunner, nil
	}

	runner, err := svc.Get(ctx, "any-id")
	require.NoError(t, err)
	assert.Equal(t, "run_custom", runner.ID)

	// Test custom ListFunc
	svc.ListFunc = func(_ context.Context, _ ListRunnersOptions) (*store.ListResult[store.Runner], error) {
		return &store.ListResult[store.Runner]{
			Items:      []*store.Runner{customRunner},
			TotalCount: 1,
		}, nil
	}

	result, err := svc.List(ctx, ListRunnersOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "run_custom", result.Items[0].ID)
}

func TestMockRunnerService_Reset(t *testing.T) {
	svc := NewMockRunnerService()

	// Add runners
	svc.AddRunner(&store.Runner{ID: id.Runner()})
	svc.AddRunner(&store.Runner{ID: id.Runner()})

	assert.Len(t, svc.GetAllRunners(), 2)

	// Reset
	svc.Reset()
	assert.Len(t, svc.GetAllRunners(), 0)
}

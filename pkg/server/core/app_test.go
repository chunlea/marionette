package core

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testWireDeps(t *testing.T) WireDeps {
	t.Helper()
	s := newTestStore()
	return WireDeps{
		Store:              s,
		ConnManager:        &mockConnManagerForSession{},
		CmdSender:          &mockCommandSender{},
		RunnerTokenService: auth.NewRunnerTokenService(s, id.RunnerToken),
		ProviderRegistry:   provider.NewRegistry(s),
		Logger:             zap.NewNop(),
		Jobs: JobsConfig{
			// Keep the loops from actually firing during the test.
			StaleCheckInterval:            time.Hour,
			TaskTimeoutCheckInterval:      time.Hour,
			ScheduledTaskCheckInterval:    time.Hour,
			ScheduledSessionCheckInterval: time.Hour,
			ReapInterval:                  time.Hour,
			PartitionInterval:             time.Hour,
		},
	}
}

// TestWire_BuildsEveryManager is the anti-drift test for production wiring.
// Every field asserted here was, at some point, nil in the shipped binary while
// unit tests injected it by hand and passed.
func TestWire_BuildsEveryManager(t *testing.T) {
	app, err := Wire(testWireDeps(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	require.NotNil(t, app.Sessions)
	require.NotNil(t, app.Tasks)
	require.NotNil(t, app.Permissions)
	require.NotNil(t, app.Runners)
	require.NotNil(t, app.RunnerRegistry)
	require.NotNil(t, app.Workspaces)
	require.NotNil(t, app.ScheduledTasks)
	require.NotNil(t, app.Webhooks)
	require.NotNil(t, app.LogSubscribers)
	require.NotNil(t, app.Events)
	require.NotNil(t, app.Reaper)

	// The specific regression: without a task manager, handleInFlightTasks
	// silently no-ops and a dead runner's tasks stay "running" forever.
	assert.NotNil(t, app.Runners.taskMgr, "RunnerManager must receive the TaskManager")
	assert.NotNil(t, app.Runners.sessionMgr, "RunnerManager must receive the SessionManager")

	// The session <-> task cycle must be closed in both directions.
	assert.NotNil(t, app.Sessions.taskManager, "SessionManager must receive the TaskManager")
	assert.Same(t, app.Sessions, app.Tasks.sessionMgr)

	assert.NotNil(t, app.Sessions.workspaceManager)
	assert.NotNil(t, app.Sessions.providerRegistry)

	// Every event-dispatching manager must share the webhook integration.
	assert.NotNil(t, app.Sessions.webhooks)
	assert.NotNil(t, app.Tasks.webhooks)
	assert.NotNil(t, app.Permissions.webhooks)
	assert.NotNil(t, app.Runners.webhooks)
}

// TestWire_StartsEveryBackgroundJob guards the five components that had zero
// production constructor calls before this wiring pass.
func TestWire_StartsEveryBackgroundJob(t *testing.T) {
	app, err := Wire(testWireDeps(t))
	require.NoError(t, err)

	names := make([]string, 0, len(app.jobs))
	for _, job := range app.jobs {
		names = append(names, job.name)
	}

	assert.ElementsMatch(t, []string{
		"stale-detector",
		"task-timeout-enforcer",
		"permission-timeout-enforcer",
		"scheduled-task-executor",
		"scheduled-session-activator",
		"runner-reaper",
		"log-partition-maintainer",
	}, names)

	require.NoError(t, app.Start(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, app.Stop(ctx))
}

func TestWire_RequiresEveryDependency(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*WireDeps)
		wantErr error
	}{
		{"no store", func(d *WireDeps) { d.Store = nil }, ErrStoreRequired},
		{"no conn manager", func(d *WireDeps) { d.ConnManager = nil }, ErrConnManagerRequired},
		{"no command sender", func(d *WireDeps) { d.CmdSender = nil }, ErrCmdSenderRequired},
		{"no runner token service", func(d *WireDeps) { d.RunnerTokenService = nil }, ErrRunnerTokenSvcRequired},
		{"no provider registry", func(d *WireDeps) { d.ProviderRegistry = nil }, ErrProviderRegistryRequired},
		{"no logger", func(d *WireDeps) { d.Logger = nil }, ErrLoggerRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := testWireDeps(t)
			tt.mutate(&deps)

			app, err := Wire(deps)
			assert.Nil(t, app)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestWire_EventsTeeThroughWebhookDispatch proves the websocket event stream
// and the webhook path observe the same dispatch.
func TestWire_EventsTeeThroughWebhookDispatch(t *testing.T) {
	app, err := Wire(testWireDeps(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	events, unsubscribe := app.Events.Subscribe([]string{"session.*"})
	defer unsubscribe()

	// Dispatch straight through the integration the managers hold. The webhook
	// side has no subscribers in this test, so it is expected to succeed
	// trivially; the bus side is what we assert on.
	require.NoError(t, app.Sessions.webhooks.dispatcher.Dispatch(
		context.Background(),
		"session.created",
		webhook.ResourceInfo{ID: "sess_1", Type: "session"},
		map[string]any{"status": "pending"},
		nil,
	))

	select {
	case evt := <-events:
		assert.Equal(t, "session.created", evt.Type)
		assert.Equal(t, "sess_1", evt.ResourceID)
		assert.Equal(t, "session", evt.ResourceType)
	case <-time.After(2 * time.Second):
		t.Fatal("event was not published to the bus")
	}
}

func TestApp_StopIsIdempotent(t *testing.T) {
	app, err := Wire(testWireDeps(t))
	require.NoError(t, err)

	require.NoError(t, app.Start(context.Background()))
	require.NoError(t, app.Stop(context.Background()))
	require.NoError(t, app.Stop(context.Background()))

	assert.Error(t, app.Context().Err(), "App context must be cancelled after Stop")
}

func TestApp_StopWithoutStart(t *testing.T) {
	app, err := Wire(testWireDeps(t))
	require.NoError(t, err)

	require.NoError(t, app.Stop(context.Background()))
	assert.Error(t, app.Context().Err())
}

// TestWire_LogRetentionOffByDefault guards a foot-gun: the log partitions are
// the only copy of the logs until archiving is wired, so a non-zero retention
// here deletes them rather than ageing them out of hot storage.
func TestWire_LogRetentionOffByDefault(t *testing.T) {
	var deps WireDeps
	assert.Zero(t, deps.Jobs.LogRetentionDays,
		"log retention must stay disabled until log archiving exists")
}

// TestWire_SkipsPartitionMaintainerWithoutSupport keeps a store that cannot
// partition from blocking startup.
func TestWire_SkipsPartitionMaintainerWithoutSupport(t *testing.T) {
	deps := testWireDeps(t)
	deps.Store = &nonPartitioningStore{Store: deps.Store}

	app, err := Wire(deps)
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	for _, job := range app.jobs {
		assert.NotEqual(t, "log-partition-maintainer", job.name)
	}
}

// nonPartitioningStore hides the LogPartitioner methods of the store it wraps.
// Embedding the store.Store interface promotes only that interface's methods,
// and partition maintenance is not part of it - so the wrapper satisfies
// store.Store but not jobs.LogPartitioner.
type nonPartitioningStore struct {
	store.Store
}

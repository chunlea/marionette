package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

// Two servers, one database, one live tail.
//
// Logs arrive on the replica holding the runner's control stream and events
// are published by the replica that acted; both were then broadcast to that
// process's subscribers only. A follow client or an SSE consumer on any other
// replica saw history and nothing live.
//
// These tests run two independently Wire()'d apps against one Postgres with
// their real relays, so what is exercised is the actual transport - LISTEN and
// NOTIFY over separate connections - rather than a fake in one process.

// newFanoutApp is one "process": its own store handle, managers and relay.
//
// Every background job except the relay stays off. The relay is started
// through App.Start rather than by hand, so what the test exercises is the
// production wiring: a relay Wire did not build, or a job it did not register,
// fails here.
func newFanoutApp(t *testing.T, dsn, replicaID string) *testApp {
	t.Helper()

	s, err := pgstore.New(context.Background(), pgstore.Config{URL: dsn}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	conn := allConnected{}
	app, err := Wire(WireDeps{
		Store:              s,
		ConnManager:        conn,
		CmdSender:          conn,
		RunnerTokenService: auth.NewRunnerTokenService(s, id.RunnerToken),
		ProviderRegistry:   &fakeProviderRegistry{},
		ReplicaID:          replicaID,
		Logger:             zap.NewNop(),
		Jobs: JobsConfig{
			DisableStaleDetector:       true,
			DisableTaskTimeout:         true,
			DisablePermissionTimeout:   true,
			DisableScheduledTasks:      true,
			DisableScheduledSessions:   true,
			DisableReaper:              true,
			DisablePartitionMaintainer: true,
			DisableChunkGC:             true,
			DisableRedispatch:          true,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, app.LiveFanout, "Wire must build the relay for a store that can listen")

	require.NoError(t, app.Start(context.Background()))
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	return &testApp{app: app, store: s}
}

// fanoutBatch writes a run's worth of logs and returns them.
func fanoutBatch(t *testing.T, a *testApp, sessionID, runID string, from, to int64) []*store.Log {
	t.Helper()

	logs := make([]*store.Log, 0, to-from+1)
	for seq := from; seq <= to; seq++ {
		logs = append(logs, &store.Log{
			ID:        id.Log(),
			SessionID: sessionID,
			TaskID:    "task_" + runID,
			RunID:     runID,
			RunnerID:  "rnr_fanout",
			Stream:    "stdout",
			Level:     "info",
			Content:   fmt.Sprintf("line %d", seq),
			Sequence:  seq,
		})
	}
	require.NoError(t, a.store.CreateLogs(context.Background(), logs))
	return logs
}

// The regression, end to end: the tail has to reach a subscriber on the replica
// that is not holding the stream.
func TestTwoProcesses_LogTailCrossesToTheOtherReplica(t *testing.T) {
	dsn := startPostgres(t)
	first := newFanoutApp(t, dsn, "repl_fanout_a")
	second := newFanoutApp(t, dsn, "repl_fanout_b")

	sessionID := "sess_cross_" + id.New("x")
	runID := "run_cross_" + id.New("x")

	sub := make(chan *store.Log, 16)
	first.app.LogSubscribers.Subscribe(sessionID, sub)
	defer first.app.LogSubscribers.Unsubscribe(sessionID, sub)

	// The listener has to be established before the notification is sent:
	// NOTIFY reaches sessions listening at commit time and nobody else. In
	// production that is a process that has been up since long before any
	// runner connected; here it is a short wait after Start.
	waitForListener(t)

	batch := fanoutBatch(t, second, sessionID, runID, 1, 3)
	second.app.LogSubscribers.BroadcastBatch(batch)

	var got []int64
	for i := 0; i < 3; i++ {
		select {
		case log := <-sub:
			got = append(got, log.Sequence)
		case <-time.After(15 * time.Second):
			t.Fatalf("the other replica's log tail never arrived (got %v)", got)
		}
	}
	assert.Equal(t, []int64{1, 2, 3}, got)
}

func TestTwoProcesses_EventCrossesToTheOtherReplica(t *testing.T) {
	dsn := startPostgres(t)
	first := newFanoutApp(t, dsn, "repl_event_a")
	second := newFanoutApp(t, dsn, "repl_event_b")

	sub, unsubscribe := first.app.Events.Subscribe([]string{"task.*"})
	defer unsubscribe()

	waitForListener(t)

	resourceID := "task_" + id.New("x")
	second.app.Events.Publish(Event{
		Type:         "task.completed",
		ResourceID:   resourceID,
		ResourceType: "task",
		Data:         map[string]any{"status": "completed"},
	})

	select {
	case evt := <-sub:
		assert.Equal(t, "task.completed", evt.Type)
		assert.Equal(t, resourceID, evt.ResourceID)
		assert.NotNil(t, evt.Data, "the payload has to survive the crossing")
	case <-time.After(15 * time.Second):
		t.Fatal("the other replica's event never arrived")
	}
}

// Postgres delivers a notification to every listening session, the publisher's
// own process included. Without suppression the replica that produced a batch
// would deliver every line to its own subscribers twice.
func TestTwoProcesses_ThePublisherDoesNotEchoToItself(t *testing.T) {
	dsn := startPostgres(t)
	first := newFanoutApp(t, dsn, "repl_echo_a")
	second := newFanoutApp(t, dsn, "repl_echo_b")

	sessionID := "sess_echo_" + id.New("x")
	fenceSession := "sess_fence_" + id.New("x")

	sub := make(chan *store.Log, 16)
	first.app.LogSubscribers.Subscribe(sessionID, sub)
	defer first.app.LogSubscribers.Unsubscribe(sessionID, sub)

	fence := make(chan *store.Log, 4)
	first.app.LogSubscribers.Subscribe(fenceSession, fence)
	defer first.app.LogSubscribers.Unsubscribe(fenceSession, fence)

	waitForListener(t)

	batch := fanoutBatch(t, first, sessionID, "run_echo_"+id.New("x"), 1, 2)
	first.app.LogSubscribers.BroadcastBatch(batch)

	for i := 0; i < 2; i++ {
		select {
		case <-sub:
		case <-time.After(15 * time.Second):
			t.Fatal("local delivery is missing")
		}
	}

	// A notice from the other replica, published after the echo would have
	// been: once it lands, the echo has had its chance.
	fenceBatch := fanoutBatch(t, second, fenceSession, "run_fence_"+id.New("x"), 1, 1)
	second.app.LogSubscribers.BroadcastBatch(fenceBatch)

	select {
	case <-fence:
	case <-time.After(15 * time.Second):
		t.Fatal("the fence notice never arrived")
	}

	select {
	case log := <-sub:
		t.Fatalf("the publisher echoed its own log back to itself: sequence %d", log.Sequence)
	default:
	}
}

// waitForListener gives the relay's listener time to finish its LISTEN.
//
// Start returns as soon as the goroutines are running, and a notification sent
// before the LISTEN has committed reaches nobody. Production never notices
// (the listener is up long before any runner is), and closing the gap properly
// would mean blocking startup on the database for a subsystem whose whole
// premise is that a gap is survivable.
func waitForListener(t *testing.T) {
	t.Helper()
	time.Sleep(500 * time.Millisecond)
}

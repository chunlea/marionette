package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

func newReplica(t *testing.T, addr string) *store.ServerReplica {
	t.Helper()

	replica := &store.ServerReplica{
		ID:            id.New("repl"),
		AdvertiseAddr: addr,
	}
	require.NoError(t, testStore.RegisterServerReplica(context.Background(), replica))
	t.Cleanup(func() { _ = testStore.DeleteServerReplica(context.Background(), replica.ID) })
	return replica
}

func TestServerReplicaRegisterIsIdempotent(t *testing.T) {
	ctx := context.Background()
	replica := newReplica(t, "10.0.0.1:9090")

	assert.NotZero(t, replica.StartedAt)
	firstStart := replica.StartedAt

	// Re-registering the same id refreshes the address and the heartbeat but
	// keeps started_at: the process did not restart, it re-announced.
	replica.AdvertiseAddr = "10.0.0.2:9090"
	require.NoError(t, testStore.RegisterServerReplica(ctx, replica))
	assert.Equal(t, firstStart, replica.StartedAt)

	replicas, err := testStore.ListServerReplicas(ctx)
	require.NoError(t, err)

	var found *store.ServerReplica
	for _, r := range replicas {
		if r.ID == replica.ID {
			found = r
		}
	}
	require.NotNil(t, found, "the replica must be listed")
	assert.Equal(t, "10.0.0.2:9090", found.AdvertiseAddr)
}

func TestHeartbeatServerReplicaReportsAMissingRow(t *testing.T) {
	err := testStore.HeartbeatServerReplica(context.Background(), "repl_does_not_exist")
	assert.ErrorIs(t, err, store.ErrNotFound,
		"a reaped replica must learn that it was reaped, so it can re-register")
}

// TestRunnerConnectionRoundTrip is the routing lookup itself: bind a runner to
// a replica, and the sender must be able to resolve where to reach it.
func TestRunnerConnectionRoundTrip(t *testing.T) {
	ctx := context.Background()
	runner := newClaimRunner(t, "conn-roundtrip")
	replica := newReplica(t, "10.0.0.7:9090")

	_, err := testStore.GetRunnerConnection(ctx, runner.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "an unheld runner has no connection")

	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, replica.ID))

	conn, err := testStore.GetRunnerConnection(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, runner.ID, conn.RunnerID)
	assert.Equal(t, replica.ID, conn.ReplicaID)
	assert.Equal(t, "10.0.0.7:9090", conn.AdvertiseAddr)
	assert.NotZero(t, conn.ConnectedAt)
	assert.NotZero(t, conn.LastHeartbeatAt)

	require.NoError(t, testStore.ReleaseRunnerConnection(ctx, runner.ID, replica.ID))
	_, err = testStore.GetRunnerConnection(ctx, runner.ID)
	assert.ErrorIs(t, err, store.ErrNotFound, "a released runner is held by nobody")
}

func TestBindRunnerConnectionOnMissingRunner(t *testing.T) {
	replica := newReplica(t, "10.0.0.8:9090")
	err := testStore.BindRunnerConnection(context.Background(), "run_does_not_exist", replica.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestReleaseRunnerConnectionIsFenced is the fence, and the reason the release
// is conditional. A stream that moves from replica A to replica B leaves A
// running its deferred disconnect afterwards; if that cleared the pointer, every
// command to the runner would fail until it reconnected again.
func TestReleaseRunnerConnectionIsFenced(t *testing.T) {
	ctx := context.Background()
	runner := newClaimRunner(t, "conn-fence")
	first := newReplica(t, "10.0.0.1:9090")
	second := newReplica(t, "10.0.0.2:9090")

	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, first.ID))
	// The runner reconnects to the second replica...
	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, second.ID))
	// ...and only then does the first replica notice its stream died.
	require.NoError(t, testStore.ReleaseRunnerConnection(ctx, runner.ID, first.ID))

	conn, err := testStore.GetRunnerConnection(ctx, runner.ID)
	require.NoError(t, err, "the live pointer must survive the loser's disconnect")
	assert.Equal(t, second.ID, conn.ReplicaID)
}

// TestDeletingAReplicaClearsItsRoutingPointers: the foreign key is what makes
// reaping a dead replica one statement instead of a sweep that can be
// forgotten, interrupted, or raced.
func TestDeletingAReplicaClearsItsRoutingPointers(t *testing.T) {
	ctx := context.Background()
	runner := newClaimRunner(t, "conn-fk")
	replica := newReplica(t, "10.0.0.9:9090")

	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, replica.ID))
	require.NoError(t, testStore.DeleteServerReplica(ctx, replica.ID))

	_, err := testStore.GetRunnerConnection(ctx, runner.ID)
	assert.ErrorIs(t, err, store.ErrNotFound,
		"a runner held by a deleted replica must be held by nobody")

	// And the runner itself survives: only the pointer is cleared.
	got, err := testStore.GetRunner(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, runner.ID, got.ID)
}

// TestDeleteExpiredServerReplicasReapsTheDead: a replica that stopped
// heartbeating is holding streams it no longer has, and every command routed
// to it is lost until the pointer goes.
func TestDeleteExpiredServerReplicasReapsTheDead(t *testing.T) {
	ctx := context.Background()
	live := newReplica(t, "10.0.0.10:9090")
	dead := newReplica(t, "10.0.0.11:9090")
	runner := newClaimRunner(t, "conn-reap")
	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, dead.ID))

	// A nanosecond expiry is already in the past by the time the DELETE runs,
	// so both rows qualify - which is why the live one is heartbeated first
	// and the expiry is then set to something it comfortably beats.
	require.NoError(t, testStore.HeartbeatServerReplica(ctx, live.ID))

	removed, err := testStore.DeleteExpiredServerReplicas(ctx, time.Nanosecond)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, removed, 2, "both replicas are past a nanosecond expiry")

	_, err = testStore.GetRunnerConnection(ctx, runner.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestDeleteExpiredServerReplicasKeepsTheLiving: the reaper runs on every
// replica on every tick, so an over-eager predicate would delete the whole
// fleet's routing table repeatedly.
func TestDeleteExpiredServerReplicasKeepsTheLiving(t *testing.T) {
	ctx := context.Background()
	replica := newReplica(t, "10.0.0.12:9090")
	runner := newClaimRunner(t, "conn-keep")
	require.NoError(t, testStore.BindRunnerConnection(ctx, runner.ID, replica.ID))

	_, err := testStore.DeleteExpiredServerReplicas(ctx, time.Hour)
	require.NoError(t, err)

	conn, err := testStore.GetRunnerConnection(ctx, runner.ID)
	require.NoError(t, err)
	assert.Equal(t, replica.ID, conn.ReplicaID)
}

func TestDeleteExpiredServerReplicasRejectsANonPositiveAge(t *testing.T) {
	_, err := testStore.DeleteExpiredServerReplicas(context.Background(), 0)
	assert.Error(t, err, "a zero expiry would delete every replica including this one")
}

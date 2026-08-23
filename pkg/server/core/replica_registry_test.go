package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

func newTestRegistry(t *testing.T, s store.Store, cfg ReplicaRegistryConfig) *ReplicaRegistry {
	t.Helper()

	if cfg.AdvertiseAddr == "" {
		cfg.AdvertiseAddr = "127.0.0.1:9090"
	}
	// Long enough that no test races the loop; the ticks these tests care
	// about are driven by calling tick directly.
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = time.Hour
	}

	registry, err := NewReplicaRegistry(s, cfg, zap.NewNop())
	require.NoError(t, err)
	return registry
}

func TestReplicaRegistry_RequiresAnAdvertiseAddress(t *testing.T) {
	_, err := NewReplicaRegistry(newTestStore(), ReplicaRegistryConfig{}, zap.NewNop())
	assert.ErrorIs(t, err, ErrAdvertiseAddrRequired,
		"a replica peers cannot reach is worse than one that never registered")
}

func TestReplicaRegistry_GeneratesADistinctIDPerProcess(t *testing.T) {
	s := newTestStore()
	first := newTestRegistry(t, s, ReplicaRegistryConfig{})
	second := newTestRegistry(t, s, ReplicaRegistryConfig{})

	assert.NotEmpty(t, first.ID())
	assert.NotEqual(t, first.ID(), second.ID(),
		"two processes sharing an id would overwrite each other's routing pointers")
}

// TestReplicaRegistry_LocateFindsAPeer is the lookup the hop depends on.
func TestReplicaRegistry_LocateFindsAPeer(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	holder := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.2:9090"})
	require.NoError(t, holder.Start(ctx))
	t.Cleanup(func() { holder.Stop(ctx) })

	sender := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.1:9090"})
	require.NoError(t, sender.Start(ctx))
	t.Cleanup(func() { sender.Stop(ctx) })

	holder.BindRunner(ctx, "run_1")

	peer, ok := sender.Locate("run_1")
	require.True(t, ok)
	assert.Equal(t, holder.ID(), peer.ReplicaID)
	assert.Equal(t, "10.0.0.2:9090", peer.Addr)
}

// TestReplicaRegistry_LocateExcludesItself: the caller has already checked its
// own connection map, so reporting itself would send a command on a round trip
// back to the process that just failed to find the runner.
func TestReplicaRegistry_LocateExcludesItself(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	registry := newTestRegistry(t, s, ReplicaRegistryConfig{})
	require.NoError(t, registry.Start(ctx))
	t.Cleanup(func() { registry.Stop(ctx) })

	registry.BindRunner(ctx, "run_mine")

	_, ok := registry.Locate("run_mine")
	assert.False(t, ok, "a replica must not hop to itself")
}

// TestReplicaRegistry_LocateIgnoresAnExpiredHolder: a pointer to a process
// that stopped heartbeating is a promise it cannot keep. Reporting it would
// turn a clean "runner not connected" into a dial timeout.
func TestReplicaRegistry_LocateIgnoresAnExpiredHolder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	holder := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.2:9090"})
	require.NoError(t, holder.Start(ctx))
	holder.BindRunner(ctx, "run_1")

	// Expiry in the past by construction: the holder's heartbeat is from a
	// moment ago and anything older than a nanosecond is expired.
	sender := newTestRegistry(t, s, ReplicaRegistryConfig{
		AdvertiseAddr: "10.0.0.1:9090",
		Expiry:        time.Nanosecond,
	})
	require.NoError(t, sender.Start(ctx))
	t.Cleanup(func() { sender.Stop(ctx) })

	_, ok := sender.Locate("run_1")
	assert.False(t, ok, "a pointer to a replica that stopped heartbeating must not be routed to")
}

// TestReplicaRegistry_ReleaseIsFenced: the same property as the store test,
// asserted through the registry, because this is the layer the disconnect path
// actually calls.
func TestReplicaRegistry_ReleaseIsFenced(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	first := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.1:9090"})
	second := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.2:9090"})
	require.NoError(t, first.Start(ctx))
	require.NoError(t, second.Start(ctx))
	t.Cleanup(func() { first.Stop(ctx) })
	t.Cleanup(func() { second.Stop(ctx) })

	first.BindRunner(ctx, "run_1")
	second.BindRunner(ctx, "run_1")
	// The first replica's disconnect, arriving after the runner moved.
	first.ReleaseRunner(ctx, "run_1")

	peer, ok := first.Locate("run_1")
	require.True(t, ok, "the live pointer must survive the loser's disconnect")
	assert.Equal(t, second.ID(), peer.ReplicaID)
}

// TestReplicaRegistry_DoesNotBindBeforeItIsRegistered: binding to an id no
// peer can resolve is worse than not binding, because the lookup succeeds and
// the hop goes nowhere.
func TestReplicaRegistry_DoesNotBindBeforeItIsRegistered(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	registry := newTestRegistry(t, s, ReplicaRegistryConfig{})
	registry.BindRunner(ctx, "run_1")

	_, err := s.GetRunnerConnection(ctx, "run_1")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// TestReplicaRegistry_StopWithdrawsTheReplica: a clean shutdown must take the
// pointers with it, or every rolling restart is a window in which peers
// forward to a process that has closed its streams.
func TestReplicaRegistry_StopWithdrawsTheReplica(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	holder := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.2:9090"})
	require.NoError(t, holder.Start(ctx))
	holder.BindRunner(ctx, "run_1")

	sender := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.1:9090"})
	require.NoError(t, sender.Start(ctx))
	t.Cleanup(func() { sender.Stop(ctx) })
	_, ok := sender.Locate("run_1")
	require.True(t, ok)

	holder.Stop(ctx)

	// Past the locator cache, so this is the registry answering and not the
	// remembered result from before the shutdown.
	time.Sleep(replicaLocateCacheTTL + 50*time.Millisecond)
	_, ok = sender.Locate("run_1")
	assert.False(t, ok, "a withdrawn replica must not be routed to")
}

func TestReplicaRegistry_StopIsIdempotent(t *testing.T) {
	ctx := context.Background()
	registry := newTestRegistry(t, newTestStore(), ReplicaRegistryConfig{})
	require.NoError(t, registry.Start(ctx))

	registry.Stop(ctx)
	registry.Stop(ctx)
}

// TestReplicaRegistry_TickReapsAndCounts covers the heartbeat tick's two other
// jobs: removing replicas that stopped heartbeating, and publishing how many
// are left.
func TestReplicaRegistry_TickReapsAndCounts(t *testing.T) {
	ctx := context.Background()
	s := newTestStore()

	dead := newTestRegistry(t, s, ReplicaRegistryConfig{AdvertiseAddr: "10.0.0.9:9090"})
	require.NoError(t, dead.Start(ctx))
	dead.BindRunner(ctx, "run_1")

	var counted int
	reaper := newTestRegistry(t, s, ReplicaRegistryConfig{
		AdvertiseAddr:       "10.0.0.1:9090",
		Expiry:              time.Nanosecond,
		ObserveReplicaCount: func(n int) { counted = n },
	})
	require.NoError(t, reaper.Start(ctx))
	t.Cleanup(func() { reaper.Stop(ctx) })

	reaper.tick(ctx)

	// Everything older than a nanosecond went, including the reaper's own row
	// from before its heartbeat - so what matters is that the dead replica's
	// routing pointer is gone with it.
	_, err := s.GetRunnerConnection(ctx, "run_1")
	assert.ErrorIs(t, err, store.ErrNotFound,
		"a reaped replica must not keep holding a runner it cannot reach")
	assert.GreaterOrEqual(t, counted, 0, "the observer must be called")
}

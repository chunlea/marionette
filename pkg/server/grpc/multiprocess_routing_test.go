package grpc

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

// Two servers, one database, one runner.
//
// A runner's control stream terminates in exactly one process, and the map
// that resolves it is in that process's memory. Before the registry, a command
// sent from the other process simply reported "runner not connected" - which,
// in the shipped three-replica production overlay, is roughly two out of three
// ExecuteTask commands.
//
// These tests run two independently built routing stacks against one Postgres
// and prove the command crosses. Nothing here is mocked below the store: real
// registries, a real gRPC listener per replica, the real hop.

// routingDockerAvailable reports whether a Docker daemon is reachable.
// testcontainers panics rather than erroring when there is no Docker host at
// all, so the probe has to recover.
func routingDockerAvailable(ctx context.Context) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()

	prov, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer func() { _ = prov.Close() }()

	return prov.Health(ctx) == nil
}

var (
	routingDBOnce sync.Once
	routingDSN    string
	routingDBStop func()
)

// startRoutingPostgres brings up a database with every migration applied, or
// skips. Skipping rather than failing is deliberate: the rest of this package
// is pure unit tests and must keep running on a machine with no Docker.
func startRoutingPostgres(t *testing.T) string {
	t.Helper()

	routingDBOnce.Do(func() { routingDSN = bootRoutingPostgres(t) })
	if routingDSN == "" {
		t.Skip("no Docker daemon reachable: skipping the two-process routing test")
	}
	return routingDSN
}

// TestMain exists only to tear the shared container down at the end of the
// run. Registering that on the first test's t.Cleanup would kill the database
// out from under every test after it.
func TestMain(m *testing.M) {
	code := m.Run()
	if routingDBStop != nil {
		routingDBStop()
	}
	os.Exit(code)
}

func bootRoutingPostgres(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	if !routingDockerAvailable(ctx) {
		return ""
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:15-alpine",
		tcpostgres.WithDatabase("marionette_routing_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	require.NoError(t, err)
	routingDBStop = func() { _ = container.Terminate(context.Background()) }

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	migrator, err := pgstore.New(ctx, pgstore.Config{URL: dsn}, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = migrator.Close() }()

	// Glob rather than a fixed list: a migration this test does not know about
	// must not be silently skipped, or it would validate a stale schema.
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no migrations found")
	sort.Strings(files)

	for _, file := range files {
		sql, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, migrator.ExecRaw(ctx, string(sql)), "applying %s", file)
	}

	return dsn
}

// routingReplica is one "process": its own store handle, connection manager,
// registry, internal listener and forwarder.
type routingReplica struct {
	store     *pgstore.Store
	conns     *ConnectionManager
	registry  *core.ReplicaRegistry
	forwarder *PeerForwarder
	addr      string
	server    *grpc.Server
}

const routingTestMasterKey = "routing-test-master-key"

func newRoutingReplica(t *testing.T, dsn string) *routingReplica {
	t.Helper()

	logger := zap.NewNop()

	s, err := pgstore.New(context.Background(), pgstore.Config{URL: dsn}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// Port zero: two replicas in one test binary cannot share a fixed port,
	// and the address only has to be reachable from the other replica.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	conns := NewConnectionManager(logger)
	cred := DerivePeerCredential(routingTestMasterKey)

	server := grpc.NewServer()
	pb.RegisterInternalRouterServiceServer(server, NewInternalRouterService(conns, cred, nil, logger))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	registry, err := core.NewReplicaRegistry(s, core.ReplicaRegistryConfig{
		AdvertiseAddr: lis.Addr().String(),
		// Long enough that no test races the heartbeat, short enough that the
		// expiry check in Locate is still exercised.
		HeartbeatInterval: time.Minute,
		Expiry:            time.Minute,
	}, logger)
	require.NoError(t, err)
	require.NoError(t, registry.Start(context.Background()))
	t.Cleanup(func() { registry.Stop(context.Background()) })

	forwarder := NewPeerForwarder(cred, registry.ID(), logger)
	t.Cleanup(forwarder.Close)

	conns.SetRouter(routingLocator{registry}, forwarder, nil)

	return &routingReplica{
		store:     s,
		conns:     conns,
		registry:  registry,
		forwarder: forwarder,
		addr:      lis.Addr().String(),
		server:    server,
	}
}

// routingLocator is the same adapter cmd/server wires: the two Peer types are
// identical, and separate only because core cannot import this package.
type routingLocator struct {
	registry *core.ReplicaRegistry
}

func (l routingLocator) Locate(runnerID string) (RunnerPeer, bool) {
	peer, ok := l.registry.Locate(runnerID)
	if !ok {
		return RunnerPeer{}, false
	}
	return RunnerPeer{ReplicaID: peer.ReplicaID, Addr: peer.Addr}, true
}

// attachRunner registers a runner row and a fake control stream on this
// replica, and publishes the routing pointer, exactly as Connect does.
//
// The connection carries only a command channel: sendLocal writes there and
// the sender goroutine - which production runs and this test does not need -
// is what drains it onto the wire.
func (r *routingReplica) attachRunner(t *testing.T, runnerID string) chan *pb.ServerCommand {
	t.Helper()

	ch := make(chan *pb.ServerCommand, 4)
	require.NoError(t, r.conns.Register(runnerID, &RunnerConnection{
		RunnerID:  runnerID,
		Name:      runnerID,
		commandCh: ch,
	}))
	r.registry.BindRunner(context.Background(), runnerID)
	return ch
}

func seedRoutingRunner(t *testing.T, r *routingReplica) string {
	t.Helper()

	runner := &store.Runner{
		ID:           id.Runner(),
		Name:         "routing-" + id.Runner(),
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		SandboxTypes: []string{},
		Capabilities: []string{},
	}
	require.NoError(t, r.store.CreateRunner(context.Background(), runner))
	return runner.ID
}

func killTask(taskID string) *pb.ServerCommand {
	return &pb.ServerCommand{
		Payload: &pb.ServerCommand_KillTask{KillTask: &pb.KillTask{TaskId: taskID}},
	}
}

// TestTwoProcesses_CommandCrossesToTheHoldingReplica is the regression this
// whole change exists for: the runner is attached to the second replica, and
// the first one must still be able to command it.
func TestTwoProcesses_CommandCrossesToTheHoldingReplica(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	runnerID := seedRoutingRunner(t, first)
	inbox := second.attachRunner(t, runnerID)

	// The first replica has never seen this runner's stream.
	_, heldLocally := first.conns.Get(runnerID)
	require.False(t, heldLocally, "the sending replica must not hold the runner")

	require.NoError(t, first.conns.SendCommand(runnerID, killTask("task_crossing")))

	select {
	case cmd := <-inbox:
		require.NotNil(t, cmd.GetKillTask())
		assert.Equal(t, "task_crossing", cmd.GetKillTask().GetTaskId())
	case <-time.After(5 * time.Second):
		t.Fatal("the command never reached the replica holding the runner")
	}
}

// TestTwoProcesses_IsConnectedSeesTheWholeFleet: routing a command is only
// half of it. Runner selection asks IsConnected, and a runner invisible to the
// replica doing the allocating can never be given any work at all.
func TestTwoProcesses_IsConnectedSeesTheWholeFleet(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	// Two runners rather than one before-and-after on the same id: locator
	// answers are cached briefly, so re-asking about the same runner inside the
	// TTL would be testing the cache rather than the registry.
	attached := seedRoutingRunner(t, first)
	unattached := seedRoutingRunner(t, first)

	second.attachRunner(t, attached)

	assert.True(t, first.conns.IsConnected(attached),
		"a runner attached to another replica must still count as connected")
	assert.False(t, first.conns.IsConnected(unattached),
		"a runner nobody holds must not")
	assert.Equal(t, 0, first.conns.LocalCount(),
		"and neither may be counted as one of this process's own connections")
}

// TestTwoProcesses_ReleaseIsFenced: the runner moved from the second replica to
// the first, and only afterwards does the second notice its stream died.
// Clearing unconditionally would delete the pointer that had just become
// correct, and every command would fail until the next reconnect.
func TestTwoProcesses_ReleaseIsFenced(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	runnerID := seedRoutingRunner(t, first)
	second.attachRunner(t, runnerID)
	inbox := first.attachRunner(t, runnerID)

	// The loser's disconnect, running late: its stream is gone and only then
	// does it get round to clearing the pointer.
	second.conns.Unregister(runnerID)
	second.registry.ReleaseRunner(context.Background(), runnerID)

	conn, err := first.store.GetRunnerConnection(
		store.WithSystemAccess(context.Background()), runnerID)
	require.NoError(t, err, "the live pointer must survive the loser's disconnect")
	assert.Equal(t, first.registry.ID(), conn.ReplicaID)

	// And the command still lands, locally, on the replica that holds it.
	require.NoError(t, second.conns.SendCommand(runnerID, killTask("task_fenced")))
	select {
	case cmd := <-inbox:
		assert.Equal(t, "task_fenced", cmd.GetKillTask().GetTaskId())
	case <-time.After(5 * time.Second):
		t.Fatal("the command never reached the current holder")
	}
}

// TestTwoProcesses_DeadReplicaFailsVisibly is the failure-path honesty
// requirement: a runner whose replica is gone must produce an error the caller
// can compensate for, not a command that disappears.
func TestTwoProcesses_DeadReplicaFailsVisibly(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	runnerID := seedRoutingRunner(t, first)
	second.attachRunner(t, runnerID)

	// The holder's process dies without deregistering: its row and its routing
	// pointer are still in the database, and its listener is gone.
	second.server.Stop()

	err := first.conns.SendCommand(runnerID, killTask("task_lost"))
	require.Error(t, err, "a command to a dead replica must not look like success")
	assert.ErrorIs(t, err, ErrReplicaUnreachable)
}

// TestTwoProcesses_StaleRegistryPointerReportsNotConnected: the pointer is
// live but the runner hung up on the holder between the lookup and the hop.
// The holder says so, and the sender surfaces the same error a local miss
// produces - so unwindDispatch and the redispatch backoff behave identically.
func TestTwoProcesses_StaleRegistryPointerReportsNotConnected(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	runnerID := seedRoutingRunner(t, first)
	second.attachRunner(t, runnerID)

	// The stream goes away but the pointer stays: exactly the window between
	// a disconnect and its deferred release.
	second.conns.Unregister(runnerID)

	err := first.conns.SendCommand(runnerID, killTask("task_stale"))
	assert.ErrorIs(t, err, ErrRunnerNotFound,
		"a stale pointer must read as 'runner not connected', which every caller already handles")
}

// TestTwoProcesses_PeerCredentialIsRequired: the internal method shares the
// port runners dial, so a caller without the shared credential must be
// refused. A runner never holds it.
func TestTwoProcesses_PeerCredentialIsRequired(t *testing.T) {
	dsn := startRoutingPostgres(t)
	first := newRoutingReplica(t, dsn)
	second := newRoutingReplica(t, dsn)

	runnerID := seedRoutingRunner(t, first)
	inbox := second.attachRunner(t, runnerID)

	impostor := NewPeerForwarder(
		DerivePeerCredential("not-the-master-key"), "repl_impostor", zap.NewNop())
	t.Cleanup(impostor.Close)

	err := impostor.Forward(
		RunnerPeer{ReplicaID: second.registry.ID(), Addr: second.addr},
		runnerID, killTask("task_forged"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReplicaUnreachable)

	select {
	case <-inbox:
		t.Fatal("a command from an unauthenticated peer must not reach the runner")
	case <-time.After(200 * time.Millisecond):
	}
}

// countingLocator records every lookup, so a test can assert that a lookup
// never happened rather than merely that the answer was right.
type countingLocator struct {
	mu    sync.Mutex
	calls int
	peer  RunnerPeer
	held  bool
}

func (l *countingLocator) Locate(string) (RunnerPeer, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return l.peer, l.held
}

func (l *countingLocator) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// refusingForwarder fails any hop, so a test that expected none says so
// loudly.
type refusingForwarder struct{ t *testing.T }

func (f refusingForwarder) Forward(RunnerPeer, string, *pb.ServerCommand) error {
	f.t.Error("a locally held runner must never be reached through a peer hop")
	return nil
}

// TestSingleProcess_LocallyHeldRunnerCostsNoLookup is the zero-cost guarantee.
//
// The whole design rests on the claim that adding routing does not slow down
// the deployment shape almost everyone runs. Local-first is what delivers it,
// and this is the assertion: a runner this process holds is answered from the
// map, with no locator call and therefore no query.
func TestSingleProcess_LocallyHeldRunnerCostsNoLookup(t *testing.T) {
	conns := NewConnectionManager(zap.NewNop())
	locator := &countingLocator{}
	conns.SetRouter(locator, refusingForwarder{t}, nil)

	inbox := make(chan *pb.ServerCommand, 1)
	require.NoError(t, conns.Register("run_local", &RunnerConnection{
		RunnerID:  "run_local",
		commandCh: inbox,
	}))

	require.NoError(t, conns.SendCommand("run_local", killTask("task_local")))
	assert.True(t, conns.IsConnected("run_local"))

	select {
	case cmd := <-inbox:
		assert.Equal(t, "task_local", cmd.GetKillTask().GetTaskId())
	default:
		t.Fatal("the command was not written to the local connection")
	}

	assert.Zero(t, locator.count(),
		"a locally held runner must be resolved without consulting the registry")
}

// TestSingleProcess_WithoutARouterNothingChanges: a deployment that never
// attaches routing behaves exactly as it did before routing existed, down to
// the error.
func TestSingleProcess_WithoutARouterNothingChanges(t *testing.T) {
	conns := NewConnectionManager(zap.NewNop())

	assert.ErrorIs(t, conns.SendCommand("run_missing", killTask("t")), ErrRunnerNotFound)
	assert.False(t, conns.IsConnected("run_missing"))
}

// TestPeerCredentialDerivation: the credential is deterministic across
// replicas (they derive it from the same master key and must agree), distinct
// per key, and absent when there is no key - which is what disables the
// internal router rather than leaving it open.
func TestPeerCredentialDerivation(t *testing.T) {
	first := DerivePeerCredential("shared-master-key")
	second := DerivePeerCredential("shared-master-key")
	other := DerivePeerCredential("a-different-key")

	assert.NotEmpty(t, first)
	assert.Equal(t, first, second, "replicas sharing a master key must agree")
	assert.NotEqual(t, first, other)
	assert.True(t, first.Equal(string(second)))
	assert.False(t, first.Equal(string(other)))

	assert.Empty(t, DerivePeerCredential(""), "no master key means no peer credential")
	assert.False(t, PeerCredential("").Equal(""), "an empty credential must never match")

	// And the derivation must not simply be the master key.
	assert.NotEqual(t, "shared-master-key", string(first))
}

package core

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Replica registry defaults.
const (
	// DefaultReplicaHeartbeatInterval is how often this process proves it is
	// alive. It bounds how long a dead replica's routing pointers stay in the
	// database, so it is short - the write is one indexed UPDATE.
	DefaultReplicaHeartbeatInterval = 10 * time.Second

	// DefaultReplicaExpiry is how long a replica may go without a heartbeat
	// before its rows are reaped, taking every routing pointer it held with
	// them.
	//
	// Three intervals: one missed tick is a slow database, three is a process
	// that is not coming back. Reaping early is not harmful (the runner
	// reconnects and rebinds) but it does cost a window in which commands fail
	// that would have worked.
	DefaultReplicaExpiry = 3 * DefaultReplicaHeartbeatInterval

	// replicaLocateCacheTTL is how long a locator answer is reused.
	//
	// Staleness in either direction is benign and already has a recovery path:
	// a stale "connected" produces a send that fails and unwinds, a stale "not
	// connected" produces a skipped candidate the redispatch sweeper revisits.
	// What the cache buys is that IsConnected, which runner selection calls in
	// a loop, is not a query per candidate.
	replicaLocateCacheTTL = time.Second
)

// ReplicaRegistry is this process's entry in the cross-replica routing table,
// and its view of everyone else's.
//
// Three jobs, all small:
//
//   - announce this process and keep its heartbeat fresh, so peers know where
//     to reach it and know when it is gone;
//   - record which runners this process is holding, so a command sent from
//     anywhere reaches the stream instead of falling into "runner not
//     connected";
//   - reap replicas that stopped heartbeating, which clears their pointers
//     through the foreign key.
//
// Everything it does runs with system access. Routing serves every tenant's
// runners and has no request to take a tenant from, exactly like the reaper
// and the partition maintainer - and runners has row level security, so
// without it a lookup in multi-tenant mode would quietly find nothing.
type ReplicaRegistry struct {
	store  store.Store
	logger *zap.Logger

	id            string
	advertiseAddr string
	version       string

	heartbeatInterval time.Duration
	expiry            time.Duration
	observeCount      func(int)

	// registered records whether the row is in the database. A process that
	// failed to announce itself must not bind runners to an id no peer can
	// resolve, and must keep trying on the tick.
	registered atomic0

	mu     sync.Mutex
	cache  map[string]locateEntry
	stop   chan struct{}
	done   chan struct{}
	closed bool
}

// atomic0 is a tiny mutex-guarded bool. sync/atomic would do, but the registry
// already takes mu on every cache read and this keeps the state in one place.
type atomic0 struct {
	mu sync.RWMutex
	v  bool
}

func (a *atomic0) set(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

func (a *atomic0) get() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.v
}

// locateEntry is one cached answer about where a runner is held.
type locateEntry struct {
	peer    Peer
	present bool
	at      time.Time
}

// Peer is another replica, addressed.
type Peer struct {
	ReplicaID string
	Addr      string
}

// ReplicaRegistryConfig configures the registry.
type ReplicaRegistryConfig struct {
	// AdvertiseAddr is the host:port peers dial to hand this process a
	// command. Required: a replica that cannot be reached is worse than one
	// that never registered, because peers will route to it and fail.
	AdvertiseAddr string
	// Version is stamped on the row for operators. Optional.
	Version string
	// HeartbeatInterval and Expiry default to the constants above.
	HeartbeatInterval time.Duration
	Expiry            time.Duration
	// ID overrides the generated replica id. Tests use it; production never
	// should, because a fixed id shared by two processes would have them
	// overwrite each other's routing pointers.
	ID string
	// ObserveReplicaCount receives the number of live replicas after each
	// reap. Optional, and skipped entirely when unset - it costs a query per
	// tick, which is worth paying only for the gauge it feeds.
	ObserveReplicaCount func(int)
}

// ErrAdvertiseAddrRequired is returned when the registry has no address to
// publish.
var ErrAdvertiseAddrRequired = errors.New("core: replica advertise address is required")

// NewReplicaRegistry builds the registry. It does not touch the database;
// Start does.
func NewReplicaRegistry(s store.Store, cfg ReplicaRegistryConfig, logger *zap.Logger) (*ReplicaRegistry, error) {
	if cfg.AdvertiseAddr == "" {
		return nil, ErrAdvertiseAddrRequired
	}

	replicaID := cfg.ID
	if replicaID == "" {
		// Generated per process start and never persisted. A restarted process
		// holds none of the streams its predecessor held, so inheriting the id
		// would route commands into an empty connection map.
		replicaID = id.New("repl")
	}

	interval := cfg.HeartbeatInterval
	if interval <= 0 {
		interval = DefaultReplicaHeartbeatInterval
	}
	expiry := cfg.Expiry
	if expiry <= 0 {
		expiry = DefaultReplicaExpiry
	}

	version := cfg.Version
	if version == "" {
		if host, err := os.Hostname(); err == nil {
			version = host
		}
	}

	return &ReplicaRegistry{
		store:             s,
		logger:            logger,
		id:                replicaID,
		advertiseAddr:     cfg.AdvertiseAddr,
		version:           version,
		heartbeatInterval: interval,
		expiry:            expiry,
		observeCount:      cfg.ObserveReplicaCount,
		cache:             make(map[string]locateEntry),
		stop:              make(chan struct{}),
		done:              make(chan struct{}),
	}, nil
}

// ID is this process's replica id.
func (r *ReplicaRegistry) ID() string { return r.id }

// Start announces this process and begins heartbeating.
//
// A failed announcement is not fatal: the tick retries, and until it succeeds
// the registry reports itself unregistered so nothing binds runners to an id
// peers cannot resolve.
func (r *ReplicaRegistry) Start(ctx context.Context) error {
	if err := r.register(ctx); err != nil {
		r.logger.Error("could not announce this replica; cross-replica routing is degraded until the next tick",
			zap.String("replica_id", r.id), zap.Error(err))
	}

	go r.run()
	return nil
}

// Stop deregisters this process and waits for the heartbeat loop to exit.
//
// Deregistering on a clean shutdown is what makes a rolling restart quick: the
// pointers go immediately instead of waiting out the expiry, so peers stop
// routing to a process that has already closed its streams.
func (r *ReplicaRegistry) Stop(ctx context.Context) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.stop)
	r.mu.Unlock()

	<-r.done

	if err := r.store.DeleteServerReplica(store.WithSystemAccess(ctx), r.id); err != nil {
		r.logger.Warn("could not deregister this replica; peers will route to it until it expires",
			zap.String("replica_id", r.id), zap.Error(err))
	}
	r.registered.set(false)
}

func (r *ReplicaRegistry) run() {
	defer close(r.done)

	ticker := time.NewTicker(r.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(store.WithSystemAccess(context.Background()), r.heartbeatInterval)
			r.tick(ctx)
			cancel()
		}
	}
}

// tick keeps this replica alive and reaps the ones that are not.
func (r *ReplicaRegistry) tick(ctx context.Context) {
	if !r.registered.get() {
		if err := r.register(ctx); err != nil {
			r.logger.Warn("replica re-registration failed",
				zap.String("replica_id", r.id), zap.Error(err))
			return
		}
	} else if err := r.store.HeartbeatServerReplica(ctx, r.id); err != nil {
		// ErrNotFound means somebody reaped us during a stall. Re-announce on
		// the next tick rather than pretending the row is there.
		r.registered.set(false)
		if !errors.Is(err, store.ErrNotFound) {
			r.logger.Warn("replica heartbeat failed",
				zap.String("replica_id", r.id), zap.Error(err))
		}
		return
	}

	removed, err := r.store.DeleteExpiredServerReplicas(ctx, r.expiry)
	if err != nil {
		r.logger.Warn("could not reap expired replicas", zap.Error(err))
		return
	}
	if removed > 0 {
		r.logger.Info("reaped replicas that stopped heartbeating",
			zap.Int("count", removed))
	}

	if r.observeCount != nil {
		replicas, err := r.store.ListServerReplicas(ctx)
		if err != nil {
			r.logger.Debug("could not count live replicas", zap.Error(err))
			return
		}
		r.observeCount(len(replicas))
	}
}

func (r *ReplicaRegistry) register(ctx context.Context) error {
	replica := &store.ServerReplica{
		ID:            r.id,
		AdvertiseAddr: r.advertiseAddr,
	}
	if r.version != "" {
		replica.Version = &r.version
	}

	if err := r.store.RegisterServerReplica(store.WithSystemAccess(ctx), replica); err != nil {
		return err
	}

	r.registered.set(true)
	r.logger.Info("replica registered",
		zap.String("replica_id", r.id),
		zap.String("advertise_addr", r.advertiseAddr),
	)
	return nil
}

// BindRunner records that this process holds a runner's control stream.
//
// Called immediately after the stream registers, which is what makes this
// process the holder: the write is unconditional because the most recent
// successful registration is by definition the current one.
func (r *ReplicaRegistry) BindRunner(ctx context.Context, runnerID string) {
	if !r.registered.get() {
		// Binding to an id no peer can resolve would be worse than not binding
		// at all: the lookup would succeed and the hop would go nowhere.
		r.logger.Warn("not binding a runner connection: this replica is not registered",
			zap.String("runner_id", runnerID))
		return
	}

	if err := r.store.BindRunnerConnection(store.WithSystemAccess(ctx), runnerID, r.id); err != nil {
		r.logger.Warn("could not record the runner connection; commands from other replicas will not reach it",
			zap.String("runner_id", runnerID), zap.Error(err))
		return
	}
	r.invalidate(runnerID)
}

// ReleaseRunner clears the pointer, but only if this process still holds it.
//
// The condition is the fence. A stream that moved to another replica leaves
// this one running its deferred disconnect afterwards, and clearing
// unconditionally would delete the pointer that had just become correct.
func (r *ReplicaRegistry) ReleaseRunner(ctx context.Context, runnerID string) {
	if err := r.store.ReleaseRunnerConnection(store.WithSystemAccess(ctx), runnerID, r.id); err != nil {
		r.logger.Warn("could not clear the runner connection",
			zap.String("runner_id", runnerID), zap.Error(err))
	}
	r.invalidate(runnerID)
}

// Locate answers where a runner's stream is held.
//
// It returns false for "nobody holds it", for "this process holds it" (the
// caller has already checked its own map and should not hop to itself), and
// for a holder whose heartbeat has expired - a pointer that old is a promise
// the process cannot keep, and reporting it would turn a clean error into a
// timeout.
func (r *ReplicaRegistry) Locate(runnerID string) (Peer, bool) {
	if entry, ok := r.cached(runnerID); ok {
		return entry.peer, entry.present
	}

	ctx, cancel := context.WithTimeout(store.WithSystemAccess(context.Background()), 2*time.Second)
	defer cancel()

	conn, err := r.store.GetRunnerConnection(ctx, runnerID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return r.remember(runnerID, Peer{}, false)
	case err != nil:
		// Not cached: an error is not an answer, and caching it would extend
		// one bad query into a second of wrong routing.
		r.logger.Warn("could not locate the runner's replica",
			zap.String("runner_id", runnerID), zap.Error(err))
		return Peer{}, false
	}

	if conn.ReplicaID == r.id {
		return r.remember(runnerID, Peer{}, false)
	}
	if time.Since(conn.LastHeartbeatAt) > r.expiry {
		r.logger.Debug("ignoring a routing pointer to an expired replica",
			zap.String("runner_id", runnerID),
			zap.String("replica_id", conn.ReplicaID),
		)
		return r.remember(runnerID, Peer{}, false)
	}

	return r.remember(runnerID, Peer{ReplicaID: conn.ReplicaID, Addr: conn.AdvertiseAddr}, true)
}

func (r *ReplicaRegistry) cached(runnerID string) (locateEntry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.cache[runnerID]
	if !ok || time.Since(entry.at) > replicaLocateCacheTTL {
		return locateEntry{}, false
	}
	return entry, true
}

func (r *ReplicaRegistry) remember(runnerID string, peer Peer, present bool) (Peer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache[runnerID] = locateEntry{peer: peer, present: present, at: time.Now()}
	return peer, present
}

func (r *ReplicaRegistry) invalidate(runnerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.cache, runnerID)
}

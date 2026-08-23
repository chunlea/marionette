package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/tunnel"
)

// fakeTunnelStore is an in-memory stand-in for the tunnels table.
type fakeTunnelStore struct {
	mu      sync.Mutex
	tunnels map[string]*store.Tunnel
	byHash  map[string]*store.Tunnel
	expired int64
}

func newFakeTunnelStore() *fakeTunnelStore {
	return &fakeTunnelStore{
		tunnels: map[string]*store.Tunnel{},
		byHash:  map[string]*store.Tunnel{},
	}
}

func (f *fakeTunnelStore) CreateTunnel(_ context.Context, t *store.Tunnel) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tunnels[t.ID] = t
	f.byHash[t.TokenHash] = t
	return nil
}

func (f *fakeTunnelStore) GetTunnel(_ context.Context, id string) (*store.Tunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tunnels[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeTunnelStore) GetTunnelByTokenHash(_ context.Context, hash string) (*store.Tunnel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.byHash[hash]
	if !ok {
		return nil, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeTunnelStore) ListTunnels(_ context.Context, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var items []*store.Tunnel
	for _, t := range f.tunnels {
		if opts.SessionID != nil && t.SessionID != *opts.SessionID {
			continue
		}
		if !opts.IncludeClosed && t.ClosedAt != nil {
			continue
		}
		items = append(items, t)
	}
	return &store.ListResult[store.Tunnel]{Items: items, TotalCount: int64(len(items))}, nil
}

func (f *fakeTunnelStore) UpdateTunnel(_ context.Context, id string, updates store.TunnelUpdates) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tunnels[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.ClosedAt != nil {
		t.ClosedAt = updates.ClosedAt
	}
	if updates.PublicURL != nil {
		t.PublicURL = updates.PublicURL
	}
	return nil
}

func (f *fakeTunnelStore) DeleteExpiredTunnels(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired++
	return 0, nil
}

func (f *fakeTunnelStore) stored(id string) *store.Tunnel {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tunnels[id]
}

func tunnelDeps(s tunnelStore) wireTunnelsDeps {
	return wireTunnelsDeps{
		tunnelStore: s,
		baseURL:     "http://localhost:8080",
		logger:      zap.NewNop(),
	}
}

// TestNewTunnelManager_PersistsWhatItCreates is the anti-drift test for tunnel
// wiring, in the same shape as the admin one: build the manager the way
// production builds it and assert the property that was wrong.
//
// cmd/server built this manager without a store for the life of the project,
// so Create never wrote the tunnels table and every read-through path added
// since round 1 dead-ended on a nil store.
func TestNewTunnelManager_PersistsWhatItCreates(t *testing.T) {
	fake := newFakeTunnelStore()
	mgr := newTunnelManager(tunnelDeps(fake))
	ctx := context.Background()

	created, err := mgr.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_1",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	row := fake.stored(created.ID)
	require.NotNil(t, row, "a created tunnel must reach the database")
	assert.Equal(t, "sess_1", row.SessionID)
	require.NotNil(t, row.RunnerID)
	assert.Equal(t, "run_1", *row.RunnerID)
	assert.Equal(t, 3000, row.LocalPort)

	// token_prefix is NOT NULL, and the tunnel package's Store interface does
	// not carry it, so the adapter has to reconstruct it.
	assert.NotEmpty(t, row.TokenPrefix, "token_prefix is NOT NULL")
	assert.True(t, strings.HasPrefix(created.Token, row.TokenPrefix),
		"the stored prefix must be a prefix of the token it describes")
	assert.NotEqual(t, created.Token, row.TokenPrefix,
		"the whole token must never be stored as its display prefix")

	// The hash is what a token authenticates against after a restart.
	assert.NotEmpty(t, row.TokenHash)
	assert.NotContains(t, row.TokenHash, created.Token)
}

// TestNewTunnelManager_SurvivesARestart: the point of persistence. A manager
// with an empty cache but the same database must still resolve a tunnel, and
// still authenticate its token.
func TestNewTunnelManager_SurvivesARestart(t *testing.T) {
	fake := newFakeTunnelStore()
	ctx := context.Background()

	before := newTunnelManager(tunnelDeps(fake))
	created, err := before.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_1",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	// A new process: same database, empty cache.
	after := newTunnelManager(tunnelDeps(fake))

	got, err := after.Get(ctx, created.ID)
	require.NoError(t, err, "a tunnel created before the restart must still resolve")
	assert.Equal(t, created.ID, got.ID)

	validated, err := after.ValidateToken(ctx, created.ID, created.Token)
	require.NoError(t, err, "the token handed out before the restart must still authenticate")
	assert.Equal(t, created.ID, validated.ID)

	_, err = after.ValidateToken(ctx, created.ID, "ttok_not-the-token")
	assert.Error(t, err, "a wrong token must not authenticate after a restart either")
}

// TestNewTunnelManager_WithoutAStoreStaysInMemory documents the degraded
// shape rather than pretending it cannot happen: a server with no database
// still serves tunnels, they just do not survive it.
func TestNewTunnelManager_WithoutAStoreStaysInMemory(t *testing.T) {
	ctx := context.Background()

	before := newTunnelManager(tunnelDeps(nil))
	created, err := before.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_1",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	after := newTunnelManager(tunnelDeps(nil))
	_, err = after.Get(ctx, created.ID)
	assert.Error(t, err, "without a store a tunnel cannot outlive the process that made it")
}

// TestNewTunnelManager_CloseAndListReachTheStore covers the other two
// read-through paths, which were equally dead against a nil store.
func TestNewTunnelManager_CloseAndListReachTheStore(t *testing.T) {
	fake := newFakeTunnelStore()
	mgr := newTunnelManager(tunnelDeps(fake))
	ctx := context.Background()

	created, err := mgr.Create(ctx, tunnel.CreateTunnelOptions{
		SessionID: "sess_list",
		RunnerID:  "run_1",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	// A fresh process lists from the database, not from a cache it never had.
	listed, err := newTunnelManager(tunnelDeps(fake)).GetBySession(ctx, "sess_list")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, created.ID, listed[0].ID)

	require.NoError(t, mgr.Close(ctx, created.ID))
	assert.NotNil(t, fake.stored(created.ID).ClosedAt, "closing must be recorded, not just forgotten")
}

// TestTunnelTokenPrefix pins the reconstruction rule to the one cryptoutil
// used when the token was minted, so what lands in the column matches what the
// manager logged.
func TestTunnelTokenPrefix(t *testing.T) {
	token, displayPrefix, _, _, err := cryptoutil.GenerateTunnelToken()
	require.NoError(t, err)

	assert.Equal(t, displayPrefix, tunnelTokenPrefix(token),
		"the stored prefix must match the one cryptoutil produced")

	// The column is NOT NULL, so the degenerate inputs still have to yield
	// something.
	assert.NotEmpty(t, tunnelTokenPrefix(""))
	assert.Equal(t, cryptoutil.PrefixTunnelToken, tunnelTokenPrefix(""))
	assert.Equal(t, "ttok_ab", tunnelTokenPrefix("ttok_ab"), "a short token is its own prefix")
}

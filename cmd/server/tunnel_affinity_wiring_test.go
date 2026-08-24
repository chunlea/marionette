package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/server/api"
	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"github.com/chunlea/marionette/pkg/tunnel"
)

// The affinity proxy is exercised thoroughly in pkg/server/api. What is tested
// here is the half that lives in this file and nowhere else: that the handler
// production builds actually carries it.
//
// It landed with no call site at all - the lane that wrote it does not own
// cmd/server - so "built but not wired" was its literal state, which is the
// failure mode this whole restart exists to stop repeating.

// fixedLocator answers for one runner, the way the replica registry would.
type fixedLocator struct {
	runnerID string
	peer     grpcserver.RunnerPeer
}

func (l fixedLocator) Locate(runnerID string) (grpcserver.RunnerPeer, bool) {
	if runnerID != l.runnerID {
		return grpcserver.RunnerPeer{}, false
	}
	return l.peer, true
}

// TestWireTunnels_ProxiesToTheReplicaHoldingTheRunner drives a real request
// through the API server built from wireTunnels' options.
//
// Nothing here mocks the handler: the tunnel is read back out of the store by
// the manager wireTunnels constructs, the route comes from api.New, and the
// only fake is the registry answer that says the runner is somewhere else.
func TestWireTunnels_ProxiesToTheReplicaHoldingTheRunner(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RequestURI())
		mu.Unlock()

		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("served-by-the-holder"))
	}))
	defer peer.Close()

	peerHost, peerPort := splitHostPort(t, peer.URL)

	// A tunnel whose runner this process does not hold. Created through the
	// same manager wireTunnels builds, so what the wired one reads back is a
	// real row rather than a fixture.
	fake := newFakeTunnelStore()
	created, err := newTunnelManager(tunnelDeps(fake)).Create(context.Background(), tunnel.CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_elsewhere",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	// The registry publishes gRPC addresses; the API port is this process's
	// own. That derivation is the resolver's whole job, so the harness feeds it
	// the real shape: the peer's host with a gRPC port on it.
	affinity := api.NewTunnelAffinity(api.TunnelAffinityConfig{
		Locator: fixedLocator{
			runnerID: "run_elsewhere",
			peer:     grpcserver.RunnerPeer{ReplicaID: "repl_holder", Addr: net.JoinHostPort(peerHost, "9090")},
		},
		Resolver:  api.NewPeerAPIResolver(peerPort, false),
		ReplicaID: "repl_local",
		Logger:    zap.NewNop(),
	})
	require.NotNil(t, affinity)

	deps := tunnelDeps(fake)
	deps.affinity = affinity

	var apiOpts []api.Option
	wireTunnels(deps, &apiOpts)

	local := httptest.NewServer(api.New(api.Config{}, zap.NewNop(), apiOpts...).Router())
	defer local.Close()

	req, err := http.NewRequest(http.MethodGet, local.URL+"/tunnels/"+created.ID+"/echo?q=1", nil)
	require.NoError(t, err)
	req.Header.Set("X-Marionette-Tunnel-Token", created.Token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "served-by-the-holder", string(body),
		"the request must be served by the replica holding the runner, not locally")

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 1, "exactly one leg: proxied once, not retried")
	assert.Equal(t, "/tunnels/"+created.ID+"/echo?q=1", seen[0],
		"the peer must see the request as the client sent it, prefix included")
}

// TestWireTunnels_WithoutAffinityServesLocally documents the single-replica
// shape: with no affinity there is nowhere to proxy to, and the request takes
// the local path (which, with no runner attached, fails here rather than
// silently succeeding somewhere else).
func TestWireTunnels_WithoutAffinityServesLocally(t *testing.T) {
	fake := newFakeTunnelStore()
	created, err := newTunnelManager(tunnelDeps(fake)).Create(context.Background(), tunnel.CreateTunnelOptions{
		SessionID: "sess_1",
		RunnerID:  "run_elsewhere",
		Type:      tunnel.TypeHTTP,
		LocalPort: 3000,
	})
	require.NoError(t, err)

	var apiOpts []api.Option
	wireTunnels(tunnelDeps(fake), &apiOpts)

	local := httptest.NewServer(api.New(api.Config{}, zap.NewNop(), apiOpts...).Router())
	defer local.Close()

	req, err := http.NewRequest(http.MethodGet, local.URL+"/tunnels/"+created.ID+"/echo", nil)
	require.NoError(t, err)
	req.Header.Set("X-Marionette-Tunnel-Token", created.Token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.NotEqual(t, http.StatusOK, resp.StatusCode,
		"with no replica to proxy to and no runner attached, the request must fail here")
}

func splitHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)

	host, portStr, err := net.SplitHostPort(parsed.Host)
	require.NoError(t, err)

	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, port
}

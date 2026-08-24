package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// The two-app harness.
//
// Two tunnel entry points, A and B, each a real TunnelProxyHandler behind a
// real HTTP server, exactly as cmd/server mounts them. B holds the runner: its
// tunnel service writes to a real backend over a real TCP connection, which is
// what marionette-agent does at the far end. A holds nothing and its locator
// says the runner is on B.
//
// A request that enters A must come back with B's bytes, and B's backend must
// see it as though the client had called B directly.

const affinityTestToken = "ttok_affinity_test"

// fakeRunnerLocator is the routing registry, mocked. It counts lookups so a
// test can assert the loop guard consulted it and still refused to hop.
type fakeRunnerLocator struct {
	peers map[string]RunnerPeer
	calls atomic.Int32
}

func (f *fakeRunnerLocator) Locate(runnerID string) (RunnerPeer, bool) {
	f.calls.Add(1)
	peer, ok := f.peers[runnerID]
	return peer, ok
}

// fakeRunnerTunnel stands in for a runner holding a tunnel.
//
// It takes the serialized HTTP request the entry point produces, writes it to
// a real backend over a real TCP connection and streams the bytes back. That
// is what the agent does, so what this exercises is the wire behaviour rather
// than an invented one.
type fakeRunnerTunnel struct {
	backendAddr string
	tunnelID    string
	runnerID    string

	mu         sync.Mutex
	conns      map[string]net.Conn
	tokensSeen []string
}

func newFakeRunnerTunnel(backendURL, tunnelID, runnerID string) *fakeRunnerTunnel {
	u, _ := url.Parse(backendURL)
	return &fakeRunnerTunnel{
		backendAddr: u.Host,
		tunnelID:    tunnelID,
		runnerID:    runnerID,
		conns:       make(map[string]net.Conn),
	}
}

func (f *fakeRunnerTunnel) ValidateTunnel(_ context.Context, tunnelID string) (*TunnelInfo, error) {
	return &TunnelInfo{
		ID:        tunnelID,
		Type:      "http",
		RunnerID:  f.runnerID,
		SessionID: "sess_affinity",
		IsPublic:  false,
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeRunnerTunnel) ValidateTunnelToken(_ context.Context, _, token string) (bool, error) {
	f.mu.Lock()
	f.tokensSeen = append(f.tokensSeen, token)
	f.mu.Unlock()
	return token == affinityTestToken, nil
}

func (f *fakeRunnerTunnel) seenTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.tokensSeen...)
}

func (f *fakeRunnerTunnel) SendRequest(_ context.Context, _, connectionID string, data []byte) (<-chan []byte, error) {
	conn, err := net.Dial("tcp", f.backendAddr)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	f.conns[connectionID] = conn
	f.mu.Unlock()

	if _, err := conn.Write(data); err != nil {
		f.CloseConnection(connectionID)
		return nil, err
	}

	ch := make(chan []byte, 32)
	go func() {
		defer close(ch)
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case ch <- chunk:
				case <-time.After(5 * time.Second):
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return ch, nil
}

func (f *fakeRunnerTunnel) SendData(_ context.Context, _, connectionID string, data []byte, eof bool) error {
	f.mu.Lock()
	conn, ok := f.conns[connectionID]
	f.mu.Unlock()
	if !ok {
		return net.ErrClosed
	}

	if len(data) > 0 {
		if _, err := conn.Write(data); err != nil {
			return err
		}
	}
	if eof {
		f.CloseConnection(connectionID)
	}
	return nil
}

func (f *fakeRunnerTunnel) CloseConnection(connectionID string) {
	f.mu.Lock()
	conn, ok := f.conns[connectionID]
	delete(f.conns, connectionID)
	f.mu.Unlock()

	if ok {
		_ = conn.Close()
	}
}

// affinityReplica is one server process in the harness.
type affinityReplica struct {
	id      string
	server  *httptest.Server
	tunnel  *fakeRunnerTunnel
	locator *fakeRunnerLocator
	host    string
	port    int
}

// newAffinityReplica mounts a tunnel entry point the way cmd/server does.
func newAffinityReplica(t *testing.T, id string, tun *fakeRunnerTunnel, locator *fakeRunnerLocator, peerAPIPort int) *affinityReplica {
	return newAffinityReplicaWithLogger(t, id, tun, locator, peerAPIPort, zaptest.NewLogger(t))
}

// newAffinityReplicaWithLogger is the same, for the tests whose handler
// goroutines outlive the test body.
//
// A hijacked WebSocket relay is not waited for by httptest.Server.Close, so it
// can still be logging its teardown after the test function has returned -
// and a zaptest logger writing to a finished *testing.T is a data race in the
// harness that says nothing about the code under test.
func newAffinityReplicaWithLogger(
	t *testing.T,
	id string,
	tun *fakeRunnerTunnel,
	locator *fakeRunnerLocator,
	peerAPIPort int,
	logger *zap.Logger,
) *affinityReplica {
	t.Helper()

	var affinity *TunnelAffinity
	if locator != nil {
		affinity = NewTunnelAffinity(TunnelAffinityConfig{
			Locator:   locator,
			Resolver:  NewPeerAPIResolver(peerAPIPort, false),
			ReplicaID: id,
			Logger:    logger,
		})
		require.NotNil(t, affinity)
	}

	h := NewTunnelProxyHandler(
		WithTPLogger(logger),
		WithTPService(tun),
		WithTPAffinity(affinity),
	)

	r := chi.NewRouter()
	r.HandleFunc("/tunnels/{tunnelID}/*", h.ServeHTTP)
	r.HandleFunc("/tunnels/{tunnelID}", h.ServeHTTP)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	host, portStr, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return &affinityReplica{id: id, server: srv, tunnel: tun, locator: locator, host: host, port: port}
}

// grpcAddr is what a replica publishes in the routing registry: its gRPC
// address, not its API one. Resolving the API address out of it is the whole
// job of PeerAPIResolver, so the harness feeds it the real shape.
func (r *affinityReplica) grpcAddr() string {
	return net.JoinHostPort(r.host, "9090")
}

func (r *affinityReplica) peer() RunnerPeer {
	return RunnerPeer{ReplicaID: r.id, Addr: r.grpcAddr()}
}

// newEchoBackend is the user's own service at the far end of the tunnel. It
// records what it was asked, so a test can assert what did and did not survive
// the crossing.
func newEchoBackend(t *testing.T, body string) (*httptest.Server, func() []*http.Request) {
	t.Helper()

	var (
		mu   sync.Mutex
		seen []*http.Request
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(context.Background()))
		mu.Unlock()

		payload := body
		if r.Body != nil {
			if in, err := io.ReadAll(r.Body); err == nil && len(in) > 0 {
				payload = body + ":" + string(in)
			}
		}

		// Closing the connection is what ends the tunnel's byte stream: the
		// runner side has no framing of its own, so a keep-alive backend would
		// leave the entry point waiting for bytes that never come.
		w.Header().Set("Connection", "close")
		w.Header().Set("X-Backend-Path", r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		return append([]*http.Request(nil), seen...)
	}
}

func tunnelRequest(t *testing.T, method, rawURL, body string) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	require.NoError(t, err)
	req.Header.Set("X-Marionette-Tunnel-Token", affinityTestToken)
	return req
}

// TestTunnelAffinity_CrossReplicaHTTP is the crossing test: the tunnel is held
// by B's runner, the request enters A, and the bytes round-trip.
func TestTunnelAffinity_CrossReplicaHTTP(t *testing.T) {
	backendB, backendBRequests := newEchoBackend(t, "hello-from-b")

	// A's own backend answers differently, so a response that did not cross is
	// not mistaken for one that did.
	backendA, backendARequests := newEchoBackend(t, "hello-from-a")

	// B holds the runner. No locator: everything it sees is its own.
	replicaB := newAffinityReplica(t, "repl_b",
		newFakeRunnerTunnel(backendB.URL, "tun_cross", "run_b"), nil, 0)

	// A holds nothing. Its registry says run_b is on B, and it only knows B's
	// gRPC address - the API port comes from its own configuration.
	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{"run_b": replicaB.peer()}}
	replicaA := newAffinityReplica(t, "repl_a",
		newFakeRunnerTunnel(backendA.URL, "tun_cross", "run_b"), locator, replicaB.port)

	req := tunnelRequest(t, http.MethodPost,
		replicaA.server.URL+"/tunnels/tun_cross/echo?q=1", "payload-from-client")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "hello-from-b:payload-from-client", string(body),
		"the response must come from the replica holding the runner, with the request body intact")
	assert.Equal(t, "/echo?q=1", resp.Header.Get("X-Backend-Path"),
		"the backend's own response headers must survive both legs")
	assert.Empty(t, backendARequests(), "A must not have served this itself")

	require.Len(t, backendBRequests(), 1, "exactly one request reached the backend: one leg, not a retry")
	assert.Equal(t, "/echo?q=1", backendBRequests()[0].URL.RequestURI())

	// The proxied leg carried the caller's own credential; B authenticated it
	// itself. No second peer credential exists for this path.
	assert.Equal(t, []string{affinityTestToken}, replicaB.tunnel.seenTokens())
	assert.Empty(t, backendBRequests()[0].Header.Get("X-Marionette-Tunnel-Token"),
		"the tunnel token must not reach the user's service")
	assert.Empty(t, backendBRequests()[0].Header.Get(TunnelHopHeader),
		"the hop marker is routing bookkeeping and must not reach the user's service")
}

// TestTunnelAffinity_CrossReplicaWebSocket is the same crossing for a
// long-lived byte stream: after the upgrade, every frame flows on the
// connection B already owns.
func TestTunnelAffinity_CrossReplicaWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(msgType, append([]byte("echo:"), msg...)); err != nil {
				return
			}
		}
	}))
	defer backend.Close()

	replicaB := newAffinityReplicaWithLogger(t, "repl_b",
		newFakeRunnerTunnel(backend.URL, "tun_ws", "run_b"), nil, 0, zap.NewNop())

	// A's own tunnel leads to a plain HTTP server that refuses to upgrade, so
	// an upgrade that did not cross cannot be mistaken for one that did.
	notAWebSocket, _ := newEchoBackend(t, "not-a-websocket")

	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{"run_b": replicaB.peer()}}
	replicaA := newAffinityReplicaWithLogger(t, "repl_a",
		newFakeRunnerTunnel(notAWebSocket.URL, "tun_ws", "run_b"), locator, replicaB.port, zap.NewNop())

	wsURL := "ws" + strings.TrimPrefix(replicaA.server.URL, "http") + "/tunnels/tun_ws/socket"
	header := http.Header{}
	header.Set("X-Marionette-Tunnel-Token", affinityTestToken)

	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, header)
	require.NoError(t, err, "the upgrade must cross both legs")
	defer func() { _ = conn.Close() }()
	if resp != nil {
		_ = resp.Body.Close()
	}
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Several frames in each direction: the point of affinity is that these
	// cost nothing extra once the request has landed.
	for i := range 3 {
		payload := "frame-" + strconv.Itoa(i)
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(payload)))

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
		_, got, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, "echo:"+payload, string(got))
	}
}

// TestTunnelAffinity_LoopGuard: a request that has already taken its one leg
// is served where it lands, whatever the registry says.
func TestTunnelAffinity_LoopGuard(t *testing.T) {
	backendA, _ := newEchoBackend(t, "served-by-a")
	backendB, backendBRequests := newEchoBackend(t, "served-by-b")

	replicaB := newAffinityReplica(t, "repl_b",
		newFakeRunnerTunnel(backendB.URL, "tun_loop", "run_b"), nil, 0)

	// A's registry says B holds the runner, and A would normally proxy.
	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{"run_b": replicaB.peer()}}
	replicaA := newAffinityReplica(t, "repl_a",
		newFakeRunnerTunnel(backendA.URL, "tun_loop", "run_b"), locator, replicaB.port)

	req := tunnelRequest(t, http.MethodGet, replicaA.server.URL+"/tunnels/tun_loop/echo", "")
	req.Header.Set(TunnelHopHeader, "repl_somebody_else")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "served-by-a", string(body),
		"an already-hopped request is served locally rather than taking a second leg")
	assert.Empty(t, backendBRequests(), "the request must not have reached B")
	assert.Positive(t, locator.calls.Load(),
		"the guard still asks the registry, so the mismatch can be logged")
}

// TestTunnelAffinity_DeadPeer: the replica holding the tunnel is gone. The
// failure names it instead of blaming the runner.
func TestTunnelAffinity_DeadPeer(t *testing.T) {
	backend, _ := newEchoBackend(t, "unreachable")

	// A port nothing is listening on: a replica that died between its last
	// heartbeat and this request.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadPort := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{
		"run_dead": {ReplicaID: "repl_dead", Addr: "127.0.0.1:9090"},
	}}
	replicaA := newAffinityReplica(t, "repl_a",
		newFakeRunnerTunnel(backend.URL, "tun_dead", "run_dead"), locator, deadPort)

	req := tunnelRequest(t, http.MethodGet, replicaA.server.URL+"/tunnels/tun_dead/echo", "")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, string(body), "repl_dead", "the 502 must name the replica that is not answering")
	assert.Contains(t, string(body), strconv.Itoa(deadPort), "and the address it was tried on")
}

// TestTunnelAffinity_UnaddressablePeer: the registry knows the holder but its
// published address cannot be turned into an API address.
func TestTunnelAffinity_UnaddressablePeer(t *testing.T) {
	backend, _ := newEchoBackend(t, "unaddressable")

	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{
		"run_odd": {ReplicaID: "repl_odd", Addr: "no-port-here"},
	}}
	replicaA := newAffinityReplica(t, "repl_a",
		newFakeRunnerTunnel(backend.URL, "tun_odd", "run_odd"), locator, 8080)

	req := tunnelRequest(t, http.MethodGet, replicaA.server.URL+"/tunnels/tun_odd/echo", "")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
	assert.Contains(t, string(body), "repl_odd")
}

// TestTunnelAffinity_LocalRunnerIsNotProxied: the single-replica path, and the
// path a multi-replica deployment takes for every tunnel it does hold. Locate
// reports false for a local runner, so nothing is proxied.
func TestTunnelAffinity_LocalRunnerIsNotProxied(t *testing.T) {
	backend, backendRequests := newEchoBackend(t, "served-locally")

	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{}}
	replicaA := newAffinityReplica(t, "repl_a",
		newFakeRunnerTunnel(backend.URL, "tun_local", "run_a"), locator, 8080)

	req := tunnelRequest(t, http.MethodGet, replicaA.server.URL+"/tunnels/tun_local/echo", "")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "served-locally", string(body))
	assert.Len(t, backendRequests(), 1)
}

// TestTunnelAffinity_RefusesToProxyToItself: Locate already excludes this
// process, but a registry that ever answered otherwise must not produce a
// request that loops back into the same handler.
func TestTunnelAffinity_RefusesToProxyToItself(t *testing.T) {
	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{
		"run_self": {ReplicaID: "repl_a", Addr: "127.0.0.1:9090"},
	}}
	affinity := NewTunnelAffinity(TunnelAffinityConfig{
		Locator:   locator,
		Resolver:  NewPeerAPIResolver(8080, false),
		ReplicaID: "repl_a",
		Logger:    zaptest.NewLogger(t),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_self/x", nil)

	assert.False(t, affinity.serve(rec, req, "tun_self", "run_self"))
}

// TestTunnelAffinity_NilIsInert: a single-process deployment builds no
// affinity proxy at all, and the call site must not have to know that.
func TestTunnelAffinity_NilIsInert(t *testing.T) {
	assert.Nil(t, NewTunnelAffinity(TunnelAffinityConfig{}),
		"no locator means no cross-replica routing")

	var affinity *TunnelAffinity
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_x/y", nil)

	assert.False(t, affinity.serve(rec, req, "tun_x", "run_x"))
	assert.Equal(t, http.StatusOK, rec.Code, "nothing was written")
}

// TestTunnelAffinity_NoRunnerIsNotProxied: a tunnel with no runner has no
// holder to proxy to, and asking the registry for "" would be a wasted query.
func TestTunnelAffinity_NoRunnerIsNotProxied(t *testing.T) {
	locator := &fakeRunnerLocator{peers: map[string]RunnerPeer{}}
	affinity := NewTunnelAffinity(TunnelAffinityConfig{
		Locator:   locator,
		Resolver:  NewPeerAPIResolver(8080, false),
		ReplicaID: "repl_a",
		Logger:    zaptest.NewLogger(t),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tunnels/tun_x/y", nil)

	assert.False(t, affinity.serve(rec, req, "tun_x", ""))
	assert.Zero(t, locator.calls.Load())
}

func TestPeerAPIResolver_KeepsTheHostAndSubstitutesThePort(t *testing.T) {
	resolver := NewPeerAPIResolver(8080, false)

	target, err := resolver.Resolve(RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2:9090"})
	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.2:8080", target.String())
}

func TestPeerAPIResolver_IPv6(t *testing.T) {
	resolver := NewPeerAPIResolver(8080, false)

	target, err := resolver.Resolve(RunnerPeer{ReplicaID: "repl_b", Addr: "[fd00::2]:9090"})
	require.NoError(t, err)
	assert.Equal(t, "http://[fd00::2]:8080", target.String())
}

func TestPeerAPIResolver_TLSUsesHTTPS(t *testing.T) {
	resolver := NewPeerAPIResolver(8443, true)

	target, err := resolver.Resolve(RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2:9090"})
	require.NoError(t, err)
	assert.Equal(t, "https://10.0.0.2:8443", target.String())
}

// TestPeerAPIResolver_EnvOverrides is the escape hatch for a deployment where
// the API is not on the port this process binds.
func TestPeerAPIResolver_EnvOverrides(t *testing.T) {
	t.Setenv(EnvPeerAPIPort, "18080")
	t.Setenv(EnvPeerAPIScheme, "https")

	resolver := NewPeerAPIResolver(8080, false)

	target, err := resolver.Resolve(RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2:9090"})
	require.NoError(t, err)
	assert.Equal(t, "https://10.0.0.2:18080", target.String())
}

func TestPeerAPIResolver_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		peer    RunnerPeer
		wantErr string
	}{
		{
			name:    "no api port configured",
			port:    0,
			peer:    RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2:9090"},
			wantErr: EnvPeerAPIPort,
		},
		{
			name:    "peer publishes nothing",
			port:    8080,
			peer:    RunnerPeer{ReplicaID: "repl_b"},
			wantErr: "publishes no address",
		},
		{
			name:    "peer address is not host:port",
			port:    8080,
			peer:    RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2"},
			wantErr: "not host:port",
		},
		{
			name:    "peer address has no host",
			port:    8080,
			peer:    RunnerPeer{ReplicaID: "repl_b", Addr: ":9090"},
			wantErr: "no host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPeerAPIResolver(tt.port, false).Resolve(tt.peer)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPeerAPIResolver_NilRejects(t *testing.T) {
	var resolver *PeerAPIResolver
	_, err := resolver.Resolve(RunnerPeer{ReplicaID: "repl_b", Addr: "10.0.0.2:9090"})
	require.Error(t, err)
}

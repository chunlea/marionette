package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	grpcserver "github.com/chunlea/marionette/pkg/server/grpc"
	"go.uber.org/zap"
)

// Cross-replica tunnel affinity.
//
// Round 5 made commands correct across replicas by hopping one command at a
// time: SendCommand asks the registry which process holds the runner's control
// stream and forwards the command there. That is the right shape for a
// command. It is the wrong shape for a tunnel.
//
// A tunnel's data path is TunnelData per chunk. Routing it through the command
// hop would put a replica-to-replica round trip in front of every 32KB of a
// byte stream - an HTTP body, a WebSocket frame, an SSE tail - and would do it
// in both directions. The routing proposal named the answer in section 4.8 and
// left it as a follow-up: affinity. The HTTP entry point proxies the WHOLE
// request to the replica that holds the runner, and every chunk after that
// flows on the connection that replica already owns.
//
// One extra network leg per REQUEST. Zero per chunk.

// TunnelHopHeader marks a request that has already been proxied to the replica
// holding the runner.
//
// It is the loop guard. A request carrying it is never proxied again, whatever
// the registry says, so a stale or circular routing pointer costs one wasted
// leg rather than a request bouncing between two processes until it times out.
const TunnelHopHeader = "X-Marionette-Tunnel-Hop"

// Environment overrides for peer API address derivation. Both mirror
// MARIONETTE_GRPC_ADVERTISE_ADDR: a derivation that is right for every
// deployment we ship, plus an escape hatch for the one that is not.
const (
	// EnvPeerAPIPort overrides the port peers' public API is reached on.
	EnvPeerAPIPort = "MARIONETTE_PEER_API_PORT"
	// EnvPeerAPIScheme overrides the scheme peers' public API is reached on.
	EnvPeerAPIScheme = "MARIONETTE_PEER_API_SCHEME"
)

// Timeouts for the proxied leg.
//
// None of them bound the response body: a tunnel carries SSE tails and
// WebSocket sessions that are meant to stay open for hours. What they bound is
// getting to the first byte, which is where a dead peer shows up.
const (
	peerDialTimeout           = 5 * time.Second
	peerTLSHandshakeTimeout   = 10 * time.Second
	peerResponseHeaderTimeout = 60 * time.Second
	peerIdleConnTimeout       = 90 * time.Second
	peerMaxIdleConnsPerHost   = 32
)

// RunnerLocator answers which process holds a runner's control stream.
//
// Aliased rather than redeclared. "Where is this runner" has exactly one
// answer in this server and it is the replica registry; a second interface
// with the same shape would be a second place for that answer to drift, and
// would make the wiring in cmd/server carry a third adapter for a type it
// already has.
type RunnerLocator = grpcserver.RunnerLocator

// RunnerPeer is another replica, addressed by the gRPC address it publishes.
type RunnerPeer = grpcserver.RunnerPeer

// PeerAPIResolver turns the address a replica publishes in the routing
// registry into the base URL of its public API.
//
// The registry stores one address per replica and it is the gRPC one - that is
// what peers dial to hand each other a command. A tunnel needs the API
// address, and the registry does not carry it.
//
// Rather than add a column, this derives it: keep the host the peer published,
// substitute the API port this process is configured with. That uses the only
// two facts that are actually true. The host is per-process and only the peer
// knows it. The port is deployment-wide: every replica is the same image
// reading the same configuration - a single Kubernetes Deployment is what the
// shipped overlay scales - so this process's API port is also its peers'.
//
// Where that does not hold - a deployment publishing a shared load-balancer
// name for gRPC rather than a per-pod address - EnvPeerAPIPort and
// EnvPeerAPIScheme override the derivation, and a request that still lands on
// the wrong replica is caught by the hop guard rather than looping.
type PeerAPIResolver struct {
	scheme string
	port   string
}

// NewPeerAPIResolver builds the resolver from this process's own API listener,
// with the environment overrides applied on top.
//
// An apiPort of zero with no override yields a resolver that refuses to
// resolve, rather than one that silently produces "host:0".
func NewPeerAPIResolver(apiPort int, tlsEnabled bool) *PeerAPIResolver {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	if override := os.Getenv(EnvPeerAPIScheme); override != "" {
		scheme = override
	}

	port := ""
	if apiPort > 0 {
		port = strconv.Itoa(apiPort)
	}
	if override := os.Getenv(EnvPeerAPIPort); override != "" {
		port = override
	}

	return &PeerAPIResolver{scheme: scheme, port: port}
}

// Resolve returns the base URL of a peer's public API.
func (r *PeerAPIResolver) Resolve(peer RunnerPeer) (*url.URL, error) {
	if r == nil || r.port == "" {
		return nil, fmt.Errorf("no API port configured for peer replicas (set %s)", EnvPeerAPIPort)
	}
	if peer.Addr == "" {
		return nil, fmt.Errorf("replica %s publishes no address", peer.ReplicaID)
	}

	host, _, err := net.SplitHostPort(peer.Addr)
	if err != nil {
		return nil, fmt.Errorf("replica %s publishes %q, which is not host:port: %w",
			peer.ReplicaID, peer.Addr, err)
	}
	if host == "" {
		return nil, fmt.Errorf("replica %s publishes %q, which has no host", peer.ReplicaID, peer.Addr)
	}

	return &url.URL{Scheme: r.scheme, Host: net.JoinHostPort(host, r.port)}, nil
}

// TunnelAffinity proxies a tunnel request to the replica holding its runner.
type TunnelAffinity struct {
	locator   RunnerLocator
	resolver  *PeerAPIResolver
	replicaID string
	proxy     *httputil.ReverseProxy
	logger    *zap.Logger
}

// TunnelAffinityConfig configures the affinity proxy.
type TunnelAffinityConfig struct {
	// Locator is the routing registry. Nil disables affinity entirely, which
	// is the correct behaviour for a single-process deployment: every runner
	// is local, so there is never anywhere to proxy to.
	Locator RunnerLocator
	// Resolver maps a peer's published address to its API base URL. Required
	// when Locator is set.
	Resolver *PeerAPIResolver
	// ReplicaID is this process's registry id. It is stamped on the hop header
	// so the target's logs name the origin, and it lets this process refuse to
	// proxy to itself.
	ReplicaID string
	// Logger is optional.
	Logger *zap.Logger
}

// NewTunnelAffinity builds the affinity proxy. A nil result is legitimate and
// means "no cross-replica proxying" - the handler treats it as such.
func NewTunnelAffinity(cfg TunnelAffinityConfig) *TunnelAffinity {
	if cfg.Locator == nil {
		return nil
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	a := &TunnelAffinity{
		locator:   cfg.Locator,
		resolver:  cfg.Resolver,
		replicaID: cfg.ReplicaID,
		logger:    logger,
	}

	a.proxy = &httputil.ReverseProxy{
		Rewrite:   a.rewrite,
		Transport: newPeerTransport(),
		// Flush every write straight through. A tunnel carries SSE and chunked
		// responses whose whole point is that the bytes arrive when they are
		// produced; the default
		// buffering would hold them.
		FlushInterval: -1,
		ErrorHandler:  a.handleError,
		ErrorLog:      zap.NewStdLog(logger),
	}

	return a
}

// newPeerTransport is the transport for the proxied leg.
//
// HTTP/2 is deliberately off. A tunnel request may be a WebSocket upgrade, and
// 101 Switching Protocols is an HTTP/1.1 mechanism: negotiating h2 with a peer
// over TLS would break every upgrade that crosses replicas.
func newPeerTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   peerDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   peerTLSHandshakeTimeout,
		ResponseHeaderTimeout: peerResponseHeaderTimeout,
		IdleConnTimeout:       peerIdleConnTimeout,
		MaxIdleConnsPerHost:   peerMaxIdleConnsPerHost,
	}
}

// affinityTargetKey carries the resolved target from serve to rewrite and to
// the error handler. ReverseProxy clones the inbound request's context, so a
// value stashed here reaches both.
type affinityTargetKey struct{}

type affinityTarget struct {
	peer RunnerPeer
	url  *url.URL
}

// serve proxies the request to the replica holding runnerID, and reports
// whether it handled it.
//
// False means "this request is ours to serve": no locator, no runner, the
// runner is local, its holder has expired, or this request has already taken
// its one leg. The caller falls through to the local path, which stays exactly
// what it is today.
//
// It is nil-safe on purpose. A single-process deployment builds no affinity
// proxy at all and the call site should not have to know that.
func (a *TunnelAffinity) serve(w http.ResponseWriter, r *http.Request, tunnelID, runnerID string) bool {
	if a == nil || a.locator == nil || runnerID == "" {
		return false
	}

	// The loop guard, checked before the registry so that no lookup can talk
	// this process into a second leg. A request that arrives already hopped is
	// served here whatever the registry says: if the pointer moved between the
	// origin's lookup and now, the local path still reaches the runner through
	// the round-5 command hop - slower per chunk, but correct - and that is
	// strictly better than bouncing the request back.
	if origin := r.Header.Get(TunnelHopHeader); origin != "" {
		if peer, ok := a.locator.Locate(runnerID); ok {
			a.logger.Warn("a proxied tunnel request landed on a replica that does not hold the runner; "+
				"serving it here rather than taking a second leg",
				zap.String("tunnel_id", tunnelID),
				zap.String("runner_id", runnerID),
				zap.String("origin_replica_id", origin),
				zap.String("holder_replica_id", peer.ReplicaID),
			)
		}
		return false
	}

	peer, ok := a.locator.Locate(runnerID)
	if !ok {
		// Nobody holds it, this process holds it, or the holder's heartbeat
		// expired. Locate reports all three the same way and all three mean
		// the same thing here: there is nowhere to proxy to.
		return false
	}
	if peer.ReplicaID == "" || peer.ReplicaID == a.replicaID {
		return false
	}

	target, err := a.resolver.Resolve(peer)
	if err != nil {
		// We know who holds the tunnel and cannot address them. Saying so is
		// the whole point: the local path would fail later, further from the
		// cause, as a generic "tunnel unavailable".
		a.logger.Error("cannot resolve the API address of the replica holding this tunnel",
			zap.String("tunnel_id", tunnelID),
			zap.String("runner_id", runnerID),
			zap.String("holder_replica_id", peer.ReplicaID),
			zap.String("holder_addr", peer.Addr),
			zap.Error(err),
		)
		http.Error(w, fmt.Sprintf("tunnel is held by replica %s, which could not be addressed: %v",
			peer.ReplicaID, err), http.StatusBadGateway)
		return true
	}

	a.logger.Debug("proxying a tunnel request to the replica holding its runner",
		zap.String("tunnel_id", tunnelID),
		zap.String("runner_id", runnerID),
		zap.String("holder_replica_id", peer.ReplicaID),
		zap.String("target", target.String()),
	)

	ctx := context.WithValue(r.Context(), affinityTargetKey{}, &affinityTarget{peer: peer, url: target})
	a.proxy.ServeHTTP(w, r.WithContext(ctx))
	return true
}

// rewrite points the outbound request at the peer and marks it as hopped.
//
// The request is otherwise left alone - same method, same path, same query,
// same headers, same body. That is deliberate: the peer authenticates it as
// the original caller, using the tunnel token (or API key) the client sent,
// both of which validate against the database on any replica. No second peer
// credential is invented for this leg, and none is needed.
//
// X-Forwarded-* is not added either. The peer sanitizes the request before
// handing it to the runner, so anything added here would be seen by the user's
// own service at the other end of the tunnel; the extra leg is our
// implementation detail and should not show up in their logs.
func (a *TunnelAffinity) rewrite(pr *httputil.ProxyRequest) {
	target, ok := pr.In.Context().Value(affinityTargetKey{}).(*affinityTarget)
	if !ok {
		// serve is the only caller and always sets it.
		return
	}

	pr.SetURL(target.url)
	pr.Out.Header.Set(TunnelHopHeader, a.hopValue())
}

// hopValue is what the guard looks for. The replica id makes a log line say
// who sent the request; the fallback keeps the guard working when this process
// has no registry id yet, because an unmarked hop is a loop.
func (a *TunnelAffinity) hopValue() string {
	if a.replicaID != "" {
		return a.replicaID
	}
	return "1"
}

// handleError turns a failed leg into a 502 that names the replica.
//
// "tunnel unavailable" is what this used to look like from the client side and
// it sent operators to the runner, which was healthy the whole time. The
// replica id and address are the two facts that point at the real problem.
func (a *TunnelAffinity) handleError(w http.ResponseWriter, r *http.Request, err error) {
	target, _ := r.Context().Value(affinityTargetKey{}).(*affinityTarget)

	replicaID, addr := "unknown", "unknown"
	if target != nil {
		replicaID, addr = target.peer.ReplicaID, target.url.Host
	}

	a.logger.Error("could not reach the replica holding this tunnel",
		zap.String("holder_replica_id", replicaID),
		zap.String("holder_api_addr", addr),
		zap.String("path", r.URL.Path),
		zap.Error(err),
	)

	http.Error(w, fmt.Sprintf("replica %s (%s), which holds this tunnel, is unreachable: %v",
		replicaID, addr, err), http.StatusBadGateway)
}

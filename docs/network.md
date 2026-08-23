# Network Isolation

How Marionette restricts what a coding agent can reach, what each level
actually guarantees, and what it does not.

This document describes what the code does. Where it says "cannot", that is a
real limit of the mechanism, not a to-do.

## Levels

A session picks one level. The level is stored on the session
(`sessions.network_policy`) and applied to every runner the session is given.

> **A session created without an explicit level currently defaults to
> `allow_list` with an empty host list** (`SessionManager.Create`). That policy
> is rejected — an allow list has to allow something — so a managed provider
> cannot spawn a runner for such a session at all. Pass an explicit level, or
> an explicit host list, until that default is changed.

| Level | Egress | Use when |
|-------|--------|----------|
| `none` | Unrestricted | Trusted work, local development |
| `allow_list` | Named hosts and networks, resolved and pinned | The agent needs specific registries and APIs |
| `proxy` | Only through a configured egress proxy | You need an audit trail of every request |
| `air_gapped` | Only the Marionette control plane | Untrusted prompts, sensitive source |

Everything except `none` also blocks a fixed set of ranges that no policy can
open. See [Always blocked](#always-blocked).

## Docker: how a policy is enforced

The enforcement point is `iptables` in the container's own network namespace,
driven from the host with `nsenter`. The rules are not visible to, or editable
from, the sandbox.

### Rules exist before any interface does

A restricted runner is created with Docker's `none` network. Docker gives it a
namespace containing a loopback interface and nothing else: no veth, no route
off-box, no address. The spawn sequence is:

```
ContainerCreate(NetworkMode: "none")   namespace will hold only lo
ContainerStart                          entrypoint runs; it has nowhere to send
iptables ...                            rules written into that namespace
NetworkDisconnect(none)                 drop the placeholder
NetworkConnect(<network>)               an interface, an address and a route appear
```

The security property is structural rather than a race won by being fast:
before the final step there is no path a packet could take, whatever the
process inside does. Docker refuses to attach a network to a container still in
`none` mode, which is why the placeholder is dropped first; neither of those
two calls creates an interface on its own.

This replaced applying the policy after the container was already on the
network, which left a window of unrestricted egress between start and
enforcement — hundreds of milliseconds, and ample for a process whose first act
is to phone home.

If any rule fails to install, the container is destroyed rather than connected.

Restricted containers are also created with restart policy `no`. A restart
would re-create the namespace with an interface already attached, and the rules
would only come back at the next refresh tick.

### Chain layout

Two chains per runner. `MARIONETTE_<runner-id>` holds everything static;
`MARIONETTE_<runner-id>_D` holds the allow-list addresses that the DNS
refresher adds and removes. `OUTPUT` jumps to the first.

```
OUTPUT -j MARIONETTE_run_abc123

MARIONETTE_run_abc123
  -o lo                                          ACCEPT   loopback, incl. Docker's resolver stub
  -m conntrack --ctstate ESTABLISHED,RELATED     ACCEPT   replies to our own connections
  -d <control plane> -p tcp --dport <port>       ACCEPT   operator pins, ahead of the blocks
  -d <proxy> -p tcp --dport <port>               ACCEPT       (proxy level only)
  -d <resolver> -p udp/tcp --dport 53            ACCEPT       (if resolvers are pinned)
  -d 169.254.169.254/32                          DROP     always blocked, not configurable
  -d 169.254.0.0/16                              DROP
  -d 127.0.0.0/8                                 DROP
  -d 10.0.0.0/8                                  DROP
  -d 172.16.0.0/12                               DROP
  -d 192.168.0.0/16                              DROP
  -p udp/tcp --dport 53                          ACCEPT   only if no resolver was pinned
  -j MARIONETTE_run_abc123_D                              the refreshable allow list
                                                 DROP     default
```

Two details carry weight:

**The operator's pins come before the blocks.** A Marionette server and a DNS
resolver normally live on a private network, and the private ranges are
blocked. Ordering the pins after the blocks would cut every restricted runner
off from the server it is supposed to report to.

**The allow list is a separate chain.** Appending to it can never land a rule
ahead of the metadata-endpoint block, because the blocks are in the parent
chain and are evaluated before the jump. That is what makes it safe for the
refresher to modify rules on a live sandbox.

The IPv6 chain has the same shape, built from the IPv6 blocked ranges. A
container with IPv6 connectivity and an IPv4-only chain would have no policy
at all.

Only TCP is permitted to allow-list destinations. UDP egress is dropped, so
QUIC and HTTP/3 fall back to TCP. ICMP is dropped: `ping` does not work in a
restricted sandbox.

### Always blocked

These are dropped at every restricted level and cannot be opened by any
configuration:

| Range | Why |
|-------|-----|
| `169.254.169.254/32` | AWS, GCP and Azure instance metadata: a credential vending machine |
| `169.254.0.0/16` | Link-local |
| `127.0.0.0/8`, `::1/128` | Loopback by address (SSRF against host-bound services) |
| `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | Private networks: lateral movement |
| `fe80::/10`, `fc00::/7`, `fd00::/8` | IPv6 link-local and unique-local |

An allow-list entry that resolves into one of these is dropped from the rules.
A CIDR entry that overlaps one is dropped whole and reported, rather than
trimmed: `allow 169.254.0.0/16` must not quietly become a route to the metadata
endpoint, and silently narrowing an operator's block would hide that their
policy said something they did not mean.

The only exception is an operator pin — the control plane, the proxy, a
resolver. Those are explicit addresses supplied by the operator, not names the
sandbox can influence.

## DNS: resolve, pin, refresh

Rules are written against addresses, never hostnames. A hostname in an allow
list is resolved on the server at spawn time and the result is pinned into the
firewall.

This is what makes DNS rebinding ineffective. There is no name lookup at
connection time for an attacker to race: by the time the sandbox connects, the
kernel is matching on a destination address that was fixed before the container
had a network.

Pinning alone goes stale — a CDN rotates a record and the pin becomes both a
false deny (the new address is closed) and a false allow (the old address stays
open, possibly to someone else). So each restricted runner gets a refresher:

- Re-resolves on a jittered cadence, bypassing the resolver cache.
- Diffs the new address set against the installed one and applies the
  difference in place. **Add before remove**, so a rotating record is reachable
  through both addresses for one cycle rather than through neither.
- Carries forward the previous addresses for any host that fails to resolve. A
  transient SERVFAIL must not revoke a live allow-list entry and break a
  running task; an entry is only closed when DNS positively says it moved.
- Checks that its rules are still installed before diffing. A container that
  restarted inside a session comes back with a fresh namespace holding none of
  them, and diffing against its own memory would conclude "no change" and leave
  the sandbox wide open. It reinstalls instead.
- Stops when the runner is destroyed or the provider closes.

### Cadence

Go's resolver discards record TTLs (`net.Resolver.LookupIP` throws them away),
so Marionette cannot follow a record's own TTL. The pin lifetime is the policy
TTL, clamped to `[30s, 15m]`, defaulting to 4 minutes, with ±20% jitter so a
hundred sandboxes do not re-resolve in the same millisecond.

The 30-second floor is also the short-TTL defence: a hostile record with a
one-second TTL cannot turn a runner into a DNS hammer. The 15-minute ceiling
bounds how long a stale pin survives even if a policy asks for a longer TTL.

### Wildcards are not enforced

`*.github.com` has no fixed address set to pin, so a packet filter can enforce
nothing for it. Marionette does not silently pretend otherwise: a wildcard
pattern produces no rules — which fails closed — and logs a warning naming the
pattern. Use `proxy` level if you need hostname matching; a proxy sees the
CONNECT host.

### Residual risk

- **DNS is an exfiltration channel.** In `allow_list` and `proxy` mode the
  sandbox can send DNS queries, and a query is a message. Pinning resolvers
  (`isolation.dns_servers`) restricts who can receive them; it does not stop
  data going out inside a query name. Closing the channel entirely means
  `air_gapped`.
- **Time-of-check to time-of-use, narrowly.** A record that changes between
  the server resolving it and the sandbox connecting leaves the sandbox
  reaching the old address. That address was legitimately the host's a moment
  earlier; the exposure is one refresh cycle wide.
- **Shared addresses.** Pinning an address that a CDN also serves other tenants
  from opens that address, not that hostname. A packet filter cannot see SNI.
- **No refresh after a server restart.** Refreshers live in the server process.
  A restart leaves existing runners with their last-pinned rules and no refresh
  until they are recreated. The rules stay enforced; they just stop following
  DNS.

## What each level promises

### `none`

No isolation whatsoever. No rules are installed, the container is attached at
creation exactly as before, and none of the machinery above runs. Every code
path in this document is gated on the level, so `none` behaves exactly as it
did before any of it existed.

### `allow_list`

**Promises:** TCP egress only to the resolved addresses of the named hosts and
networks, on the policy's ports (80 and 443 by default); the control plane; and
DNS. Everything else is dropped, including the metadata endpoint and the
private ranges.

**Cannot:** enforce wildcards. Stop exfiltration through DNS, or through an
allowed host that accepts uploads — an allow list containing a paste service is
an allow list containing an exfiltration route. Distinguish two sites sharing
an address.

### `proxy`

All HTTP(S) egress is forced through an operator-configured proxy. Marionette
uses an **explicit** proxy — environment injection plus a default-drop firewall
— not a transparent `iptables REDIRECT`. The reasoning:

- Every tool in a runner image (curl, git, npm, pip, node, the Claude CLI)
  already honours `HTTP_PROXY`/`HTTPS_PROXY`. Nothing has to be taught a new
  protocol.
- A REDIRECT-based intercept must terminate TLS to learn the destination host,
  which means MITM with a trusted CA for *all* traffic. Explicit proxying keeps
  CONNECT tunnels intact, so certificate pinning still works unless the
  operator deliberately deploys a MITM bundle.
- The firewall is what actually enforces the policy: only the proxy endpoint
  and the control plane are reachable. A tool that ignores the environment
  fails closed instead of escaping — which is the exact failure mode a REDIRECT
  is usually reached for.

Injected into the container:

```
HTTP_PROXY, HTTPS_PROXY, http_proxy, https_proxy   the proxy URL
NO_PROXY, no_proxy                                 loopback + the control-plane host
SSL_CERT_FILE, REQUESTS_CA_BUNDLE,                 the CA bundle, if configured
NODE_EXTRA_CA_CERTS, GIT_SSL_CAINFO
```

The control-plane host is excluded from proxying so the agent's gRPC connection
is never tunnelled. Session environment variables are appended after these, so
an operator override still wins.

**Promises:** no direct egress. Every HTTP(S) request is visible to the proxy.

**Cannot:** carry non-HTTP protocols unless the proxy supports CONNECT for
their ports and the client is configured to use it — raw TCP, ssh and `git://`
are simply dropped. Marionette does not ship a proxy; `isolation.proxy_url`
must point at one you run. If the proxy terminates TLS, the runner image must
already contain the CA bundle: Marionette references the path, it does not
create the file.

### `air_gapped`

**Promises:** no egress at all except TCP to the Marionette server on its
control-plane port. No DNS, no allow list, no metadata, no neighbours. Verified
against a live daemon: a container on the same subnet as the runner is
unreachable, while the pinned control plane is reachable despite sitting inside
a blocked private range.

Because there is no DNS, the server's address is pinned into the container's
`/etc/hosts` at spawn time (`ExtraHosts`; `hostAliases` on Kubernetes). Without
that, an agent configured with a hostname could not resolve it and the runner
would never connect.

**Cannot:**

- **Filter what rides the control plane.** The runner↔server gRPC stream is
  open by construction; that is how the agent reports and how tunnels are
  multiplexed. An agent that wants to move data out can put it in a task log.
  Air-gapped means "no independent network path", not "no data leaves".
- **Stop host-level side channels.** Anything a container shares with the host
  or with other containers is outside a network policy's reach: a bind-mounted
  workspace, `/dev/shm`, a shared volume, CPU and timing channels, and the
  Docker socket if it is ever mounted (never mount it into a runner). Network
  isolation is not a hypervisor.
- **Filter inbound traffic.** The rules constrain `OUTPUT` only. Marionette
  publishes no ports for runner containers and tunnels ride the outbound
  control connection, so nothing is reachable from outside by default — but a
  process that binds a port is reachable from anything already on the same
  Docker network. If that matters, put restricted runners on their own network.
- **Survive a compromised host.** Rules are written from the host with
  CAP_NET_ADMIN. Root on the host, or a container with CAP_NET_ADMIN in its own
  namespace, can remove them. Runner containers must not be privileged.

## Kubernetes

The Kubernetes provider expresses the same policy as a per-runner
`NetworkPolicy`. It has real parity for the parts NetworkPolicy can express,
and says so where it cannot.

**The policy is created before the pod.** A NetworkPolicy selects pods by
label, so it is already in the API server when the pod carrying that label
appears, and the CNI has it to program from the start. This replaced creating
it after the pod became ready, which left the pod unfiltered for up to two
minutes — long enough for a short task to run start to finish having never been
isolated. A failed pod creation removes the policy again.

| Level | Egress rules |
|-------|--------------|
| `allow_list` | Control plane + resolved `/32`/`/128` ipBlocks + CIDR entries, on the policy's ports; plus DNS to the cluster DNS namespace |
| `proxy` | Control plane + the proxy endpoint; plus DNS. Proxy environment injected into the pod |
| `air_gapped` | Control plane only. No DNS. Server address pinned via `hostAliases` |
| `none` | No NetworkPolicy is created |

An empty, non-nil egress list is the deny-all case; a nil one would read as "no
egress rules declared", which Kubernetes treats as no restriction.

**What NetworkPolicy cannot do:**

- **No hostnames.** Wildcards cannot be expressed at all, and concrete names
  work only because Marionette resolves them first. Real hostname filtering
  needs a DNS-aware CNI (Cilium's `toFQDNs`, for example).
- **Enforcement belongs to the CNI.** On a cluster whose CNI ignores
  NetworkPolicy — flannel without a policy add-on, for instance — every policy
  here is inert and the pod has full egress. Marionette cannot detect this. The
  operator must confirm their CNI enforces NetworkPolicy.
- **A residual programming window.** There is a gap between a pod's sandbox
  being created and the CNI programming its policy. Creating the policy first
  shrinks it to the CNI's own latency; it does not eliminate it. The Docker
  provider's guarantee is stronger because it controls when the interface
  appears.
- **No in-place refresh.** ipBlocks are pinned when the pod is created and are
  not re-resolved for its lifetime. A long-lived pod behind a rotating CDN
  record will drift. The Docker provider's refresher has no NetworkPolicy
  equivalent here.

## Configuration

### Session

```yaml
network:
  level: allow_list              # none | allow_list | proxy | air_gapped
  allowed_hosts:
    - api.anthropic.com
    - github.com
    - registry.npmjs.org
    - 203.0.113.0/24             # CIDR entries are allowed
```

An entry is a hostname, an IP literal, or a CIDR block. A wildcard is accepted
but not enforced by the packet filter (see above). A trailing port
(`api.example.com:443`) is rejected — it used to be accepted and silently
discarded, which widened the rule to every allowed port.

### Operator

A session picks a level; it never picks its own proxy, resolvers or
control-plane address, or a compromised session could widen its own policy.
Those are operator settings:

```yaml
providers:
  docker:
    isolation:
      # Kept reachable at every level. Falls back to this when the spawn
      # options carry no server address.
      server_url: "marionette.internal:9090"

      # Required for proxy level.
      proxy_url: "http://proxy.internal:3128"
      proxy_no_proxy: ["registry.internal"]
      proxy_ca_cert: "/etc/marionette/proxy-ca.crt"   # path inside the runner

      # Restrict who the sandbox may send DNS to. Empty means "anywhere",
      # because nothing in a sandbox works without name resolution.
      dns_servers: ["10.0.0.53"]

      # Re-resolve cadence. Clamped to [30s, 15m].
      refresh_interval: "2m"

      # Where the host's procfs is mounted. The default is right for a server
      # running on the Docker host; a server running inside a container must
      # share the host PID namespace or mount /proc and point this at it.
      proc_root: "/proc"

  kubernetes:
    isolation:
      server_url: "marionette.internal:9090"
      dns_namespace: "kube-system"
```

The same block is accepted in a provider's config JSON when registering one
through the admin API.

### Requirements

The server writes rules into container namespaces, which needs:

- `nsenter`, `iptables` and `ip6tables` on the server host.
- CAP_NET_ADMIN in the target namespace — in practice, the server runs as root
  on the Docker host.
- Visibility of container PIDs. A containerised server needs `--pid=host`, or
  the host's `/proc` mounted and `isolation.proc_root` pointed at it.

Without these, a restricted spawn fails. It does not fall back to running the
container unprotected.

## Tests

| What | Where | How to run |
|------|-------|------------|
| Rule generation, ordering, refresh diffing | `pkg/network/...` | `go test -race ./pkg/network/...` |
| Spawn call ordering, failure paths | `pkg/provider/docker` | `go test -race ./pkg/provider/docker/...` |
| Live enforcement against a real daemon | `pkg/provider/docker/isolation_integration_test.go` | see below |
| NetworkPolicy shape and ordering | `pkg/provider/kubernetes` | `go test ./pkg/provider/kubernetes/...` |

The live tests need Linux, root, and visibility of the runner containers' PIDs.
From a macOS or Windows host, the repo's Linux test image supplies all three:

```bash
docker build -t marionette/test:latest -f deploy/docker/test.Dockerfile .
docker run --rm --privileged --pid=host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e DOCKER_HOST=unix:///var/run/docker.sock \
  marionette/test:latest \
  go test -tags=integration -v -run TestIsolation ./pkg/provider/docker/
```

`--pid=host` is the load-bearing flag: without it the container PIDs the daemon
reports do not resolve in the test process's namespace, and the tests skip. A
test that cannot observe the kernel skips rather than passes; a false green
would be worse than no test.

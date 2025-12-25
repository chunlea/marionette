# Network Isolation

## Network isolation

Marionette provides multiple levels of network isolation to control agent internet access.

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Network Isolation Levels                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Level 0: none (full internet access)                               │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──────────────────────────────► Internet             │    │
│  │  SECURITY: No isolation, agent can exfiltrate data          │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  Level 1: allow_list (specific domains only)                        │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──► Egress Filter ──► github.com ✓                   │    │
│  │                          ──► pypi.org ✓                     │    │
│  │                          ──► evil.com ✗                     │    │
│  │  SECURITY: DNS rebinding possible, verify at packet level   │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  Level 2: proxy (full inspection + logging)                         │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──► Egress Proxy ──► Allow List ──► Internet         │    │
│  │           (TLS termination) (logged)                        │    │
│  │  SECURITY: Requires CA trust, breaks certificate pinning    │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  Level 3: air_gapped (no internet, strict control plane only)       │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──► Marionette Server only                           │    │
│  │  SECURITY: No tunnels allowed, inbound only for streaming   │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Security considerations

### DNS rebinding prevention

The `allow_list` level blocks connections at the DNS/IP layer, but DNS rebinding attacks
could potentially bypass hostname-based filtering:

1. Attacker controls `evil.com` → initially resolves to allowed IP
2. Agent connects (allowed)
3. DNS TTL expires, `evil.com` → attacker's IP
4. Subsequent connections bypass allow list

**Mitigations** (implemented by default):

```go
// pkg/network/dns_pinning.go

type DNSPinner struct {
    cache      map[string][]net.IP  // domain -> resolved IPs
    allowedIPs map[string]bool      // pre-resolved allowed IP ranges
    resolver   *net.Resolver
    ttl        time.Duration        // default: 5 min
}

// ResolveAndPin resolves at task start and pins for duration
func (p *DNSPinner) ResolveAndPin(ctx context.Context, hosts []string) error {
    for _, host := range hosts {
        ips, err := p.resolver.LookupIP(ctx, "ip", host)
        if err != nil {
            return fmt.Errorf("failed to resolve %s: %w", host, err)
        }
        p.cache[host] = ips
        for _, ip := range ips {
            p.allowedIPs[ip.String()] = true
        }
    }
    return nil
}

// ValidateConnection checks if destination IP is in pinned set
func (p *DNSPinner) ValidateConnection(destIP net.IP) bool {
    return p.allowedIPs[destIP.String()]
}
```

**Firewall enforcement** (IP-level, not DNS-level):

```bash
# allow_list resolves to IPs at task start, firewall uses IPs only
# This prevents DNS rebinding since we match on destination IP, not hostname

# Example iptables rules (generated at task start)
iptables -A OUTPUT -m owner --uid-owner $AGENT_UID -j MARIONETTE_EGRESS
iptables -A MARIONETTE_EGRESS -d 140.82.112.0/20 -p tcp --dport 443 -j ACCEPT  # github
iptables -A MARIONETTE_EGRESS -d 151.101.0.0/16 -p tcp --dport 443 -j ACCEPT   # pypi
iptables -A MARIONETTE_EGRESS -j DROP
```

### Proxy mode security

The `proxy` level performs TLS termination (MITM) to inspect HTTPS traffic:

| Aspect | Consideration |
|--------|---------------|
| **CA Trust** | Agent must trust proxy's CA certificate |
| **Certificate Pinning** | Applications with pinned certs will fail |
| **Privacy** | All traffic visible to proxy operator |
| **Compliance** | May violate terms of service for some APIs |

**When to use proxy mode**:
- Full audit trail required for compliance
- Content inspection needed (block malware downloads)
- Enterprise environments with existing MITM proxies

**When NOT to use proxy mode**:
- Using APIs with certificate pinning (some SDKs)
- Privacy-sensitive workloads
- Untrusted proxy operators

```yaml
# Proxy configuration
network:
  level: proxy
  proxy:
    # SECURITY: Operator-managed proxy, not user-configurable
    endpoint: "https://proxy.internal:8443"
    ca_cert: "/etc/marionette/proxy-ca.crt"  # Pre-installed

    # Log all requests for audit
    log_requests: true
    log_retention: 30d

    # Content filtering
    block_patterns:
      - "*.exe"
      - "*.msi"
      - "*malware*"
    max_request_size: 100MB
```

### Air-gapped mode semantics

The `air_gapped` level provides strict network isolation:

```
┌─────────────────────────────────────────────────────────────────────┐
│                    air_gapped Network Model                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ALLOWED:                                                           │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──► Marionette Server (gRPC control plane)           │    │
│  │  Agent ◄── Server (inbound desktop/browser streaming)       │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  BLOCKED:                                                           │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Agent ──► Internet (any destination)                    ✗  │    │
│  │  Agent ──► User-requested tunnels (data exfiltration)    ✗  │    │
│  │  Agent ──► Localhost services (SSRF)                     ✗  │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                     │
│  TUNNELS IN AIR-GAPPED MODE:                                        │
│  ─────────────────────────────────────────────────────────────────  │
│  Tunnels are INBOUND-ONLY for UI streaming purposes:                │
│  - Desktop streaming (WebRTC): Server → Agent display               │
│  - Browser streaming (CDP): Server → Agent headless browser         │
│  - iOS Simulator: Server → Agent simulator screen                   │
│                                                                     │
│  Outbound tunnels (agent → external) are ALWAYS BLOCKED:            │
│  - HTTP tunnels to external services ✗                              │
│  - TCP tunnels to arbitrary hosts ✗                                 │
│  - Any agent-initiated outbound connection ✗                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Tunnel policy per network level**:

| Level | Inbound Tunnels | Outbound Tunnels | Notes |
|-------|-----------------|------------------|-------|
| `none` | Allowed | Allowed | No restrictions |
| `allow_list` | Allowed | Allowed (filtered) | Subject to allow list |
| `proxy` | Allowed | Allowed (proxied) | All traffic logged |
| `air_gapped` | **Inbound only** | **Blocked** | Streaming only, no exfil |

```go
// pkg/tunnel/policy.go

type TunnelPolicy struct {
    NetworkLevel string
}

// CanCreateTunnel checks if tunnel creation is allowed
func (p *TunnelPolicy) CanCreateTunnel(req TunnelRequest) error {
    if p.NetworkLevel == "air_gapped" {
        // Only allow inbound streaming tunnels
        switch req.Type {
        case "desktop", "ios", "android", "browser":
            // These are inbound (server -> agent display)
            if req.Direction == "inbound" {
                return nil
            }
            return ErrTunnelBlocked("air_gapped mode: outbound tunnels blocked")
        case "http", "tcp":
            // These are typically outbound (agent -> external)
            return ErrTunnelBlocked("air_gapped mode: http/tcp tunnels blocked")
        }
    }
    return nil
}

// Tunnel direction
type TunnelDirection string
const (
    TunnelInbound  TunnelDirection = "inbound"   // Server/user -> agent (e.g., view desktop)
    TunnelOutbound TunnelDirection = "outbound"  // Agent -> external (e.g., expose port)
)
```

## Configuration

### Session-level network policy

```yaml
# Session-level network policy (inherited by tasks)
network:
  level: allow_list              # none | allow_list | proxy | air_gapped

  # For allow_list level
  allowed_hosts:
    - "*.github.com"
    - "api.anthropic.com"
    - "api.openai.com"
    - "pypi.org"
    - "registry.npmjs.org"
    - "rubygems.org"

  # For proxy level (additional options)
  proxy:
    log_requests: true           # Log all HTTP requests
    block_patterns:              # Block specific patterns
      - "*.exe"
      - "*.msi"
      - "*malware*"
    max_request_size: 100MB

  # Always implicitly allowed (cannot be blocked):
  # - Marionette server (gRPC control plane)
  # - DNS resolution (UDP 53) - for initial resolution only
```

### Metadata endpoint protection (ALWAYS BLOCKED)

**SECURITY: Cloud metadata endpoints are ALWAYS blocked. This is NOT configurable.**

These endpoints are blocked at the firewall level regardless of network policy to prevent credential theft:

```go
// pkg/network/metadata.go

var blockedCIDRs = []string{
    "169.254.169.254/32",      // AWS, Azure, GCP metadata
    "169.254.0.0/16",          // Link-local
    "127.0.0.0/8",             // Localhost (SSRF)
    "10.0.0.0/8",              // Private networks (configurable)
    "172.16.0.0/12",           // Private networks (configurable)
    "192.168.0.0/16",          // Private networks (configurable)
    "fd00::/8",                // IPv6 ULA
}

// CanConnect checks if connection is allowed
func (f *Firewall) CanConnect(destIP net.IP, destPort int) bool {
    for _, cidr := range f.blockedCIDRs {
        if cidr.Contains(destIP) {
            return false
        }
    }
    // ... check allow list
}
```

### Implementation per provider

| Provider | Implementation | Notes |
|----------|----------------|-------|
| Docker | iptables in container, custom network | Uses `--network` and custom rules |
| Kubernetes | NetworkPolicy + egress gateway | Native K8s NetworkPolicy |
| E2B | Built-in (isolated VPC) | Managed by E2B |
| macOS (pool) | pf firewall rules | Dynamic anchor rules |
| Linux (pool) | iptables + nftables | systemd service |

### macOS pf firewall example

```bash
# /etc/pf.anchors/marionette-agent-{agent_id}
# Generated dynamically by marionette-agent

# Block all outbound by default
block out on en0 all

# Allow DNS
pass out on en0 proto udp to any port 53

# Allow Marionette server
pass out on en0 proto tcp to marionette.example.com port {9090, 443}

# Allow listed hosts (IPs resolved at task start)
pass out on en0 proto tcp to 140.82.114.0/24 port {80, 443}  # github.com
pass out on en0 proto tcp to 151.101.0.0/16 port {80, 443}   # pypi.org
```

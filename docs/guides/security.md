# Security

Security best practices for deploying and operating Marionette.

## Authentication

### API Keys

API keys authenticate CLI and external applications:

```bash
# Create an API key with scopes
mctl admin keys create \
  --name "ci-key" \
  --scopes "sessions:*,tasks:*"
```

**Key Format:** `mk_{base64url_random_bytes}` (~48 characters)

**Storage:** SHA-256 hashed in database (original never stored)

### Runner Tokens

Runner tokens authenticate pool runners:

```bash
mctl admin runner-tokens create --pool macos
```

**Key Format:** `rtok_{base64url_random_bytes}`

### Tunnel Tokens

Tunnel tokens provide access to forwarded ports:

```bash
# Returned when creating tunnel
mctl tunnels create --session $SID --type http --port 3000
# Returns: ttok_xxx (short-lived)
```

## Transport Security

### mTLS for Agents

Production deployments should use mTLS:

```yaml
server:
  grpc:
    tls:
      enabled: true
      cert_file: "/etc/marionette/tls/server-cert.pem"
      key_file: "/etc/marionette/tls/server-key.pem"
      ca_file: "/etc/marionette/tls/ca-cert.pem"
      client_auth: require
```

Agent configuration:

```bash
./bin/agent \
  --server marionette.example.com:9090 \
  --tls-cert /etc/marionette/tls/agent-cert.pem \
  --tls-key /etc/marionette/tls/agent-key.pem \
  --tls-ca /etc/marionette/tls/ca-cert.pem
```

### TLS for API

```yaml
server:
  tls:
    enabled: true
    cert_file: "/etc/marionette/tls/cert.pem"
    key_file: "/etc/marionette/tls/key.pem"
```

## Credential Encryption

### Encryption at Rest

Agent API keys are encrypted using AES-256-GCM:

```bash
# Set encryption key (32 bytes, hex encoded)
export MARIONETTE_ENCRYPTION_KEY=$(openssl rand -hex 32)
```

### Managed vs BYOK

| Mode | Storage | Use Case |
|------|---------|----------|
| **Managed** | Encrypted in DB | Operator provides keys |
| **BYOK** | Memory only | Users bring own keys |

BYOK mode ensures keys are never written to disk:

```bash
mctl sessions create --agent claude --api-key $KEY --byok
```

## Network Isolation

### Session Network Policies

| Policy | Description |
|--------|-------------|
| `none` | No network restrictions |
| `allow_list` | Only allowed hosts (default) |
| `proxy` | All traffic through proxy |
| `air_gapped` | No external network |

```bash
mctl sessions create \
  --agent claude \
  --network-policy allow_list \
  --allowed-hosts "api.anthropic.com,github.com"
```

### Kubernetes NetworkPolicy

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: marionette-agent
spec:
  podSelector:
    matchLabels:
      app: marionette-agent
  policyTypes:
    - Egress
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 169.254.169.254/32  # Block metadata service
      ports:
        - port: 443
          protocol: TCP
```

## Permission gating

Marionette gates tool calls **before** they execute. When the agent wants to use
a tool, Claude Code runs a `PreToolUse` hook and waits for it; that hook is the
agent binary re-invoked as `marionette-agent permission-hook`, talking over a
unix socket to a broker inside the running agent, which raises a permission
request on the server. The tool does not run until someone answers.

### Policy

Deny-by-default on the unknown:

| Tools | Gated? |
|-------|--------|
| Known read-only built-ins (`Read`, `Grep`, `Glob`, …) | No |
| Mutating built-ins (`Bash`, `Write`, `Edit`, `NotebookEdit`, …) | Yes |
| Tools with outbound effects (`WebFetch`, `SendMessage`, `CronCreate`, …) | Yes |
| Every `mcp__*` tool | Yes |
| Any tool name the policy does not recognise | Yes |

Gating unknown names is deliberate: a CLI upgrade that introduces a tool must
not silently open a hole.

### Failure behaviour

Two properties matter for anyone reasoning about this as a control:

- **The CLI fails open on hook timeout.** If a hook overruns its budget, Claude
  Code runs the tool anyway. Every failure path inside the gate therefore
  *denies*, and the hook's own deadline is set shorter than the CLI's so its
  answer always wins the race.
- **The gate cannot fail quietly.** If the broker will not start, the task fails
  rather than running unsupervised.

An unanswered request suspends the session after `suspend_after_seconds`
(30 minutes by default). It stays pending across the suspend; answering it and
resuming re-runs the task.

## Multi-Tenant Isolation

!!! note "Behind a flag"
    Tenant enforcement is gated on `multi_tenant` in the server config, which
    defaults to false. With it off the `tenant_id` columns are present and
    populated but are not treated as a boundary — a single-tenant deployment
    stores everything under the empty tenant. Turn it on for anything serving
    more than one customer.

### Tenant ID Enforcement

All resources carry a `tenant_id`:

- Injected by auth middleware (never from user input)
- All queries filtered by tenant
- Cross-tenant access prevented at application layer

```sql
-- All queries include tenant filter
SELECT * FROM sessions WHERE tenant_id = $1 AND id = $2;
```

### Labels are NOT Security Boundaries

Labels are for organization only. Don't rely on labels for access control.

## Audit Logging

All sensitive actions are logged:

```bash
# View audit logs
mctl admin audit-logs list --action "permission.*"
```

Logged actions include:

- Permission approvals/denials
- Session lifecycle changes
- API key creation/revocation
- Configuration changes

## Best Practices

### API Key Management

- Use scoped keys (minimum necessary permissions)
- Rotate keys regularly
- Revoke unused keys promptly
- Never commit keys to source control

### Runner Security

- Use mTLS in production
- Run agents as non-root users
- Use read-only filesystems where possible
- Enable sandbox mode for multi-tenant

### Network Security

- Use `allow_list` network policy
- Block cloud metadata endpoints
- Use private networks for internal communication
- Enable TLS for all external endpoints

### Operational Security

- Enable audit logging
- Monitor for anomalies
- Keep software updated
- Backup encryption keys securely

## Compliance Considerations

### Data Residency

Configure storage backends per region:

```yaml
storage:
  provider: s3
  s3:
    bucket: "marionette-eu-west-1"
    region: "eu-west-1"
```

### Data Retention

Configure workspace retention policies:

```yaml
storage:
  workspace:
    retention:
      default: 30d
      max: 90d
```

### Access Controls

- Use RBAC with scoped API keys
- Implement IP allowlists for admin access
- Enable MFA for admin operations (via external IdP)

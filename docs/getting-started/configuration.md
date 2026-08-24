# Configuration

Marionette uses a combination of configuration files and environment variables.

!!! note "Security"
    Sensitive values (API keys, encryption keys) should always be set via environment variables, never in config files.

## Configuration File

The configuration file contains non-sensitive settings:

```yaml
# configs/local.yaml
server:
  api:
    port: 8080
    host: "0.0.0.0"
  admin:
    port: 8081
    host: "127.0.0.1"  # Admin should be internal only
  grpc:
    port: 9090
    host: "0.0.0.0"

# Provider configuration
providers:
  default: docker
  docker:
    host: "unix:///var/run/docker.sock"
    image: "marionette/agent:latest"
    network: "marionette-network"
    resources:
      memory: "2g"
      cpus: "2"

# Storage configuration
storage:
  provider: local
  local:
    path: "./data/storage"
  workspace:
    base_dir: "./data/workspaces"

# Logging configuration
logging:
  level: debug  # debug, info, warn, error
  format: console  # console or json

# Observability configuration
observability:
  metrics:
    enabled: true
    port: 9091
    path: /metrics
    namespace: marionette
  health:
    enabled: true
  tracing:
    enabled: false
    exporter: otlp  # otlp, stdout, or noop
    endpoint: localhost:4317
    service_name: marionette-server
    sample_rate: 0.1

# Development settings
dev:
  hot_reload: true
  skip_tls: true
```

## Environment Variables

All environment variables are prefixed with `MARIONETTE_`:

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `MARIONETTE_DATABASE_URL` | PostgreSQL connection string | `postgres://user:pass@localhost/marionette` |
| `MARIONETTE_MASTER_KEY` | Master key for admin operations | Random 32+ character string |
| `MARIONETTE_ENCRYPTION_KEY` | Key for encrypting credentials | Random 32-byte hex string |
| `MARIONETTE_UI_USERNAME` | Admin API / WebUI basic auth username | `admin` |
| `MARIONETTE_UI_PASSWORD` | Admin API / WebUI basic auth password | - |

!!! danger "The admin API fails closed"
    The admin API mints API keys, registers runners and can read every session.
    The server **refuses to start** without `MARIONETTE_UI_USERNAME` and
    `MARIONETTE_UI_PASSWORD`. Starting it with `--dev-insecure-admin` serves that
    API with no authentication at all; use it for local development only.

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MARIONETTE_CONFIG` | Config file path | `config.yaml` |
| `MARIONETTE_LOG_LEVEL` | Log level | `info` |

### Agent Variables

| Variable | Description |
|----------|-------------|
| `MARIONETTE_SERVER` | Server gRPC URL |
| `MARIONETTE_RUNNER_TOKEN` | Token for authentication |
| `MARIONETTE_SANDBOX_MODE` | `runner-is-sandbox`, `runner-creates-sandbox`, or `none` |
| `MARIONETTE_POOL_NAME` | Pool name (for pool runners) |

## Subsystem flags

Three settings decide whether whole subsystems are live. All are read from the
server config file.

```yaml
tunnels:
  enabled: true       # HTTP proxy and TCP relay

streaming:
  enabled: false      # desktop and browser streaming

multi_tenant: false   # enforce tenant isolation rather than merely recording it
```

| Flag | Default | What it does |
|------|---------|--------------|
| `tunnels.enabled` | `true` in `configs/local.yaml` | Serves the HTTP proxy and TCP relay used by `mctl tunnels` |
| `streaming.enabled` | `false` | Desktop and browser streaming. **Frozen** — the SFU has no media source, no renegotiation and never reads RTCP, so it cannot deliver a frame. Leave it off |
| `multi_tenant` | `false` | Turns `tenant_id` from a recorded column into an enforced boundary |

## Workspace sync (runner)

A pooled runner that is released has to put the workspace somewhere. Off by
default:

```bash
./bin/agent ... \
  --storage-backend local \
  --storage-local-path /var/marionette/cas \
  --storage-encryption none
```

| Flag | Default | Notes |
|------|---------|-------|
| `--storage-backend` | `none` | `none` or `local` |
| `--storage-local-path` | - | Required for `local`. A directory the runner can write: a shared volume or a mounted object store |
| `--storage-encryption` | *(none)* | Must be set explicitly to `none` when a backend is configured |

`--storage-encryption` has no default on purpose. Storing workspace contents
unencrypted is a decision an operator makes, not one a missing config value
makes for them. Per-tenant encryption is refused rather than silently
downgraded, because a runner has no way to obtain a tenant data key yet.

With sync off, a suspend reports the workspace as **not** synced rather than
implying a snapshot exists.

## Provider Configuration

### Docker Provider

```yaml
providers:
  docker:
    host: "unix:///var/run/docker.sock"
    image: "marionette/agent:latest"
    network: "marionette-network"
    resources:
      memory: "4g"
      cpus: "4"
    volumes:
      - "/data/workspaces:/workspace"
    isolation:
      # The address a spawned runner dials back on. Required for any
      # containerized runner - see below.
      server_url: "host.docker.internal:9090"
```

`isolation.server_url` is the address a spawned runner dials back on, and it is
the one setting a containerized runner will not come up without. Leave it unset
and the server derives the address from its own gRPC listener: `127.0.0.1:9090`,
which inside a container is the container itself. Runners then start, never
connect, and sit there until the reaper takes them. The server logs a WARN naming
the key whenever it has to guess, so check the startup log if runners are not
showing up. Set it to an address the container can reach:
`host.docker.internal:9090` when the server runs on the Docker Desktop host, the
Compose service name (`server:9090`) when the server is itself a container on the
same network, or the service DNS name in a cluster. The rest of the `isolation`
block — proxies, resolvers, refresh cadence — is documented in
[Network isolation](../network.md).

### Kubernetes Provider

```yaml
providers:
  kubernetes:
    namespace: "marionette"
    image: "marionette/agent:latest"
    service_account: "marionette-agent"
    resources:
      requests:
        memory: "2Gi"
        cpu: "1"
      limits:
        memory: "4Gi"
        cpu: "2"
    node_selector:
      workload: "ai-agents"
```

### Pool Provider

```yaml
providers:
  pool:
    pools:
      macos:
        min_runners: 2
        max_runners: 10
        selector:
          os: darwin
          arch: arm64
      gpu:
        min_runners: 1
        max_runners: 4
        selector:
          gpu: nvidia
```

## Storage Configuration

### Local Storage

```yaml
storage:
  provider: local
  local:
    path: "/var/marionette/storage"
  workspace:
    base_dir: "/var/marionette/workspaces"
```

### S3 Storage

```yaml
storage:
  provider: s3
  s3:
    bucket: "marionette-storage"
    region: "us-west-2"
    # Credentials from AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY
```

!!! note "GCS Storage"
    GCS storage support is planned for future releases.

## Observability Configuration

### Prometheus Metrics

```yaml
observability:
  metrics:
    enabled: true
    port: 9091
    path: /metrics
    namespace: marionette
```

Access metrics at `http://localhost:9091/metrics`.

### OpenTelemetry Tracing

```yaml
observability:
  tracing:
    enabled: true
    exporter: otlp
    endpoint: localhost:4317
    service_name: marionette-server
    sample_rate: 0.1  # 10% of traces
    insecure: false   # Use TLS
```

Supported exporters:

- `otlp` - Export to OTLP-compatible backends (Jaeger, Zipkin, etc.)
- `stdout` - Print traces to console (for debugging)
- `noop` - Disable trace export (still creates spans for overhead testing)

### Health Checks

```yaml
observability:
  health:
    enabled: true
```

Endpoints:

- `GET /health/live` - Liveness probe (server is running)
- `GET /health/ready` - Readiness probe (database connected)

## Security Configuration

### TLS

TLS configuration is at the root level (applies to all endpoints):

```yaml
tls:
  enabled: true
  cert_file: "/path/to/cert.pem"
  key_file: "/path/to/key.pem"
  ca_file: "/path/to/ca-cert.pem"      # For client verification
  verify_client: true                   # Enable mTLS for agents
```

## Example Configurations

### Development

```yaml
# configs/local.yaml
server:
  api:
    port: 8080
  admin:
    port: 8081
  grpc:
    port: 9090

providers:
  default: docker

logging:
  level: debug
  format: console

dev:
  skip_tls: true
```

### Production

```yaml
# configs/production.yaml
server:
  api:
    port: 8080
  admin:
    port: 8081
    host: "127.0.0.1"  # Internal only
  grpc:
    port: 9090

tls:
  enabled: true
  cert_file: "/etc/marionette/tls/cert.pem"
  key_file: "/etc/marionette/tls/key.pem"
  ca_file: "/etc/marionette/tls/ca.pem"
  verify_client: true

providers:
  default: docker

storage:
  provider: s3
  s3:
    bucket: "prod-marionette-storage"
    region: "us-west-2"

logging:
  level: info
  format: json

observability:
  metrics:
    enabled: true
  tracing:
    enabled: true
    exporter: otlp
    endpoint: "tempo.monitoring:4317"
    sample_rate: 0.1
```

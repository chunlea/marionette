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

### Optional Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MARIONETTE_CONFIG` | Config file path | `config.yaml` |
| `MARIONETTE_LOG_LEVEL` | Log level | `info` |
| `MARIONETTE_UI_USERNAME` | WebUI Basic Auth username | - |
| `MARIONETTE_UI_PASSWORD` | WebUI Basic Auth password | - |

### Agent Variables

| Variable | Description |
|----------|-------------|
| `MARIONETTE_SERVER` | Server gRPC URL |
| `MARIONETTE_RUNNER_TOKEN` | Token for authentication |
| `MARIONETTE_SANDBOX_MODE` | `runner-is-sandbox` or `runner-creates-sandbox` |
| `MARIONETTE_POOL_NAME` | Pool name (for pool runners) |

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
```

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

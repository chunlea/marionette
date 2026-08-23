# Installation

This guide covers different ways to install and deploy Marionette.

## Prerequisites

- **Go 1.22+** (for building from source)
- **PostgreSQL 15+** (database)
- **Docker** (recommended for local development)

## Quick Install with Docker Compose

The fastest way to get started:

```bash
# Clone the repository
git clone https://github.com/chunlea/marionette.git
cd marionette

# Start all services. The compose file lives under deploy/docker/.
make docker-up
# equivalently: docker compose -f deploy/docker/docker-compose.yml up -d
```

This starts:

- **Server** on ports 8080 (API), 8081 (Admin), 9090 (gRPC)
- **PostgreSQL** on port 5432
- **Agent** ready to accept tasks

!!! note "The schema comes from migrations"
    The database is provisioned by running `migrations/`, never by mounting a
    `.sql` file. To start from scratch:
    `docker compose -f deploy/docker/docker-compose.yml down -v`.

You still need credentials before anything is usable — see
[Quick start](quick-start.md), step 3.

## Building from Source

### 1. Clone and Install Dependencies

```bash
git clone https://github.com/chunlea/marionette.git
cd marionette

# Install dependencies
make deps
```

### 2. Setup Database

```bash
# Create database
createdb marionette

# Run migrations
make migrate
```

### 3. Build Binaries

```bash
make build
```

This creates three binaries in `./bin/`:

| Binary | Description |
|--------|-------------|
| `server` | Main server (API, Admin, gRPC) |
| `agent` | Runner agent |
| `mctl` | CLI tool |

### 4. Start the Server

```bash
./bin/server --config configs/local.yaml
```

## Kubernetes Deployment

### Using Kustomize

```bash
# Development
kubectl apply -k deploy/kubernetes/overlays/dev

# Production
kubectl apply -k deploy/kubernetes/overlays/prod
```

### Using Helm

```bash
# Add the chart repository (if hosted)
helm repo add marionette https://chunlea.github.io/marionette/charts

# Install
helm install marionette marionette/marionette \
  --set postgresql.enabled=true \
  --set server.replicas=3
```

Or install from local chart:

```bash
helm install marionette ./deploy/helm/marionette \
  --values ./deploy/helm/marionette/values.yaml
```

## Configuration

After installation, configure Marionette using environment variables or a config file:

=== "Environment Variables"

    ```bash
    export MARIONETTE_DATABASE_URL=postgres://localhost/marionette?sslmode=disable
    export MARIONETTE_MASTER_KEY=your-secure-master-key
    export MARIONETTE_ENCRYPTION_KEY=your-32-byte-encryption-key
    ```

=== "Config File"

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
      docker:
        image: "marionette/agent:latest"

    storage:
      provider: local
      local:
        path: "./data/storage"
    ```

See [Configuration](configuration.md) for detailed options.

## Verify Installation

```bash
# Check server health
curl http://localhost:8081/health/ready

# List sessions (should be empty)
./bin/mctl sessions list

# Check metrics
curl http://localhost:9091/metrics | head -20
```

## Next Steps

- [Quick Start](quick-start.md) - Run your first AI coding agent
- [Configuration](configuration.md) - Configure providers and storage
- [CLI Reference](../guides/cli-reference.md) - Learn the CLI commands

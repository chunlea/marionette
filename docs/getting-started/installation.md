# Installation

This guide covers different ways to install and deploy Marionette.

## Prerequisites

- **Go 1.22+** (for building from source)
- **PostgreSQL 15+** (database)
- **Docker** (recommended for local development)

## Published images

Releases publish multi-architecture images (`linux/amd64` and `linux/arm64`) to
GitHub Container Registry:

```bash
docker pull ghcr.io/chunlea/marionette-server:v0.1.0
docker pull ghcr.io/chunlea/marionette-agent:v0.1.0
```

`:latest` follows the newest non-prerelease tag. Pin a version for anything you
depend on.

!!! warning "v0.1.0 predates the release workflow"
    The v0.1.0 images were built by hand on an arm64 machine: they run on
    `linux/arm64` and nothing else, and that release has no `mctl` binaries
    attached. Both are fixed from the next tag on — until then, build from
    source on amd64.

The compose stack can run published images instead of building the checkout —
the override file is the only difference:

```bash
git clone https://github.com/chunlea/marionette.git
cd marionette

MARIONETTE_VERSION=v0.1.0 docker compose \
  -f deploy/docker/docker-compose.yml \
  -f deploy/docker/docker-compose.release.yml up -d
```

The repository is still needed for the compose file, the server config and
`migrations/`. Leave `MARIONETTE_VERSION` unset to track `:latest`.

### mctl

From the next release on, the CLI is attached to each release for macOS (arm64)
and Linux (amd64, arm64):

```bash
VERSION=v0.2.0   # the release you want
curl -fsSLO "https://github.com/chunlea/marionette/releases/download/${VERSION}/mctl_${VERSION}_darwin_arm64.tar.gz"
curl -fsSLO "https://github.com/chunlea/marionette/releases/download/${VERSION}/SHA256SUMS"
shasum -a 256 -c SHA256SUMS --ignore-missing

tar -xzf "mctl_${VERSION}_darwin_arm64.tar.gz"
sudo mv mctl /usr/local/bin/
mctl version
```

## Quick Install with Docker Compose

The fastest way to get started from a checkout. This one **builds** the images
locally; use the override above to run published ones instead.

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

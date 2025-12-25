# Marionette

A remote agent orchestration and observability platform for controlling coding agents (Claude Code, Codex, etc.) running in isolated environments.

## Features

- **Multi-Agent Support** - Run Claude Code, Codex, or custom agents
- **Flexible Deployment** - Docker, Kubernetes, E2B, Firecracker, or bare metal pools
- **Session Management** - Long-lived work contexts with suspend/resume
- **Workspace Persistence** - Encrypted storage with S3/GCS sync
- **Real-time Streaming** - Live logs, permission requests, progress updates
- **Port Forwarding** - HTTP tunnels, desktop streaming (WebRTC), iOS/Android emulator
- **Multi-tenant** - Built for SaaS integration with tenant isolation

## Quick Start

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- Docker (for local development)

### Installation

```bash
# Clone repository
git clone https://github.com/chunlea/marionette.git
cd marionette

# Install dependencies
make deps

# Setup database
createdb marionette
make migrate

# Build binaries
make build
```

### Running Locally

```bash
# Terminal 1: Start server
./bin/server --config configs/local.yaml

# Terminal 2: Start a Docker runner
./bin/agent --server localhost:9090 --token dev-token

# Terminal 3: Use CLI
./bin/mctl sessions create --agent claude --api-key $ANTHROPIC_API_KEY
./bin/mctl tasks create --session $SESSION_ID --prompt "Build a REST API"
./bin/mctl tasks logs --follow $TASK_ID
```

### Docker Compose

```bash
docker compose up -d
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Server (Go)                               │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐    │
│  │ SessionMgr  │ │  TaskMgr    │ │ RunnerMgr   │ │ TunnelMgr   │    │
│  └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘    │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Provider Registry (Docker, K8s, E2B, Pool, ...)            │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                              │                                      │
│  :9090 gRPC  │  :8080 Public API  │  :8081 Admin UI                 │
└──────────────┼────────────────────┼─────────────────────────────────┘
               │                    │
               ▼                    ▼
┌──────────────────────┐    ┌──────────────────────┐
│  Runner (isolated)   │    │  Runner (pool)       │
│  ┌────────────────┐  │    │  ┌────────────────┐  │
│  │marionette-agent│  │    │  │marionette-agent│  │
│  │ ┌────────────┐ │  │    │  └───────┬────────┘  │
│  │ │Claude Code │ │  │    │          │           │
│  │ └────────────┘ │  │    │  ┌───────▼────────┐  │
│  │ ┌────────────┐ │  │    │  │Sandbox (gVisor)│  │
│  │ │ /workspace │ │  │    │  │ Agent+Workspace│  │
│  │ └────────────┘ │  │    │  └────────────────┘  │
│  └────────────────┘  │    └──────────────────────┘
└──────────────────────┘
```

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Runner** | Execution environment (container, VM, or machine) |
| **Session** | Long-lived work context binding Runner + Workspace |
| **Task** | Unit of work (prompt) executed within a Session |
| **Workspace** | Persistent `/workspace` directory |
| **Agent** | AI coding agent (Claude Code, Codex, etc.) |

## API Usage

### Create a Session

```bash
curl -X POST http://localhost:8080/api/v1/sessions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "agent": "claude",
    "api_key": "sk-ant-xxx",
    "labels": {"user": "alice", "project": "my-app"}
  }'
```

### Submit a Task

```bash
curl -X POST http://localhost:8080/api/v1/sessions/$SESSION_ID/tasks \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "Build a REST API with authentication"}'
```

### Stream Logs

```bash
curl -N http://localhost:8080/api/v1/tasks/$TASK_ID/logs?follow=true \
  -H "Authorization: Bearer $API_KEY"
```

## Configuration

```yaml
# config.yaml - non-sensitive settings only
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
    image: "ghcr.io/chunlea/marionette-runner:latest"

storage:
  provider: local
  local:
    path: /var/marionette/storage
```

```bash
# .env - sensitive values (all prefixed with MARIONETTE_)
MARIONETTE_DATABASE_URL=postgres://localhost/marionette?sslmode=disable
MARIONETTE_MASTER_KEY=your-master-key
MARIONETTE_ENCRYPTION_KEY=your-encryption-key
```

## Documentation

| Document | Description |
|----------|-------------|
| [CLAUDE.md](CLAUDE.md) | Complete architecture and design |
| [docs/schema.sql](docs/schema.sql) | Database schema |
| [docs/id.md](docs/id.md) | ID generation (Stripe-style) |
| [docs/runner.proto](docs/runner.proto) | gRPC protocol |
| [docs/storage.md](docs/storage.md) | Storage, CAS, and encryption |
| [docs/agents.md](docs/agents.md) | Agent plugin architecture |
| [docs/pool.md](docs/pool.md) | Pool runners and watchdog |
| [docs/roadmap.md](docs/roadmap.md) | Implementation phases |

## CLI Reference

```bash
# Sessions
mctl sessions list
mctl sessions create --agent claude --api-key $KEY
mctl sessions suspend $SESSION_ID
mctl sessions resume $SESSION_ID --api-key $KEY
mctl sessions terminate $SESSION_ID

# Tasks
mctl tasks create --session $SID --prompt "Fix the bug"
mctl tasks list --session $SID
mctl tasks logs --follow $TASK_ID
mctl tasks cancel $TASK_ID

# Runners
mctl runners list
mctl runners get $RUNNER_ID
mctl runners pause $RUNNER_ID
mctl runners resume $RUNNER_ID

# Tunnels
mctl tunnels create --session $SID --type http --port 3000
mctl tunnels list --session $SID
```

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Generate protobuf
make proto

# Build Docker image
make docker-build

# Run with hot reload
make dev
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MARIONETTE_CONFIG` | Config file path | `config.yaml` |
| `MARIONETTE_DATABASE_URL` | PostgreSQL connection string | - |
| `MARIONETTE_MASTER_KEY` | Master key for admin operations | - |
| `MARIONETTE_ENCRYPTION_KEY` | Key for encrypting agent credentials | - |
| `MARIONETTE_LOG_LEVEL` | Log level | `info` |

## License

[MIT](LICENSE)

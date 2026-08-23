# Marionette

[![CI](https://github.com/chunlea/marionette/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/chunlea/marionette/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/chunlea/marionette?sort=semver)](https://github.com/chunlea/marionette/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/chunlea/marionette)](https://goreportcard.com/report/github.com/chunlea/marionette)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Marionette runs AI coding agents on machines you control, and gives you an API to
drive them: create a session, send it work, watch the logs, approve what it wants
to run, suspend it, pick it up again later.

The agent (today: Claude Code) runs inside a **runner** — a container, a VM, or a
pooled machine. Marionette owns the lifecycle around it: which runner the work
lands on, what the agent is allowed to do, where the workspace lives, and what
happened.

## Status

This is a working system, not a finished product. What the end-to-end acceptance
walk (`scripts/smoke.sh`) exercises on every change:

| Area | State |
|------|-------|
| Sessions, tasks, task runs | Working |
| Claude Code execution, real token accounting | Working |
| Permission gating before a tool runs | Working |
| Log streaming and retrieval | Working |
| Suspend / resume / terminate | Working |
| Pool runners | Working |
| Docker provider | Working |
| Workspace sync to content-addressable storage | Agent side working; server side in progress |
| HTTP / TCP tunnels | Behind `tunnels.enabled`, on by default locally |
| Desktop and browser streaming | **Frozen.** See [Frozen features](#frozen-features) |
| Multi-tenancy | Columns everywhere, enforcement behind `multi_tenant` |

## Install

Every release publishes multi-architecture images (`linux/amd64` and
`linux/arm64`) to GitHub Container Registry, and `mctl` binaries for macOS
(arm64) and Linux (amd64, arm64):

```bash
docker pull ghcr.io/chunlea/marionette-server:v0.1.0
docker pull ghcr.io/chunlea/marionette-agent:v0.1.0
```

The compose stack can run those images instead of building the checkout. You
still need the repository for the compose file, the server config and
`migrations/`:

```bash
MARIONETTE_VERSION=v0.1.0 docker compose \
  -f deploy/docker/docker-compose.yml \
  -f deploy/docker/docker-compose.release.yml up -d
```

`:latest` follows the newest non-prerelease tag; pin a version for anything you
depend on.

> **v0.1.0 predates the release automation.** Its images were built by hand on
> an arm64 machine — they run on `linux/arm64` and nothing else — and no `mctl`
> binaries are attached to it. Both are fixed from the next tag on; on amd64,
> build from source until then.

[Installation](docs/getting-started/installation.md) covers Kubernetes and
Helm. [CHANGELOG.md](CHANGELOG.md) is what changed between versions.

## Quick start

The walk below builds from source, and is the same one `scripts/smoke.sh`
automates. If a command here stops working, that script is the source of truth
— it is run against every change.

### Prerequisites

- Go 1.22+
- Docker (for PostgreSQL and for the Docker provider)
- The `claude` CLI, logged in on this host (`claude auth`). No `ANTHROPIC_API_KEY`
  is needed when the CLI already has a login.
- Ports 8080, 8081, 9090 and a PostgreSQL port free.

### 1. Build

```bash
git clone https://github.com/chunlea/marionette.git
cd marionette
make deps
make build
```

### 2. Database

The schema comes from `migrations/`. `docs/schema.sql` is a generated rendering
of it — never provision from that file.

```bash
docker run -d --name marionette-pg \
  -e POSTGRES_USER=marionette -e POSTGRES_PASSWORD=marionette \
  -e POSTGRES_DB=marionette -p 5432:5432 postgres:16-alpine

export MARIONETTE_DATABASE_URL='postgres://marionette:marionette@localhost:5432/marionette?sslmode=disable'
make migrate
```

### 3. Terminal 1 — server

The admin API mints API keys, registers runners and can read every session, so it
**fails closed**: the server refuses to start without credentials for it.

```bash
export MARIONETTE_DATABASE_URL='postgres://marionette:marionette@localhost:5432/marionette?sslmode=disable'
export MARIONETTE_MASTER_KEY=$(openssl rand -hex 32)
export MARIONETTE_ENCRYPTION_KEY=$(openssl rand -hex 32)
export MARIONETTE_UI_USERNAME=admin
export MARIONETTE_UI_PASSWORD=choose-something-better

./bin/server --config configs/local.yaml
```

To skip admin auth while developing locally, start it with `--dev-insecure-admin`
instead of setting the username and password. Never do that on anything reachable
from a network.

Check it came up, and that admin really is closed:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/health          # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/admin/api/v1/keys # 401
```

### 4. Credentials

Two different tokens: an **API key** for the public API, and a **runner token**
for the agent. Both are minted through the admin API and shown exactly once.

```bash
ADMIN='-u admin:choose-something-better'

export MARIONETTE_API_KEY=$(curl -s $ADMIN -X POST http://localhost:8081/admin/api/v1/keys \
  -H 'Content-Type: application/json' \
  -d '{"name":"local","scopes":["*"]}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])')

export MARIONETTE_RUNNER_TOKEN=$(curl -s $ADMIN -X POST http://localhost:8081/admin/api/v1/runner-tokens \
  -H 'Content-Type: application/json' \
  -d '{"pool_name":"default"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])')
```

### 5. Terminal 2 — runner

This joins the `default` pool as a plain local runner. `--sandbox-mode none` means
the agent runs directly on this host, which is the right choice for a local walk
and the wrong choice for anything else.

```bash
export MARIONETTE_RUNNER_TOKEN=...   # from step 4

./bin/agent \
  --server localhost:9090 \
  --pool default \
  --name local-runner \
  --sandbox-mode none \
  --workspace ./data/workspaces \
  --log-format console
```

### 6. Terminal 3 — drive it

`mctl` reads `MARIONETTE_API_URL` and `MARIONETTE_API_KEY`, so export them once.

```bash
export MARIONETTE_API_URL=http://localhost:8080
export MARIONETTE_API_KEY=...   # from step 4

# Confirm the runner joined. There is no `mctl runners` command yet.
curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" $MARIONETTE_API_URL/api/v1/runners

SESSION=$(./bin/mctl sessions create --agent claude --name local -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')

TASK=$(./bin/mctl tasks create --session $SESSION \
  --prompt 'Run this exact bash command and then stop: echo marionette-alive' \
  -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
```

### 7. Approve what the agent wants to run

The agent asks **before** the tool runs, not after. The task sits waiting until
you answer.

```bash
PERM=$(curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  "$MARIONETTE_API_URL/api/v1/permissions?status=pending" \
  | python3 -c 'import json,sys;i=json.load(sys.stdin)["items"];print(i[0]["id"] if i else "")')

curl -s -X POST -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  -H 'Content-Type: application/json' -d '{"reason":"looks fine"}' \
  "$MARIONETTE_API_URL/api/v1/permissions/$PERM/approve"
```

### 8. Watch it finish

```bash
./bin/mctl tasks get $TASK
./bin/mctl tasks logs $TASK | grep marionette-alive
```

### 9. Suspend and resume

A suspended session keeps its workspace and its agent conversation. Resuming
picks the conversation up where it stopped, on whatever runner is free.

```bash
./bin/mctl sessions suspend $SESSION
./bin/mctl sessions get $SESSION       # suspended

./bin/mctl sessions resume $SESSION
./bin/mctl sessions get $SESSION       # active

./bin/mctl sessions terminate $SESSION
```

## Core concepts

| Concept | What it is |
|---------|-----------|
| **Runner** | Where things run: a container, VM, or pooled machine |
| **marionette-agent** | Our binary inside the runner; talks to the server, runs the agent |
| **Agent** | The AI coding agent itself (Claude Code today) |
| **Session** | A long-lived work context. Outlives individual runners |
| **Task** | One prompt, executed inside a session |
| **Task run** | One execution attempt of a task |
| **Workspace** | The directory the agent works in. Outlives the runner |

The relationship that matters: **sessions outlive runners.** A session can be
suspended, have its runner taken away, and later resume somewhere else with its
workspace and conversation intact.

## Permissions

Marionette gates tool calls *before* they execute, using Claude Code's
`PreToolUse` hook. When the agent wants to run something, the runner asks the
server, the server records a permission request, and the tool does not run until
someone answers.

The policy is deny-by-default on the unknown: known read-only tools pass, and
everything else — including MCP tools and any tool a future CLI release adds —
needs an answer. If the gate cannot start, the task fails rather than running
unsupervised.

See [docs/guides/security.md](docs/guides/security.md).

## Configuration

Config files hold non-sensitive settings. Secrets come from the environment.

```yaml
# configs/local.yaml
server:
  api:    { port: 8080 }
  admin:  { port: 8081 }
  grpc:   { port: 9090 }

tunnels:
  enabled: true       # HTTP proxy and TCP relay

streaming:
  enabled: false      # frozen; see below

multi_tenant: false   # turn tenant isolation from a column into an enforced boundary
```

### Environment variables

**Server**

| Variable | Description |
|----------|-------------|
| `MARIONETTE_DATABASE_URL` | PostgreSQL connection string. Required |
| `MARIONETTE_MASTER_KEY` | Master key for admin operations. Required |
| `MARIONETTE_ENCRYPTION_KEY` | Encrypts stored agent credentials. Required |
| `MARIONETTE_UI_USERNAME` / `MARIONETTE_UI_PASSWORD` | Admin basic auth. Required unless `--dev-insecure-admin` |

**Runner (`bin/agent`)**

| Variable | Description |
|----------|-------------|
| `MARIONETTE_RUNNER_TOKEN` | Runner authentication token. Required |
| `MARIONETTE_SERVER` | gRPC server address (or `--server`) |

**CLI (`bin/mctl`)**

| Variable | Description |
|----------|-------------|
| `MARIONETTE_API_URL` | Public API URL (or `--server`) |
| `MARIONETTE_API_KEY` | API key (or `--api-key`) |

### Workspace sync

A pooled runner that gets released has to put the workspace somewhere. Off by
default; enable it with a backend the runner can write to:

```bash
./bin/agent ... \
  --storage-backend local \
  --storage-local-path /var/marionette/cas \
  --storage-encryption none
```

`--storage-encryption` has no default on purpose: storing workspace contents
unencrypted is a decision you make, not one a missing config value makes for you.
When sync is off, a suspend reports the workspace as **not** synced rather than
implying a snapshot exists.

## CLI

```bash
mctl sessions create --agent claude --name my-project
mctl sessions list
mctl sessions get|suspend|resume|terminate $SESSION_ID

mctl tasks create --session $SESSION_ID --prompt "Fix the failing test"
mctl tasks list --session $SESSION_ID
mctl tasks get|logs|cancel $TASK_ID

mctl tunnels create --session $SESSION_ID --type http --port 3000
mctl tunnels list --session $SESSION_ID

mctl scheduled-tasks list --session $SESSION_ID

mctl admin runner-tokens create --pool-name default \
  --admin-username admin --admin-password ...
mctl admin profiles list
mctl admin sessions list
```

Every command takes `-o json|yaml|table`.

Two gaps worth knowing about: there is no `mctl runners` command yet — list
runners through `GET /api/v1/runners` — and `mctl admin runner-tokens create`
currently prints empty fields, which is why the walk above mints the runner token
with `curl`.

## Frozen features

Some subsystems are compiled but deliberately not wired. They are frozen, not
abandoned, and they are documented as frozen so nobody builds on top of a floor
that is not there:

- **Desktop and browser streaming** (`streaming.enabled: false`). The SFU has no
  media source, no renegotiation, and never reads RTCP, so it cannot deliver a
  frame. Leave it off unless you are the one fixing it.
- **Content-addressable storage.** The chunking, manifest and encryption layers
  work and are tested. The runner can sync and restore a workspace; carrying the
  workspace identity and the manifest id across the wire is still in progress.

## Documentation

Full documentation: **<https://chunlea.github.io/marionette>** (`mkdocs serve` to
read it locally).

| Document | Description |
|----------|-------------|
| [Quick start](docs/getting-started/quick-start.md) | This walk, in more detail |
| [Architecture](docs/concepts/architecture.md) | How the pieces fit |
| [Sessions & tasks](docs/concepts/sessions-tasks.md) | Lifecycles and state machines |
| [API reference](docs/guides/api-reference.md) | The served OpenAPI spec |
| [Security](docs/guides/security.md) | Auth, permissions, tenant isolation |
| [Providers](docs/concepts/providers.md) | Docker, Kubernetes, E2B, pools |
| [Releasing](docs/development/releasing.md) | How a version is cut and published |
| [Database schema](docs/reference/schema.md) | Generated from `migrations/` |

## Development

```bash
make build          # all three binaries, stamped with the git version
make test           # unit tests (macOS-native)
make test-linux     # full suite in Docker, matching CI
make lint           # golangci-lint
make proto          # regenerate protobuf
make schema         # regenerate docs/schema.sql from migrations
make openapi        # regenerate the OpenAPI spec
make dev            # hot reload
make dist           # the mctl release tarballs, as the release workflow builds them

./scripts/smoke.sh  # the end-to-end acceptance walk
```

New code is expected to come with tests; see
[docs/development/contributing.md](docs/development/contributing.md).
Cutting a release is one tag push — see
[docs/development/releasing.md](docs/development/releasing.md).

## License

[MIT](LICENSE)

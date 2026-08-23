# Quick Start

Get a real agent running a real task, end to end.

This is the same walk `scripts/smoke.sh` automates. That script is run against
every change, so if something here stops working, the script is the thing to
trust.

## Prerequisites

- Marionette built ([Installation Guide](installation.md)) — `make build`
- Docker, for PostgreSQL
- The `claude` CLI logged in on this host (`claude auth`). A host login is
  enough; you do not need an `ANTHROPIC_API_KEY` unless you want BYOK.
- Ports 8080, 8081, 9090 and a PostgreSQL port free

!!! info "Three terminals"
    The server, the runner, and you. The runner is not optional: a session with
    no runner attached stays `pending` forever, which is the most common reason
    a first attempt appears to hang.

## Step 1: Database

The schema lives in `migrations/`. `docs/schema.sql` is generated from it for
reading — never provision a database from that file.

```bash
docker run -d --name marionette-pg \
  -e POSTGRES_USER=marionette -e POSTGRES_PASSWORD=marionette \
  -e POSTGRES_DB=marionette -p 5432:5432 postgres:16-alpine

export MARIONETTE_DATABASE_URL='postgres://marionette:marionette@localhost:5432/marionette?sslmode=disable'
make migrate
```

## Step 2: Terminal 1 — the server

The admin API mints API keys, registers runners and can read every session, so
it **fails closed**: the server refuses to start without credentials for it.

```bash
export MARIONETTE_DATABASE_URL='postgres://marionette:marionette@localhost:5432/marionette?sslmode=disable'
export MARIONETTE_MASTER_KEY=$(openssl rand -hex 32)
export MARIONETTE_ENCRYPTION_KEY=$(openssl rand -hex 32)
export MARIONETTE_UI_USERNAME=admin
export MARIONETTE_UI_PASSWORD=choose-something-better

./bin/server --config configs/local.yaml
```

!!! warning "--dev-insecure-admin"
    Passing `--dev-insecure-admin` serves the admin API with no authentication
    at all. It exists for local development. Anything reachable from a network
    must use the credentials instead.

Check both ports, including that admin really is closed:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/health           # 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8081/admin/api/v1/keys # 401
```

## Step 3: Credentials

Two different tokens, and they are not interchangeable:

| Token | Used by | Minted at |
|-------|---------|-----------|
| API key | `mctl` and the public API | `POST /admin/api/v1/keys` |
| Runner token | `bin/agent` | `POST /admin/api/v1/runner-tokens` |

Both are shown exactly once.

```bash
ADMIN='-u admin:choose-something-better'

export MARIONETTE_API_KEY=$(curl -s $ADMIN -X POST http://localhost:8081/admin/api/v1/keys \
  -H 'Content-Type: application/json' \
  -d '{"name":"local","scopes":["*"]}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])')

export MARIONETTE_RUNNER_TOKEN=$(curl -s $ADMIN -X POST http://localhost:8081/admin/api/v1/runner-tokens \
  -H 'Content-Type: application/json' \
  -d '{"pool_name":"default"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["raw_token"])')
```

!!! note
    `mctl admin runner-tokens create` exists but currently prints empty fields,
    which is why this uses `curl`.

## Step 4: Terminal 2 — the runner

```bash
export MARIONETTE_RUNNER_TOKEN=...   # from step 3

./bin/agent \
  --server localhost:9090 \
  --pool default \
  --name local-runner \
  --sandbox-mode none \
  --workspace ./data/workspaces \
  --log-format console
```

`--sandbox-mode none` runs the agent directly on this host. That is right for a
local walk and wrong for anything else — see
[Providers](../concepts/providers.md) for the sandbox modes.

Confirm it joined. There is no `mctl runners` command yet:

```bash
curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" http://localhost:8080/api/v1/runners
```

## Step 5: Terminal 3 — a session and a task

`mctl` reads `MARIONETTE_API_URL` and `MARIONETTE_API_KEY`.

```bash
export MARIONETTE_API_URL=http://localhost:8080
export MARIONETTE_API_KEY=...   # from step 3

SESSION=$(./bin/mctl sessions create --agent claude --name my-first-session -o json \
  | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')

TASK=$(./bin/mctl tasks create --session $SESSION \
  --prompt 'Run this exact bash command and then stop: echo marionette-alive' \
  -o json | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
```

To bring your own key instead of using the host login, pass
`--agent-api-key $ANTHROPIC_API_KEY` to `sessions create`. In BYOK mode the key
is held in memory and never stored.

## Step 6: Approve the tool call

This is the part that surprises people. Marionette gates tool calls **before**
they run, using Claude Code's `PreToolUse` hook. The task will sit and wait
until someone answers — that is the feature working, not a hang.

```bash
PERM=$(curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  "$MARIONETTE_API_URL/api/v1/permissions?status=pending" \
  | python3 -c 'import json,sys;i=json.load(sys.stdin)["items"];print(i[0]["id"] if i else "")')

curl -s -X POST -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  -H 'Content-Type: application/json' -d '{"reason":"looks fine"}' \
  "$MARIONETTE_API_URL/api/v1/permissions/$PERM/approve"
```

Deny it instead with `.../deny`, and the agent is told it was refused rather
than the command being run and disowned afterwards.

## Step 7: Read the result

```bash
./bin/mctl tasks get $TASK
./bin/mctl tasks logs $TASK
./bin/mctl tasks logs $TASK --follow     # stream a running task
```

## Step 8: Continue the conversation

Tasks in the same session share the agent's conversation. The second task knows
what the first one did:

```bash
./bin/mctl tasks create --session $SESSION --prompt 'What command did you just run?'
```

## Step 9: Suspend, resume, terminate

A suspended session keeps its workspace and its agent conversation, and gives
its runner back. Resuming picks the conversation up where it stopped.

```bash
./bin/mctl sessions suspend $SESSION
./bin/mctl sessions get $SESSION       # suspended

./bin/mctl sessions resume $SESSION
./bin/mctl sessions get $SESSION       # active

./bin/mctl sessions terminate $SESSION
```

## Troubleshooting

### The session stays `pending`

`pending` means no runner has been attached. Check that a runner registered and
is `idle`:

```bash
curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" http://localhost:8080/api/v1/runners
```

If the list is empty, terminal 2 is not connected — check its log for a
registration error, usually a wrong or already-used `MARIONETTE_RUNNER_TOKEN`.

### The task never finishes

Most often it is waiting on a permission request nobody answered. List them:

```bash
curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  "$MARIONETTE_API_URL/api/v1/permissions?status=pending"
```

An unanswered request suspends the session after `suspend_after_seconds`
(30 minutes by default). The request stays pending across the suspend, and the
task re-runs after you answer and resume.

### The server refuses to start

If it exits complaining about admin credentials, set `MARIONETTE_UI_USERNAME`
and `MARIONETTE_UI_PASSWORD`, or pass `--dev-insecure-admin` for local work.
This is deliberate: an admin API that is open by default is worse than one that
will not boot.

### `connection refused` from `mctl`

`mctl` talks to the public API on 8080, not the admin API on 8081. Check
`MARIONETTE_API_URL`.

## What's next

- [Configuration](configuration.md) — config files, environment variables, flags
- [Architecture](../concepts/architecture.md) — how the pieces fit together
- [Sessions & tasks](../concepts/sessions-tasks.md) — lifecycles and states
- [Security](../guides/security.md) — auth, permission policy, tenant isolation
- [CLI reference](../guides/cli-reference.md) — every `mctl` command

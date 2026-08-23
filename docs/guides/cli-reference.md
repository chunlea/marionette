# CLI Reference

`mctl` is the command line interface for Marionette.

This page lists what `mctl` can actually do today. Where the CLI does not cover
something, it says so and gives the HTTP call instead — see
[API reference](api-reference.md).

## Global flags

| Flag | Environment | Description |
|------|-------------|-------------|
| `-s, --server` | `MARIONETTE_API_URL` | Public API URL |
| `-k, --api-key` | `MARIONETTE_API_KEY` | API key |
| `-o, --output` | - | `table` (default), `json`, or `yaml` |
| `--config` | - | Config file, default `$HOME/.config/marionette/config.yaml` |
| `--context` | - | Context to use from the config file |

```bash
export MARIONETTE_API_URL=http://localhost:8080
export MARIONETTE_API_KEY=mk_...
```

Admin commands take three more, because the admin API is a different port with
different credentials:

| Flag | Description |
|------|-------------|
| `--admin-server` | Admin API URL (default: `--server` with port 8081) |
| `--admin-username` | Admin basic auth username |
| `--admin-password` | Admin basic auth password |

## Sessions

```bash
mctl sessions create --agent claude --name my-project
mctl sessions list
mctl sessions get $SESSION_ID
mctl sessions suspend $SESSION_ID
mctl sessions resume $SESSION_ID
mctl sessions terminate $SESSION_ID
```

Useful `create` flags:

| Flag | Description |
|------|-------------|
| `--agent` | Agent type, default `claude` |
| `--name` | Human-readable name |
| `--agent-api-key` | BYOK: key held in memory, never stored |
| `--agent-config` | Use a stored agent config instead |
| `--profile` | Profile that selects resources and runner |
| `--lifecycle` | `on_demand` (default), `always_on`, `scheduled` |
| `--idle-timeout` | Seconds before an `on_demand` session suspends, default 1800 |
| `--labels` | Repeatable `key=value` |

## Tasks

```bash
mctl tasks create --session $SESSION_ID --prompt "Fix the failing test"
mctl tasks list --session $SESSION_ID
mctl tasks get $TASK_ID
mctl tasks logs $TASK_ID
mctl tasks logs $TASK_ID --follow
mctl tasks logs $TASK_ID --tail 100
mctl tasks cancel $TASK_ID
```

## Tunnels

```bash
mctl tunnels create --session $SESSION_ID --port 3000
mctl tunnels create --session $SESSION_ID --port 3000 --type tcp
mctl tunnels create --session $SESSION_ID --port 3000 --public
mctl tunnels list --session $SESSION_ID
mctl tunnels get $TUNNEL_ID
mctl tunnels close $TUNNEL_ID
```

`--type` is `http` (default) or `tcp`. `--public` drops the token requirement,
so use it deliberately.

## Scheduled tasks

```bash
mctl scheduled-tasks create --session $SESSION_ID \
  --name daily-summary --cron "0 9 * * *" \
  --prompt "Summarize yesterday's commits"

mctl scheduled-tasks list --session $SESSION_ID
mctl scheduled-tasks get $SCHEDULED_TASK_ID
mctl scheduled-tasks update $SCHEDULED_TASK_ID --cron "0 10 * * *"
mctl scheduled-tasks pause $SCHEDULED_TASK_ID
mctl scheduled-tasks resume $SCHEDULED_TASK_ID
mctl scheduled-tasks trigger $SCHEDULED_TASK_ID
mctl scheduled-tasks delete $SCHEDULED_TASK_ID
```

## Admin

Admin commands talk to the admin API and need its credentials.

```bash
mctl admin runner-tokens create --pool-name default \
  --admin-username admin --admin-password ...
mctl admin runner-tokens list
mctl admin runner-tokens get $TOKEN_ID
mctl admin runner-tokens rotate $TOKEN_ID
mctl admin runner-tokens revoke $TOKEN_ID

mctl admin profiles create|list|get|update|delete
mctl admin sessions activate $SESSION_ID
mctl admin sessions suspend $SESSION_ID
```

!!! warning "Known issue"
    `mctl admin runner-tokens create` currently prints empty fields. Mint runner
    tokens through the admin API until that is fixed:

    ```bash
    curl -s -u admin:PASSWORD -X POST \
      http://localhost:8081/admin/api/v1/runner-tokens \
      -H 'Content-Type: application/json' \
      -d '{"pool_name":"default"}'
    ```

## Contexts

`mctl` supports named contexts, like `kubectl`:

```bash
mctl config set-context prod --server https://marionette.example.com --api-key mk_...
mctl config get-contexts
mctl config use-context prod
mctl config view
mctl config delete-context prod
```

## Not in the CLI yet

These have no `mctl` command. Use the API:

| What | Endpoint |
|------|----------|
| List or inspect runners | `GET /api/v1/runners`, `GET /api/v1/runners/{id}` |
| List pending permissions | `GET /api/v1/permissions?status=pending` |
| Approve or deny a permission | `POST /api/v1/permissions/{id}/approve` \| `/deny` |
| Manage API keys | `POST /admin/api/v1/keys` (admin API) |
| Manage workspaces | `/api/v1/workspaces` |
| Manage agent configs and providers | Admin API |

```bash
# Approve the permission a task is waiting on
PERM=$(curl -s -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  "$MARIONETTE_API_URL/api/v1/permissions?status=pending" \
  | python3 -c 'import json,sys;i=json.load(sys.stdin)["items"];print(i[0]["id"] if i else "")')

curl -s -X POST -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  -H 'Content-Type: application/json' -d '{"reason":"approved"}' \
  "$MARIONETTE_API_URL/api/v1/permissions/$PERM/approve"
```

## See also

- [Quick start](../getting-started/quick-start.md) — the whole walk end to end
- [API reference](api-reference.md) — the generated OpenAPI spec

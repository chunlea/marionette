# API Reference

Marionette generates its OpenAPI spec from the routes the server actually
registers, and serves it. That spec is the reference — this page tells you where
it is and the conventions that apply across all of it.

!!! tip "The spec is generated, this page is not"
    A hand-maintained endpoint list drifts the moment someone adds a field. If
    this page and the served spec disagree, the spec is right.

## Where the spec lives

| | Public API | Admin API |
|---|---|---|
| Base URL | `http://localhost:8080` | `http://localhost:8081` |
| Browsable docs | `/docs` | `/docs` |
| Raw spec | `/openapi.yaml` | `/openapi.yaml` |
| Auth | API key | Basic auth |

```bash
# Browse it
open http://localhost:8080/docs

# Or pull the spec
curl -s http://localhost:8080/openapi.yaml -o openapi.yaml
```

`make openapi` regenerates the checked-in copy, and `make openapi-check` fails
the build if it has drifted from the routes.

## Authentication

Public API requests carry an API key:

```bash
curl -H "Authorization: Bearer $MARIONETTE_API_KEY" \
  http://localhost:8080/api/v1/sessions
```

API keys are minted through the admin API and shown exactly once. They carry
scopes (`sessions:read`, `tasks:write`, `permissions:write`, …); `*` grants all.

The admin API uses basic auth and **fails closed** — the server will not start
without credentials for it unless you pass `--dev-insecure-admin`. See
[Security](security.md).

## What the public API covers

| Group | Endpoints |
|-------|-----------|
| Sessions | create, list, get, suspend, resume, terminate |
| Tasks | create, list, get, execute, cancel, retry, logs, runs |
| Permissions | list, get, approve, deny |
| Runners | list, get |
| Workspaces | create, list, get, update, delete |
| Scheduled tasks | create, list, get, update, delete, pause, resume, trigger |
| Tunnels | create, list, get, delete, plus the proxied `/tunnels/{id}/…` paths |
| Streaming | `/api/v1/events`, task log streams, stream WebSockets |
| Service | `/health`, `/healthz`, `/docs`, `/openapi.yaml` |

Two things worth knowing before you go looking for them:

- A task is created at `POST /api/v1/tasks` with the session in the body, not
  under the session path.
- There is no runner-management endpoint beyond read. Runners are created by
  starting an agent with a runner token, not through the API.

## Conventions

### Pagination

List endpoints are cursor-paginated. Pass `next_cursor` from one response as
`cursor` on the next, and stop when `has_more` is false.

```json
{
  "items": [ ... ],
  "has_more": true,
  "next_cursor": "eyJpZCI6InNlc3NfMDAwMngifQ",
  "total_count": 137
}
```

`limit` defaults to 50.

### Errors

Errors carry a machine-readable code and a human-readable message:

```json
{
  "error": {
    "code": "not_found",
    "message": "session sess_0002xK9mNpV1StGXR8 does not exist"
  }
}
```

| Status | Meaning |
|--------|---------|
| 400 | Malformed request |
| 401 | Missing or invalid credentials |
| 403 | Authenticated, but the key lacks the scope |
| 404 | No such resource |
| 409 | Conflicts with current state, e.g. suspending a terminated session |
| 422 | Well-formed but semantically invalid |
| 500 | Server error |

### Identifiers

Every id is prefixed by its type — `sess_`, `task_`, `trun_`, `perm_`, `ws_`,
`run_` — and is time-ordered, so sorting by id sorts by creation. See
[ID generation](../reference/id.md).

### Timestamps

RFC 3339, UTC. Fields that cross the gRPC boundary use `_unix_ms` suffixes and
carry milliseconds since the epoch.

## Rate limiting

There is none. Nothing throttles a client today; do not build against headers
that are not sent.

## See also

- [CLI reference](cli-reference.md) — `mctl` covers most of this API
- [Security](security.md) — keys, scopes, permission gating
- [Sessions & tasks](../concepts/sessions-tasks.md) — the state machines behind the verbs

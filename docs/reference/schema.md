# Database Schema

Marionette uses PostgreSQL for persistent storage.

## Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Database Schema                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Core Tables                    Configuration Tables                │
│  ───────────                    ────────────────────                │
│  ┌───────────┐                  ┌─────────────────┐                 │
│  │  runners  │                  │ provider_configs│                 │
│  └─────┬─────┘                  └─────────────────┘                 │
│        │                        ┌─────────────────┐                 │
│  ┌─────▼─────┐                  │  agent_configs  │                 │
│  │ sessions  │                  └─────────────────┘                 │
│  └─────┬─────┘                  ┌─────────────────┐                 │
│        │                        │    profiles     │                 │
│  ┌─────▼─────┐                  └─────────────────┘                 │
│  │   tasks   │                                                      │
│  └─────┬─────┘                  Auth Tables                         │
│        │                        ────────────                        │
│  ┌─────▼─────┐                  ┌─────────────────┐                 │
│  │ task_runs │                  │    api_keys     │                 │
│  └───────────┘                  └─────────────────┘                 │
│                                 ┌─────────────────┐                 │
│  ┌───────────┐                  │  runner_tokens  │                 │
│  │permissions│                  └─────────────────┘                 │
│  └───────────┘                                                      │
│                                 Storage Tables                      │
│  ┌───────────┐                  ──────────────────                  │
│  │workspaces │                  ┌─────────────────┐                 │
│  └───────────┘                  │     chunks      │                 │
│                                 └─────────────────┘                 │
│  ┌───────────┐                  ┌─────────────────┐                 │
│  │  tunnels  │                  │    manifests    │                 │
│  └───────────┘                  └─────────────────┘                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Core Tables

### sessions

Long-lived work contexts.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (`sess_xxx`) |
| `name` | TEXT | Human-readable name |
| `status` | TEXT | pending, active, suspended, resuming, terminated |
| `runner_id` | TEXT | Current runner (nullable) |
| `workspace_id` | TEXT | Associated workspace |
| `agent` | TEXT | Agent type (claude, codex, etc.) |
| `tenant_id` | TEXT | Tenant isolation |

### tasks

Units of work within a session.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (`task_xxx`) |
| `session_id` | TEXT | Parent session |
| `prompt` | TEXT | The task prompt |
| `status` | TEXT | pending, running, completed, failed, canceled |
| `max_retries` | INT | Maximum retry attempts |
| `timeout_seconds` | INT | Task timeout |

### task_runs

Execution attempts for tasks.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (`trun_xxx`) |
| `task_id` | TEXT | Parent task |
| `attempt` | INT | Attempt number |
| `status` | TEXT | pending, assigned, running, completed, failed, timeout |
| `exit_code` | INT | Process exit code |
| `tokens_input` | INT | Input tokens used |
| `tokens_output` | INT | Output tokens used |

### runners

Execution environments.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (`run_xxx`) |
| `name` | TEXT | Runner name |
| `status` | TEXT | offline, idle, busy, paused |
| `sandbox_mode` | TEXT | runner-is-sandbox, runner-creates-sandbox |
| `pool_name` | TEXT | Pool membership (nullable) |
| `provider_config_id` | TEXT | Provider configuration |

### permission_requests

Async permission approvals.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT | Primary key (`perm_xxx`) |
| `session_id` | TEXT | Parent session |
| `task_id` | TEXT | Parent task |
| `tool` | TEXT | Tool requesting permission |
| `action` | TEXT | Action description |
| `status` | TEXT | pending, approved, denied, canceled |
| `risk_level` | TEXT | low, medium, high, critical |

## ID Format

All IDs use Stripe-style prefixed format:

```
{prefix}_{base62_timestamp}{nanoid}

Examples:
  sess_0002xK9mNpV1StGXR8
  task_0002xK9mNqW2TuHYS9
```

| Prefix | Resource |
|--------|----------|
| `run_` | runner |
| `sess_` | session |
| `task_` | task |
| `trun_` | task_run |
| `perm_` | permission_request |
| `ws_` | workspace |
| `key_` | api_key |

See [ID Generation](id.md) for details.

## Tenant Isolation

All tables have `tenant_id` column:

- Single-tenant: `NULL` everywhere
- Multi-tenant: NOT NULL enforced at application layer
- Injected by auth middleware, never from user input

## Full Schema

For the complete schema definition, see:

[`docs/schema.sql`](https://github.com/chunlea/marionette/blob/main/docs/schema.sql)

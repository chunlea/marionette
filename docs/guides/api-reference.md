# API Reference

REST API documentation for Marionette.

## Base URL

```
http://localhost:8080/api/v1
```

## Authentication

All API requests require an API key in the `Authorization` header:

```bash
Authorization: Bearer mk_your_api_key
```

## Sessions

### Create Session

```http
POST /api/v1/sessions
```

**Request:**

```json
{
  "agent": "claude",
  "api_key": "sk-ant-xxx",
  "name": "my-session",
  "labels": {
    "user": "alice",
    "project": "api"
  },
  "lifecycle_mode": "on_demand",
  "idle_timeout_seconds": 1800
}
```

**Response:**

```json
{
  "id": "sess_0002xK9mNpV1StGXR8",
  "name": "my-session",
  "status": "pending",
  "agent": "claude",
  "workspace_id": "ws_0002xK9mNpV1StGXR9",
  "labels": {"user": "alice", "project": "api"},
  "created_at": "2024-01-15T10:30:00Z"
}
```

### List Sessions

```http
GET /api/v1/sessions
GET /api/v1/sessions?status=active
GET /api/v1/sessions?labels=user:alice
```

### Get Session

```http
GET /api/v1/sessions/{session_id}
```

### Suspend Session

```http
POST /api/v1/sessions/{session_id}/suspend
```

### Resume Session

```http
POST /api/v1/sessions/{session_id}/resume
```

### Terminate Session

```http
DELETE /api/v1/sessions/{session_id}
```

## Tasks

### Create Task

```http
POST /api/v1/tasks
```

**Request:**

```json
{
  "session_id": "sess_0002xK9mNpV1StGXR8",
  "prompt": "Build a REST API with authentication",
  "timeout_seconds": 3600,
  "max_retries": 2
}
```

**Response:**

```json
{
  "id": "task_0002xK9mNqW2TuHYS9",
  "session_id": "sess_0002xK9mNpV1StGXR8",
  "prompt": "Build a REST API with authentication",
  "status": "pending",
  "created_at": "2024-01-15T10:35:00Z"
}
```

### List Tasks

```http
GET /api/v1/tasks
GET /api/v1/tasks?status=running
GET /api/v1/tasks?session_id=sess_xxx
```

### Get Task

```http
GET /api/v1/tasks/{task_id}
```

### Get Task Logs

```http
GET /api/v1/tasks/{task_id}/logs
GET /api/v1/tasks/{task_id}/logs?stream=stdout
```

**Response:**

```json
{
  "items": [
    {"stream": "stdout", "content": "Creating main.go...", "sequence": 1, "created_at": "..."},
    {"stream": "stdout", "content": "Writing code...", "sequence": 2, "created_at": "..."}
  ],
  "total": 2
}
```

### Execute Task

Manually trigger execution of a pending task.

```http
POST /api/v1/tasks/{task_id}/execute
```

### Cancel Task

```http
POST /api/v1/tasks/{task_id}/cancel
```

### Retry Task

Retry a failed task.

```http
POST /api/v1/tasks/{task_id}/retry
```

## Permission Requests

### List Permissions

```http
GET /api/v1/permissions
GET /api/v1/permissions?status=pending
GET /api/v1/permissions?session_id=sess_xxx
```

### Get Permission

```http
GET /api/v1/permissions/{permission_id}
```

### Approve Permission

```http
POST /api/v1/permissions/{permission_id}/approve
```

**Request:**

```json
{
  "reason": "Approved for testing"
}
```

### Deny Permission

```http
POST /api/v1/permissions/{permission_id}/deny
```

**Request:**

```json
{
  "reason": "Command not allowed"
}
```

## Runners

### List Runners

```http
GET /api/v1/runners
GET /api/v1/runners?status=idle
GET /api/v1/runners?pool=macos
```

### Get Runner

```http
GET /api/v1/runners/{runner_id}
```

## Tunnels

### Create Tunnel

```http
POST /api/v1/sessions/{session_id}/tunnels
```

**Request:**

```json
{
  "type": "http",
  "local_port": 3000,
  "is_public": false
}
```

**Response:**

```json
{
  "id": "tun_0002xK9mNrX3UvIZT0",
  "session_id": "sess_0002xK9mNpV1StGXR8",
  "type": "http",
  "local_port": 3000,
  "public_url": "https://tun-abc123.marionette.example.com",
  "token": "ttok_xxx",
  "expires_at": "2024-01-15T12:30:00Z"
}
```

### List Tunnels

```http
GET /api/v1/sessions/{session_id}/tunnels
```

### Close Tunnel

```http
DELETE /api/v1/tunnels/{tunnel_id}
```

## Workspaces

### Create Workspace

```http
POST /api/v1/workspaces
```

**Request:**

```json
{
  "name": "my-workspace",
  "persist": true,
  "storage_type": "volume",
  "disk_quota_mb": 1024
}
```

### List Workspaces

```http
GET /api/v1/workspaces
```

### Get Workspace

```http
GET /api/v1/workspaces/{workspace_id}
```

### Update Workspace

```http
PATCH /api/v1/workspaces/{workspace_id}
```

### Delete Workspace

```http
DELETE /api/v1/workspaces/{workspace_id}
```

## WebSocket Endpoints

### Log Streaming

Stream task logs in real-time via WebSocket.

```
ws://localhost:8080/api/v1/logs/{task_id}/stream
```

Messages:

```json
{"stream": "stdout", "content": "Building...", "sequence": 42, "created_at": "..."}
```

### Event Stream

Stream server events (sessions, tasks, permissions).

```
ws://localhost:8080/api/v1/events
```

Events:

- `session.status_changed`
- `task.created`
- `task.status_changed`
- `permission.requested`
- `permission.responded`

### Browser/Desktop Streaming

Stream browser or desktop frames via WebSocket.

```
ws://localhost:8080/api/v1/streams/{stream_id}/ws?token=ttok_xxx
```

## Error Responses

All errors follow this format:

```json
{
  "error": {
    "code": "not_found",
    "message": "Session not found",
    "details": {
      "session_id": "sess_invalid"
    }
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `bad_request` | 400 | Invalid request format |
| `unauthorized` | 401 | Invalid or missing API key |
| `forbidden` | 403 | Insufficient permissions |
| `not_found` | 404 | Resource not found |
| `conflict` | 409 | Resource state conflict |
| `rate_limited` | 429 | Too many requests |
| `internal_error` | 500 | Server error |

## Rate Limiting

Default limits:

| Endpoint | Limit |
|----------|-------|
| Session creation | 10/minute |
| Task creation | 30/minute |
| Log streaming | 100 connections |

Rate limit headers:

```
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 7
X-RateLimit-Reset: 1705315200
```

## Pagination

List endpoints support pagination:

```http
GET /api/v1/sessions?limit=20&offset=40
```

Response includes pagination info:

```json
{
  "items": [...],
  "total": 156,
  "limit": 20,
  "offset": 40
}
```

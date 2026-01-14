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

### Update Session

```http
PATCH /api/v1/sessions/{session_id}
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
POST /api/v1/sessions/{session_id}/tasks
```

**Request:**

```json
{
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
GET /api/v1/sessions/{session_id}/tasks
GET /api/v1/tasks?status=running
```

### Get Task

```http
GET /api/v1/tasks/{task_id}
```

### Stream Task Logs

```http
GET /api/v1/tasks/{task_id}/logs?follow=true
```

Returns Server-Sent Events (SSE):

```
event: log
data: {"stream": "stdout", "content": "Creating main.go...", "timestamp": "..."}

event: log
data: {"stream": "stdout", "content": "Writing code...", "timestamp": "..."}
```

### Cancel Task

```http
POST /api/v1/tasks/{task_id}/cancel
```

## Permission Requests

### List Permissions

```http
GET /api/v1/sessions/{session_id}/permissions
GET /api/v1/permissions?status=pending
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

## WebSocket Endpoints

### Session Events

```
ws://localhost:8080/api/v1/sessions/{session_id}/events
```

Events:

- `session.status_changed`
- `task.created`
- `task.status_changed`
- `permission.requested`
- `permission.responded`
- `log.entry`

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

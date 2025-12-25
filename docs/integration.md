# Integration Points

## Integration points

Marionette is designed to be embedded into larger systems. Integrators can use these hooks to implement their own SaaS features (multi-tenancy, billing, quotas, etc.):

### Webhooks

Subscribe to lifecycle events via HTTP webhooks:

```yaml
webhooks:
  - url: "https://your-system.com/hooks/marionette"
    events:
      - agent.spawned
      - agent.connected
      - agent.disconnected
      - agent.destroyed
      - task.created
      - task.started
      - task.completed
      - task.failed
      - task.timeout
      - workspace.created
      - workspace.deleted
    headers:
      Authorization: "Bearer ${WEBHOOK_SECRET}"
```

**Webhook payload**:
```json
{
  "event": "task.completed",
  "timestamp": "2025-01-15T10:30:00Z",
  "resource": {
    "id": "01234567-89ab-cdef-0123-456789abcdef",
    "type": "task",
    "labels": {"org": "acme", "team": "backend"},
    "annotations": {"billing/account": "acc-123"}
  },
  "data": {
    "session_id": "...",
    "agent": "claude",
    "duration_seconds": 1234,
    "status": "completed"
  }
}
```

### Metrics (Prometheus)

Expose metrics at `/metrics` endpoint:

```prometheus
# Resource counts by label
marionette_runners_total{org="acme",env="production",status="online"} 5
marionette_sessions_total{org="acme",status="active"} 10
marionette_tasks_total{org="acme",status="completed"} 1234

# Usage metrics
marionette_task_duration_seconds{org="acme",agent="claude"} 
marionette_task_tokens_total{org="acme",agent="claude",type="input"} 50000
marionette_task_tokens_total{org="acme",agent="claude",type="output"} 25000

# Resource consumption
marionette_compute_seconds_total{org="acme",provider="kubernetes"}
marionette_storage_bytes{org="acme",workspace="project-alpha"}
```

### Usage API

Query aggregated usage data:

```bash
# Get usage summary for an org
GET /api/v1/usage?selector=org=acme&from=2025-01-01&to=2025-01-31

# Response
{
  "period": {"from": "2025-01-01", "to": "2025-01-31"},
  "selector": "org=acme",
  "summary": {
    "tasks": {"total": 1234, "completed": 1200, "failed": 34},
    "compute_seconds": 45678,
    "tokens": {"input": 1000000, "output": 500000},
    "storage_bytes": 10737418240
  },
  "by_label": {
    "team=backend": {"tasks": 800, "compute_seconds": 30000},
    "team=frontend": {"tasks": 434, "compute_seconds": 15678}
  }
}
```

### Middleware hooks

Inject custom logic into request processing:

```go
// Integrator implements custom middleware
type AuthzMiddleware struct {
    quotaService QuotaService
    billingService BillingService
}

func (m *AuthzMiddleware) BeforeTaskCreate(ctx context.Context, req *CreateTaskRequest) error {
    org := req.Labels["org"]
    
    // Check quota
    if !m.quotaService.CanCreateTask(ctx, org) {
        return ErrQuotaExceeded
    }
    
    // Check billing status
    if !m.billingService.IsAccountActive(ctx, org) {
        return ErrAccountSuspended
    }
    
    return nil
}

func (m *AuthzMiddleware) AfterTaskComplete(ctx context.Context, task *Task) {
    // Record usage for billing
    m.billingService.RecordUsage(ctx, task.Labels["org"], task.Usage)
}

// Register middleware with server
server.Use(authzMiddleware)
```

### Event streaming

Subscribe to real-time events via WebSocket or Server-Sent Events:

```
GET /api/v1/events?selector=org=acme

event: task.started
data: {"task_id": "...", "agent_id": "...", "labels": {...}}

event: task.completed  
data: {"task_id": "...", "duration_seconds": 123, "tokens": {...}}
```

### Label propagation

Labels flow through the system for consistent tracking:

```
Provider Config (org=acme, env=prod)
    └─► Agent (inherits: org=acme, env=prod)
            └─► Task (inherits: org=acme, env=prod, + project=api)
                    └─► Workspace (inherits: org=acme, env=prod, project=api)
                    └─► Logs (tagged: org=acme, env=prod, project=api)
                    └─► Metrics (labeled: org=acme, env=prod, project=api)
```


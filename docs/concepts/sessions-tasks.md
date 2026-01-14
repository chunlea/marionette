# Sessions & Tasks

Understanding how Marionette manages work contexts and execution.

## Sessions

A **Session** is a long-lived work context that:

- Binds an AI agent to a workspace
- Survives individual tasks and runner changes
- Can be suspended and resumed
- Maintains conversation context

### Session Lifecycle

=== "Diagram"

    ```mermaid
    stateDiagram-v2
        [*] --> pending
        pending --> active: assign runner
        active --> suspended: suspend
        active --> terminated: terminate
        suspended --> active: resume
        terminated --> [*]
    ```

=== "Text"

    ```
    ┌─────────┐     assign      ┌─────────┐
    │ pending │────────────────►│ active  │
    └─────────┘                 └────┬────┘
                                     │
                        ┌────────────┼────────────┐
                        │            │            │
                        ▼            │            ▼
                  ┌───────────┐     │      ┌────────────┐
                  │ suspended │     │      │ terminated │
                  └─────┬─────┘     │      └────────────┘
                        │           │
                        │  resume   │
                        └───────────┘
    ```

| State | Description |
|-------|-------------|
| `pending` | Waiting for runner assignment |
| `active` | Runner attached, can execute tasks |
| `suspended` | Runner released, state preserved |
| `terminated` | Ended, resources cleaned up |

### Session-Runner Relationship

!!! important "Key Principle"
    Sessions **outlive** runners. A session can have 0 or 1 runner at any time.
    Runners can be attached/detached without losing session state.

=== "Diagram"

    ```mermaid
    flowchart LR
        subgraph Session
            pending
            active
            suspended
            resuming
        end
        subgraph Runner
            none["(no runner)"]
            idle["idle/busy"]
            released["(released)"]
            acquiring["(acquiring)"]
        end
        pending --- none
        active <--> idle
        suspended --- released
        resuming --- acquiring
    ```

=== "Text"

    ```
    Session                     Runner
    ─────────                   ──────
    pending     ←───────────    (no runner)
    active      ←───────────►   idle/busy
    suspended   ←───────────    (released)
    resuming    ←───────────    (acquiring)
    active      ←───────────►   idle/busy
    ```

### What Persists Across Runner Changes

| Component | Storage | Survives Runner Change |
|-----------|---------|------------------------|
| Workspace files | CAS / Volume | ✓ Yes |
| Agent context | DB (snapshot) | ✓ Yes |
| Session metadata | DB | ✓ Yes |
| Pending permissions | DB | ✓ Yes |
| Running processes | Memory | ✗ No |
| Network connections | Memory | ✗ No |

### Lifecycle Modes

Sessions can operate in different modes:

| Mode | Behavior | Use Case |
|------|----------|----------|
| `on_demand` | Auto-suspend after idle timeout | Cost-efficient development |
| `always_on` | Never auto-suspend | 24/7 cloud assistant |
| `scheduled` | Activated by cron schedule | Daily reports, periodic tasks |

## Tasks

A **Task** is a unit of work (a prompt) submitted to a session.

### Task Lifecycle

=== "Diagram"

    ```mermaid
    stateDiagram-v2
        [*] --> pending
        pending --> running: assign
        running --> completed: success
        running --> failed: error
        running --> canceled: cancel
        running --> timeout: timeout
        completed --> [*]
        failed --> [*]
        canceled --> [*]
        timeout --> [*]
    ```

=== "Text"

    ```
    ┌─────────┐    assign     ┌─────────┐    start    ┌─────────┐
    │ pending │──────────────►│ running │────────────►│completed│
    └─────────┘               └────┬────┘             └─────────┘
                                   │
                        ┌──────────┼──────────┐
                        │          │          │
                        ▼          ▼          ▼
                  ┌────────┐ ┌────────┐ ┌──────────┐
                  │ failed │ │canceled│ │ timeout  │
                  └────────┘ └────────┘ └──────────┘
    ```

| State | Description |
|-------|-------------|
| `pending` | Waiting for execution |
| `running` | Currently being executed |
| `completed` | Finished successfully |
| `failed` | Failed with error |
| `canceled` | Canceled by user |
| `timeout` | Exceeded timeout |

### Task Runs

Each task can have multiple **runs** (execution attempts):

=== "Diagram"

    ```mermaid
    flowchart LR
        Task --> R1["Run 1 ❌<br/>timeout"]
        Task --> R2["Run 2 ❌<br/>error"]
        Task --> R3["Run 3 ✅<br/>completed"]
    ```

=== "Text"

    ```
    Task
    ├── Run 1 (attempt=1): failed (timeout)
    ├── Run 2 (attempt=2): failed (error)
    └── Run 3 (attempt=3): completed ✓
    ```

Configure retries:

```bash
mctl tasks create \
  --session $SESSION_ID \
  --prompt "Build the API" \
  --max-retries 3 \
  --timeout 3600
```

## Permission Requests

When an agent needs approval for sensitive operations:

=== "Diagram"

    ```mermaid
    flowchart TD
        A[Agent requests permission] --> P["PermissionRequest<br/>(status: pending)"]
        P --> Approved
        P --> Denied
        P --> NoResponse["No response (30 min)"]

        Approved --> Continue["Continue with action"]
        Denied --> Handle["Handle gracefully"]
        NoResponse --> Suspend["Session suspended"]
        Suspend --> Resume["User resumes +<br/>responds later"]
    ```

=== "Text"

    ```
    Agent requests permission
             │
             ▼
    ┌─────────────────────────────────────────┐
    │  PermissionRequest (status: pending)    │
    │  Runner blocks, waiting for approval    │
    └────────────────────┬────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         ▼               ▼               ▼
      Approved        Denied       No response
         │               │         (30 min)
         ▼               ▼               │
      Continue      Handle          Session
      with action   gracefully      suspended
                                         │
                                         ▼
                                  User resumes +
                                  responds later
    ```

!!! note "No Auto-Deny"
    Permission requests stay pending until explicit user response.
    The session suspends (not fails) if no response is received.

### Permission Levels

| Level | Examples | Default Timeout |
|-------|----------|-----------------|
| `low` | Read files, list directories | Auto-approve (configurable) |
| `medium` | Write files, run safe commands | 30 minutes |
| `high` | Network access, install packages | 30 minutes |
| `critical` | Delete files, modify system | Must approve |

## Suspend & Resume

### Suspend Strategies

Different providers support different suspend strategies:

| Strategy | Memory | Storage | Speed | Cost |
|----------|--------|---------|-------|------|
| `pause` | Preserved | Preserved | Instant | High |
| `snapshot` | Snapshot | Preserved | Slow | Medium |
| `terminate_preserve_storage` | Lost | Preserved | Medium | Low |
| `release_to_pool` | Lost | Synced | Medium | Low |
| `terminate` | Lost | Lost | Fast | None |

### Suspend Scenarios

**Permission Timeout:**
```
Session active → Permission requested → 30 min timeout →
Session suspended → User approves → Session resumes → Continue
```

**Idle Timeout:**
```
Session active → No tasks for idle_timeout →
Session suspended → User creates task → Resume → Execute
```

**Runner Failure:**
```
Session active → Runner crashes →
Session suspended → Auto-resume with new runner
```

## Examples

### Create Session with Lifecycle Mode

```bash
# Always-on session (24/7)
mctl sessions create \
  --agent claude \
  --lifecycle always_on \
  --name "assistant"

# Scheduled session (daily at 9am)
mctl sessions create \
  --agent claude \
  --lifecycle scheduled \
  --schedule "0 9 * * *" \
  --name "daily-reporter"
```

### Handle Suspended Session

```bash
# Check why session is suspended
mctl sessions get $SESSION_ID

# Resume the session
mctl sessions resume $SESSION_ID

# Check pending permissions
mctl permissions list --session $SESSION_ID

# Approve pending permission
mctl permissions approve $PERMISSION_ID
```

### Continue from Previous Task

```bash
# Get the last task
LAST_TASK=$(mctl tasks list --session $SESSION_ID --limit 1 -o json | jq -r '.[0].id')

# Continue from it
mctl tasks create \
  --continue $LAST_TASK \
  --prompt "Now add authentication"
```

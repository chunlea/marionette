# Quick Start

Get your first AI coding agent running in under 5 minutes.

## Prerequisites

- Marionette installed ([Installation Guide](installation.md))
- An Anthropic API key (for Claude Code) or OpenAI API key (for Codex)

## Step 1: Start the Server

=== "Docker Compose"

    ```bash
    docker compose up -d
    ```

=== "Local Binary"

    ```bash
    ./bin/server --config configs/local.yaml
    ```

Verify the server is running:

```bash
curl http://localhost:8081/health/ready
# {"status":"ok","checks":{"database":{"status":"ok"}}}
```

## Step 2: Create a Session

A session is a long-lived work context that binds an AI agent to a workspace.

=== "CLI"

    ```bash
    # Set your API key
    export ANTHROPIC_API_KEY=sk-ant-xxx

    # Create a session with Claude Code
    ./bin/mctl sessions create \
      --agent claude \
      --api-key $ANTHROPIC_API_KEY \
      --name "my-first-session"
    ```

=== "API"

    ```bash
    curl -X POST http://localhost:8080/api/v1/sessions \
      -H "Authorization: Bearer $MARIONETTE_API_KEY" \
      -H "Content-Type: application/json" \
      -d '{
        "agent": "claude",
        "api_key": "sk-ant-xxx",
        "name": "my-first-session"
      }'
    ```

You'll receive a session ID like `sess_0002xK9mNpV1StGXR8`.

```bash
export SESSION_ID=sess_0002xK9mNpV1StGXR8
```

## Step 3: Submit a Task

Now let's give the agent some work to do:

=== "CLI"

    ```bash
    ./bin/mctl tasks create \
      --session $SESSION_ID \
      --prompt "Create a simple Python Flask API with a /hello endpoint"
    ```

=== "API"

    ```bash
    curl -X POST http://localhost:8080/api/v1/sessions/$SESSION_ID/tasks \
      -H "Authorization: Bearer $MARIONETTE_API_KEY" \
      -H "Content-Type: application/json" \
      -d '{
        "prompt": "Create a simple Python Flask API with a /hello endpoint"
      }'
    ```

## Step 4: Watch the Logs

Follow the agent's progress in real-time:

=== "CLI"

    ```bash
    ./bin/mctl tasks logs --follow $TASK_ID
    ```

=== "API (SSE)"

    ```bash
    curl -N http://localhost:8080/api/v1/tasks/$TASK_ID/logs?follow=true \
      -H "Authorization: Bearer $MARIONETTE_API_KEY"
    ```

You'll see the agent:

1. Analyzing the request
2. Creating files in the workspace
3. Writing code
4. Testing the implementation

## Step 5: Handle Permission Requests

When the agent needs to perform sensitive operations (like running shell commands), it will request permission:

```bash
# List pending permissions
./bin/mctl permissions list --session $SESSION_ID

# Approve a permission request
./bin/mctl permissions approve $PERMISSION_ID

# Or deny it
./bin/mctl permissions deny $PERMISSION_ID --reason "Not authorized"
```

## Step 6: Continue the Conversation

Submit follow-up tasks to build on the agent's work:

```bash
./bin/mctl tasks create \
  --session $SESSION_ID \
  --prompt "Add a /goodbye endpoint that returns 'Goodbye, World!'"
```

The agent remembers the context and continues from where it left off.

## Step 7: Manage the Session

```bash
# Suspend the session (free up resources)
./bin/mctl sessions suspend $SESSION_ID

# Resume later
./bin/mctl sessions resume $SESSION_ID

# Terminate when done
./bin/mctl sessions terminate $SESSION_ID
```

## What's Next?

<div class="grid cards" markdown>

-   [:octicons-gear-24: __Configuration__](configuration.md)

    Configure providers, storage, and observability

-   [:octicons-book-24: __Architecture__](../concepts/architecture.md)

    Understand how Marionette works

-   [:octicons-terminal-24: __CLI Reference__](../guides/cli-reference.md)

    Complete CLI documentation

-   [:octicons-code-24: __API Reference__](../guides/api-reference.md)

    REST API documentation

</div>

## Troubleshooting

### Session stuck in "pending"

The session is waiting for a runner. Check if runners are available:

```bash
./bin/mctl runners list
```

If no runners are available, start one:

```bash
./bin/agent --server localhost:9090 --token dev-token
```

### Permission request timeout

By default, permission requests wait 30 minutes for approval. If no response is received, the session is suspended (not failed). Resume and respond:

```bash
./bin/mctl sessions resume $SESSION_ID
./bin/mctl permissions approve $PERMISSION_ID
```

### Connection refused

Ensure the server is running and check the ports:

```bash
# Check if server is listening
lsof -i :8080
lsof -i :9090
```

# CLI Reference

The `mctl` command-line tool for managing Marionette.

## Configuration

```bash
# Set API endpoint
export MARIONETTE_API_URL=http://localhost:8080

# Set API key
export MARIONETTE_API_KEY=mk_your_api_key
```

Or use flags:

```bash
mctl --api-url http://localhost:8080 --api-key mk_xxx sessions list
```

## Sessions

### List Sessions

```bash
mctl sessions list
mctl sessions list --status active
mctl sessions list --labels user=alice
mctl sessions list -o json
```

### Create Session

```bash
# Basic (BYOK mode - Bring Your Own Key)
mctl sessions create --agent claude --agent-api-key $ANTHROPIC_API_KEY

# Using managed agent config
mctl sessions create --agent claude --agent-config acfg_production

# With options
mctl sessions create \
  --agent claude \
  --agent-api-key $ANTHROPIC_API_KEY \
  --name "my-project" \
  --labels "user=alice,project=api" \
  --lifecycle on_demand \
  --idle-timeout 1800
```

### Get Session Details

```bash
mctl sessions get $SESSION_ID
mctl sessions get $SESSION_ID -o json
```

### Manage Session State

```bash
# Suspend (release runner, preserve state)
mctl sessions suspend $SESSION_ID

# Resume
mctl sessions resume $SESSION_ID

# Terminate (cleanup all resources)
mctl sessions terminate $SESSION_ID
```

## Tasks

### Create Task

```bash
# Basic
mctl tasks create --session $SESSION_ID --prompt "Build a REST API"

# Read prompt from file
mctl tasks create --session $SESSION_ID --prompt-file task.md

# With options
mctl tasks create \
  --session $SESSION_ID \
  --prompt "Build a REST API" \
  --timeout 3600 \
  --max-retries 2

# Wait for completion
mctl tasks create --session $SESSION_ID --prompt "Run tests" --wait

# Follow logs during execution
mctl tasks create --session $SESSION_ID --prompt "Build API" --follow

# Continue from previous task
mctl tasks create \
  --continue $PREVIOUS_TASK_ID \
  --prompt "Add authentication"
```

### List Tasks

```bash
mctl tasks list --session $SESSION_ID
mctl tasks list --session $SESSION_ID --status running
```

### Get Task Details

```bash
mctl tasks get $TASK_ID
mctl tasks get $TASK_ID -o json
```

### Stream Logs

```bash
# Follow logs in real-time
mctl tasks logs $TASK_ID --follow
mctl tasks logs $TASK_ID -f

# Get last N lines
mctl tasks logs $TASK_ID --tail 100
```

### Cancel Task

```bash
mctl tasks cancel $TASK_ID
mctl tasks cancel $TASK_ID --reason "No longer needed"
```

## Permissions

### List Pending Permissions

```bash
mctl permissions list --session $SESSION_ID
mctl permissions list --status pending
```

### Approve Permission

```bash
mctl permissions approve $PERMISSION_ID
mctl permissions approve $PERMISSION_ID --reason "Looks safe"
```

### Deny Permission

```bash
mctl permissions deny $PERMISSION_ID
mctl permissions deny $PERMISSION_ID --reason "Too risky"
```

## Runners

### List Runners

```bash
mctl runners list
mctl runners list --status idle
mctl runners list --pool macos
```

### Get Runner Details

```bash
mctl runners get $RUNNER_ID
```

### Manage Runner State

```bash
# Pause runner (freeze)
mctl runners pause $RUNNER_ID

# Resume runner
mctl runners resume $RUNNER_ID

# Destroy runner
mctl runners destroy $RUNNER_ID
```

## Tunnels

### Create Tunnel

```bash
# HTTP tunnel
mctl tunnels create --session $SESSION_ID --type http --port 3000

# Desktop streaming
mctl tunnels create --session $SESSION_ID --type desktop

# Public tunnel (no auth required)
mctl tunnels create --session $SESSION_ID --type http --port 8080 --public
```

### List Tunnels

```bash
mctl tunnels list --session $SESSION_ID
```

### Close Tunnel

```bash
mctl tunnels close $TUNNEL_ID
```

## Admin Commands

Requires master key authentication.

### API Keys

```bash
# Create API key
mctl admin keys create --name "ci-key" --scopes "tasks:*,sessions:read"

# List keys
mctl admin keys list

# Revoke key
mctl admin keys revoke $KEY_ID --reason "Compromised"
```

### Agent Configs

```bash
# Create agent config (stores encrypted API key)
mctl admin agent-configs create \
  --name "claude-prod" \
  --agent claude \
  --api-key $ANTHROPIC_API_KEY

# List configs
mctl admin agent-configs list
```

### Runner Tokens

```bash
# Create runner token for pool
mctl admin runner-tokens create --pool macos

# List tokens
mctl admin runner-tokens list

# Revoke token
mctl admin runner-tokens revoke $TOKEN_ID
```

### Provider Configs

```bash
# Create provider config
mctl admin providers create docker \
  --name "docker-local" \
  --image "marionette/agent:latest"

# List providers
mctl admin providers list
```

## Output Formats

All commands support output format flags:

| Flag | Format |
|------|--------|
| `-o json` | JSON output |
| `-o yaml` | YAML output |
| `-o table` | Table output (default) |
| `-o wide` | Wide table with more columns |

## Global Flags

| Flag | Description |
|------|-------------|
| `--api-url` | API endpoint URL |
| `--api-key` | API key for authentication |
| `--config` | Config file path |
| `-v, --verbose` | Verbose output |
| `--debug` | Debug output |
| `-h, --help` | Help for command |

## Examples

### Complete Workflow

```bash
# 1. Create a session
SESSION_ID=$(mctl sessions create \
  --agent claude \
  --agent-api-key $ANTHROPIC_API_KEY \
  --name "api-project" \
  -o json | jq -r '.id')

# 2. Submit a task and follow logs
TASK_ID=$(mctl tasks create \
  --session $SESSION_ID \
  --prompt "Create a Go REST API with CRUD endpoints" \
  -o json | jq -r '.id')

# 3. Follow logs
mctl tasks logs $TASK_ID --follow

# 4. Handle permissions as they come
mctl permissions list --session $SESSION_ID --status pending
mctl permissions approve $PERM_ID

# 5. Continue with more work
mctl tasks create \
  --session $SESSION_ID \
  --prompt "Add JWT authentication"

# 6. Suspend when done for now
mctl sessions suspend $SESSION_ID

# 7. Resume later
mctl sessions resume $SESSION_ID
```

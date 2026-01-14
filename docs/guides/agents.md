# Agents

Marionette supports multiple AI coding agents through a plugin architecture.

## Supported Agents

| Agent | Provider | Description |
|-------|----------|-------------|
| `claude` | Anthropic | Claude Code (recommended) |
| `codex` | OpenAI | OpenAI Codex |
| `custom` | Custom | Custom agent binary |

## Claude Code

The default and recommended agent.

### Configuration

```bash
# Using BYOK (Bring Your Own Key)
mctl sessions create \
  --agent claude \
  --api-key $ANTHROPIC_API_KEY

# Using managed config
mctl sessions create \
  --agent claude \
  --agent-config claude-prod
```

### Features

- Full Claude Code capabilities
- Tool use (file editing, shell commands, browsing)
- Permission request handling
- Context preservation across suspend/resume

## OpenAI Codex

### Configuration

```bash
mctl sessions create \
  --agent codex \
  --api-key $OPENAI_API_KEY
```

## Custom Agents

You can integrate custom agents by implementing the agent protocol.

### Agent Protocol

The agent binary receives tasks via stdin and outputs to stdout/stderr:

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Agent Protocol                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   marionette-agent          Agent Binary                            │
│   ───────────────          ─────────────                            │
│         │                        │                                  │
│         │───── Task JSON ───────►│                                  │
│         │      (stdin)           │                                  │
│         │                        │                                  │
│         │◄──── Logs ─────────────│                                  │
│         │      (stdout/stderr)   │                                  │
│         │                        │                                  │
│         │◄──── Permission Req ───│                                  │
│         │      (stdout, JSON)    │                                  │
│         │                        │                                  │
│         │───── Permission Resp ──►│                                  │
│         │      (stdin, JSON)     │                                  │
│         │                        │                                  │
│         │◄──── Result ───────────│                                  │
│         │      (exit code)       │                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Custom Agent Example

```bash
#!/bin/bash
# custom-agent.sh

# Read task from stdin
read -r task_json

prompt=$(echo "$task_json" | jq -r '.prompt')

echo "Starting work on: $prompt"

# Do work...
echo '{"type": "permission_request", "tool": "bash", "action": "npm install"}'

# Read permission response
read -r permission_response

approved=$(echo "$permission_response" | jq -r '.approved')

if [ "$approved" = "true" ]; then
    npm install
fi

echo "Task completed"
exit 0
```

### Registering Custom Agent

```yaml
agents:
  custom:
    binary: "/usr/local/bin/custom-agent"
    env:
      - "CUSTOM_VAR=value"
```

## Agent Configuration Management

### Create Managed Config

Store API keys securely (encrypted at rest):

```bash
mctl admin agent-configs create \
  --name "claude-prod" \
  --agent claude \
  --api-key $ANTHROPIC_API_KEY \
  --model "claude-sonnet-4-20250514"
```

### Use Managed Config

```bash
mctl sessions create \
  --agent claude \
  --agent-config claude-prod
```

### BYOK Mode

Keys passed directly, never stored:

```bash
mctl sessions create \
  --agent claude \
  --api-key $ANTHROPIC_API_KEY \
  --byok  # Explicit BYOK mode
```

## Agent Capabilities

| Capability | Claude | Codex | Custom |
|------------|--------|-------|--------|
| Code generation | ✓ | ✓ | Depends |
| File editing | ✓ | ✓ | Depends |
| Shell commands | ✓ | Limited | Depends |
| Web browsing | ✓ | ✗ | Depends |
| Context memory | ✓ | ✓ | Depends |
| Permission flow | ✓ | ✓ | Required |

## Next Steps

- [Security Guide](security.md) - Agent credential security
- [CLI Reference](cli-reference.md) - Agent-related commands

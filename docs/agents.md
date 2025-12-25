# Agent Plugin Architecture

Marionette supports multiple AI coding agents (Claude Code, Codex, Gemini CLI, etc.) through a plugin-based architecture that decouples the orchestration layer from agent-specific implementations.

## Design Goals

1. **Loose Coupling**: Core server has no compile-time dependency on agent implementations
2. **Runtime Discovery**: New agents can be added without rebuilding the server
3. **Isolation**: Agent failures don't crash the runner process
4. **Extensibility**: Third parties can develop custom agent plugins

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Runner Process                            │
│                                                                     │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                      Agent Manager                             │ │
│  │                                                                │ │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │ │
│  │  │ AgentSpec   │  │ AgentSpec   │  │ AgentSpec   │             │ │
│  │  │ (claude)    │  │ (codex)     │  │ (gemini)    │             │ │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘             │ │
│  └─────────┼────────────────┼────────────────┼────────────────────┘ │
│            │                │                │                      │
│            ▼                ▼                ▼                      │
│  ┌─────────────────────────────────────────────────────────────────┐│
│  │                    Process Supervisor                           ││
│  │                                                                 ││
│  │    ┌─────────────────┐  ┌─────────────────┐                     ││
│  │    │  claude-code    │  │  gemini         │                     ││
│  │    │  (subprocess)   │  │  (subprocess)   │                     ││
│  │    │                 │  │                 │                     ││
│  │    │  stdin/stdout   │  │  stdin/stdout   │                     ││
│  │    └─────────────────┘  └─────────────────┘                     ││
│  └─────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────┘
```

## Plugin Interface

Agents are defined through YAML specifications that describe how to launch and interact with them:

```yaml
# /etc/marionette/agents/claude.yaml

# Spec format version (required for compatibility checking)
# Used to detect and migrate old spec files when format changes
spec_version: "1"

name: claude
version: "1.0"
description: "Claude Code - Anthropic's AI coding agent"

# How to launch the agent
launch:
  command: "claude"
  args:
    - "--print"
    - "--output-format=stream-json"

  # Conditional args based on sandbox_mode
  # Operator declares sandbox_mode - we trust that configuration
  conditional_args:
    runner-is-sandbox:
      - "--dangerously-skip-permissions"
    runner-creates-sandbox:
      - "--dangerously-skip-permissions"
    none:
      - "--permission-mode=bypasstool"

  env:
    ANTHROPIC_API_KEY: "{{ .APIKey }}"
    ANTHROPIC_MODEL: "{{ .Model | default \"claude-sonnet-4-20250514\" }}"
  workdir: "{{ .WorkspaceDir }}"

# Stdin/stdout protocol
protocol:
  type: stream-json    # Options: stream-json, jsonl, text
  input:
    format: text       # Prompt is sent as plain text to stdin
  output:
    format: jsonl      # Each line is a JSON object
    events:
      - type: content
        json_path: "$.message.content"
      - type: tool_use
        json_path: "$.tool_use"
      - type: done
        json_path: "$.type"
        value: "result"

# Required capabilities
requires:
  - network           # Needs network access
  - filesystem        # Needs /workspace access

# Health check
health:
  command: "claude --version"
  interval: 30s
  timeout: 5s

# Resource limits (applied per-task)
resources:
  memory_mb: 2048
  cpu_shares: 1024
  timeout: 3600s
```

## Agent Manager

The Agent Manager handles agent lifecycle:

```go
// pkg/agent/manager.go
package agent

import (
    "context"
    "io"
)

// Current spec format version
const CurrentSpecVersion = "1"

// Spec defines an agent plugin specification
type Spec struct {
    SpecVersion string `yaml:"spec_version"` // Spec format version for compatibility
    Name        string
    Version     string
    Description string
    Launch      LaunchConfig
    Protocol    ProtocolConfig
    Requires    []string
    Health      HealthConfig
    Resources   ResourceConfig
}

// LaunchConfig defines how to start an agent
type LaunchConfig struct {
    Command string
    Args    []string
    Env     map[string]string
    Workdir string

    // Conditional arguments based on sandbox mode
    // Args are selected based on runner's sandbox configuration
    ConditionalArgs map[string][]string `yaml:"conditional_args"`
}

// SandboxMode determines which conditional args to use
// - "runner-is-sandbox": Runner itself is isolated (Docker, E2B, Firecracker)
// - "runner-creates-sandbox": Runner creates per-task sandbox (gVisor, namespace)
// - "none": No sandbox (bare metal pool without isolation)
type SandboxMode string

// Instance represents a running agent
type Instance struct {
    ID        string
    Spec      *Spec
    Process   *os.Process
    Stdin     io.WriteCloser
    Stdout    io.ReadCloser
    Stderr    io.ReadCloser
    StartedAt time.Time
}

// Manager handles agent lifecycle
type Manager struct {
    specs    map[string]*Spec
    specPath string
    mu       sync.RWMutex
}

// NewManager creates an agent manager
func NewManager(specPath string) (*Manager, error) {
    m := &Manager{
        specs:    make(map[string]*Spec),
        specPath: specPath,
    }
    return m, m.loadSpecs()
}

// loadSpecs discovers and loads agent specifications
func (m *Manager) loadSpecs() error {
    entries, err := os.ReadDir(m.specPath)
    if err != nil {
        return err
    }

    for _, entry := range entries {
        if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
            spec, err := m.loadSpec(filepath.Join(m.specPath, entry.Name()))
            if err != nil {
                return fmt.Errorf("loading %s: %w", entry.Name(), err)
            }

            // Validate and migrate spec version
            if err := m.validateSpecVersion(spec); err != nil {
                return fmt.Errorf("spec %s: %w", entry.Name(), err)
            }

            m.specs[spec.Name] = spec
        }
    }
    return nil
}

// validateSpecVersion checks spec compatibility and applies migrations
func (m *Manager) validateSpecVersion(spec *Spec) error {
    if spec.SpecVersion == "" {
        return fmt.Errorf("missing spec_version (current: %s)", CurrentSpecVersion)
    }

    specVer, err := strconv.Atoi(spec.SpecVersion)
    if err != nil {
        return fmt.Errorf("invalid spec_version: %s", spec.SpecVersion)
    }

    currentVer, _ := strconv.Atoi(CurrentSpecVersion)

    if specVer > currentVer {
        return fmt.Errorf("spec_version %d is newer than supported %d, please upgrade marionette-agent",
            specVer, currentVer)
    }

    // Apply migrations for older versions
    for v := specVer; v < currentVer; v++ {
        if migrateFn, ok := specMigrations[v]; ok {
            if err := migrateFn(spec); err != nil {
                return fmt.Errorf("migrating from v%d: %w", v, err)
            }
        }
    }

    return nil
}

// specMigrations maps spec version to migration function
var specMigrations = map[int]func(*Spec) error{
    // Example: migration from v0 to v1
    // 0: func(s *Spec) error {
    //     // Add default values for new fields
    //     return nil
    // },
}

// StartOptions for launching an agent
type StartOptions struct {
    APIKey       string
    Model        string
    WorkspaceDir string
    SandboxMode  string // "runner-is-sandbox", "runner-creates-sandbox", "none"
    Extra        map[string]string
}

// Start launches an agent instance for a task
func (m *Manager) Start(ctx context.Context, agentName string, opts StartOptions) (*Instance, error) {
    spec, ok := m.specs[agentName]
    if !ok {
        return nil, fmt.Errorf("unknown agent: %s", agentName)
    }

    // Render template variables
    command := renderTemplate(spec.Launch.Command, opts)
    args := renderTemplateList(spec.Launch.Args, opts)
    env := renderTemplateMap(spec.Launch.Env, opts)

    // Select conditional args based on sandbox_mode
    // Operator is responsible for correct configuration
    if conditionalArgs, ok := spec.Launch.ConditionalArgs[opts.SandboxMode]; ok {
        args = append(args, renderTemplateList(conditionalArgs, opts)...)
    }

    // Create subprocess
    cmd := exec.CommandContext(ctx, command, args...)
    cmd.Dir = renderTemplate(spec.Launch.Workdir, opts)
    cmd.Env = envMapToSlice(env)

    stdin, _ := cmd.StdinPipe()
    stdout, _ := cmd.StdoutPipe()
    stderr, _ := cmd.StderrPipe()

    if err := cmd.Start(); err != nil {
        return nil, fmt.Errorf("starting agent: %w", err)
    }

    return &Instance{
        ID:        id.New("ainst"),
        Spec:      spec,
        Process:   cmd.Process,
        Stdin:     stdin,
        Stdout:    stdout,
        Stderr:    stderr,
        StartedAt: time.Now(),
    }, nil
}

// SendPrompt sends a prompt to the agent
func (m *Manager) SendPrompt(inst *Instance, prompt string) error {
    _, err := io.WriteString(inst.Stdin, prompt+"\n")
    return err
}
```

## Template Security

Agent specs use Go templates for variable substitution. To prevent template injection attacks:

### Allowed Template Variables

Only these variables are available in templates (whitelist approach):

| Variable | Source | Description |
|----------|--------|-------------|
| `.APIKey` | AgentConfig (encrypted) | Decrypted API key |
| `.Model` | AgentConfig | Model name |
| `.BaseURL` | AgentConfig | Custom API endpoint |
| `.WorkspaceDir` | Session | Workspace path |
| `.SandboxMode` | Runner | Sandbox configuration |
| `.Extra.*` | AgentConfig.extra | Additional key-value pairs |

### Safe Template Rendering

```go
// pkg/agent/template.go
package agent

import (
    "bytes"
    "fmt"
    "regexp"
    "strings"
    "text/template"
)

// AllowedVariables defines the whitelist of template variables
var AllowedVariables = map[string]bool{
    "APIKey":       true,
    "Model":        true,
    "BaseURL":      true,
    "WorkspaceDir": true,
    "SandboxMode":  true,
    "Extra":        true,
}

// TemplateData is the only data passed to templates
// Fields are explicitly defined - no arbitrary map access
type TemplateData struct {
    APIKey       string
    Model        string
    BaseURL      string
    WorkspaceDir string
    SandboxMode  string
    Extra        map[string]string
}

// validateTemplate checks template for disallowed patterns
func validateTemplate(tmplStr string) error {
    // Disallow function calls except 'default'
    // Pattern: {{ .Foo | bar }} where bar is not 'default'
    funcPattern := regexp.MustCompile(`\{\{[^}]*\|\s*([a-zA-Z]+)`)
    matches := funcPattern.FindAllStringSubmatch(tmplStr, -1)
    for _, match := range matches {
        if len(match) > 1 && match[1] != "default" {
            return fmt.Errorf("disallowed template function: %s", match[1])
        }
    }

    // Disallow nested templates, define, template, block
    dangerous := []string{"{{define", "{{template", "{{block", "{{-", "-}}"}
    for _, d := range dangerous {
        if strings.Contains(tmplStr, d) {
            return fmt.Errorf("disallowed template directive: %s", d)
        }
    }

    return nil
}

// safeTemplateFuncs only includes safe functions
var safeTemplateFuncs = template.FuncMap{
    "default": func(def, val interface{}) interface{} {
        if val == nil || val == "" {
            return def
        }
        return val
    },
}

// renderTemplate safely renders a template string
func renderTemplate(tmplStr string, data TemplateData) (string, error) {
    // Validate template before parsing
    if err := validateTemplate(tmplStr); err != nil {
        return "", fmt.Errorf("invalid template: %w", err)
    }

    // Parse with restricted function set
    tmpl, err := template.New("").Funcs(safeTemplateFuncs).Parse(tmplStr)
    if err != nil {
        return "", fmt.Errorf("parsing template: %w", err)
    }

    // Execute with typed data (not arbitrary map)
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return "", fmt.Errorf("executing template: %w", err)
    }

    return buf.String(), nil
}

// renderTemplateList renders a list of template strings
func renderTemplateList(templates []string, data TemplateData) ([]string, error) {
    result := make([]string, 0, len(templates))
    for _, t := range templates {
        rendered, err := renderTemplate(t, data)
        if err != nil {
            return nil, err
        }
        result = append(result, rendered)
    }
    return result, nil
}

// renderTemplateMap renders a map of template strings
func renderTemplateMap(templates map[string]string, data TemplateData) (map[string]string, error) {
    result := make(map[string]string, len(templates))
    for k, v := range templates {
        rendered, err := renderTemplate(v, data)
        if err != nil {
            return nil, err
        }
        result[k] = rendered
    }
    return result, nil
}
```

### Spec Validation on Load

```go
// validateSpec performs security checks on agent spec
func (m *Manager) validateSpec(spec *Spec) error {
    // 1. Validate all templates in launch config
    templates := []string{
        spec.Launch.Command,
        spec.Launch.Workdir,
    }
    templates = append(templates, spec.Launch.Args...)
    for _, v := range spec.Launch.Env {
        templates = append(templates, v)
    }
    for _, args := range spec.Launch.ConditionalArgs {
        templates = append(templates, args...)
    }

    for _, t := range templates {
        if err := validateTemplate(t); err != nil {
            return fmt.Errorf("invalid template in spec: %w", err)
        }
    }

    // 2. Validate command is not a shell
    if isShellCommand(spec.Launch.Command) {
        return fmt.Errorf("shell commands not allowed: %s", spec.Launch.Command)
    }

    // 3. Validate no shell metacharacters in args
    for _, arg := range spec.Launch.Args {
        if containsShellMeta(arg) {
            return fmt.Errorf("shell metacharacters not allowed in args: %s", arg)
        }
    }

    return nil
}

func isShellCommand(cmd string) bool {
    shells := []string{"sh", "bash", "zsh", "fish", "csh", "tcsh", "ksh", "dash"}
    base := filepath.Base(cmd)
    for _, shell := range shells {
        if base == shell {
            return true
        }
    }
    return false
}

func containsShellMeta(s string) bool {
    // Only check static parts (not template variables)
    // Template variables are validated separately
    static := regexp.MustCompile(`\{\{[^}]+\}\}`).ReplaceAllString(s, "")
    meta := []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "<", ">", "\n"}
    for _, m := range meta {
        if strings.Contains(static, m) {
            return true
        }
    }
    return false
}
```

### Security Guarantees

| Attack Vector | Protection |
|---------------|------------|
| Template injection via API key | API key is from encrypted config, not user input |
| Template injection via model name | Model validated against allowlist |
| Template injection via extra fields | Only alphanumeric keys/values allowed |
| Shell injection via command | Shell commands disallowed |
| Shell injection via args | Shell metacharacters disallowed |
| Arbitrary template functions | Only `default` function allowed |
| Template directives (define/block) | Explicitly disallowed |

```go

// StreamOutput reads agent output and emits events
func (m *Manager) StreamOutput(inst *Instance, events chan<- Event) error {
    parser := NewProtocolParser(inst.Spec.Protocol)
    scanner := bufio.NewScanner(inst.Stdout)

    for scanner.Scan() {
        event, err := parser.Parse(scanner.Bytes())
        if err != nil {
            continue // Skip malformed lines
        }
        events <- event
    }
    return scanner.Err()
}
```

## Protocol Parsers

Each protocol type has a dedicated parser:

```go
// pkg/agent/protocol.go
package agent

// Event types emitted by agents
type EventType string

const (
    EventContent    EventType = "content"
    EventToolUse    EventType = "tool_use"
    EventToolResult EventType = "tool_result"
    EventDone       EventType = "done"
    EventError      EventType = "error"
)

type Event struct {
    Type      EventType
    Content   string
    ToolUse   *ToolUse
    Error     error
    Timestamp time.Time
}

type ToolUse struct {
    ID        string
    Name      string
    Arguments json.RawMessage
}

// ProtocolParser parses agent output
type ProtocolParser interface {
    Parse(line []byte) (Event, error)
}

// StreamJSONParser for Claude Code's stream-json format
type StreamJSONParser struct {
    config ProtocolConfig
}

func (p *StreamJSONParser) Parse(line []byte) (Event, error) {
    var msg map[string]interface{}
    if err := json.Unmarshal(line, &msg); err != nil {
        return Event{}, err
    }

    // Check each event type mapping
    for _, eventDef := range p.config.Output.Events {
        if val := jsonPath(msg, eventDef.JSONPath); val != nil {
            if eventDef.Value == "" || val == eventDef.Value {
                return Event{
                    Type:      EventType(eventDef.Type),
                    Content:   fmt.Sprint(val),
                    Timestamp: time.Now(),
                }, nil
            }
        }
    }

    return Event{}, fmt.Errorf("no matching event type")
}
```

## Built-in Agent Specs

### Claude Code

```yaml
# agents/claude.yaml
spec_version: "1"
name: claude
version: "1.0"
description: "Claude Code - Anthropic's AI coding agent"

launch:
  command: "claude"

  # Base args (always included)
  args:
    - "--print"
    - "--output-format=stream-json"

  # Conditional args based on sandbox_mode
  # Operator declares sandbox_mode - we trust that configuration
  conditional_args:
    runner-is-sandbox:
      - "--dangerously-skip-permissions"
    runner-creates-sandbox:
      - "--dangerously-skip-permissions"
    none:
      - "--permission-mode=bypasstool"

  env:
    ANTHROPIC_API_KEY: "{{ .APIKey }}"
    CLAUDE_CODE_MODEL: "{{ .Model }}"
  workdir: "{{ .WorkspaceDir }}"

protocol:
  type: stream-json
  input:
    format: text
  output:
    format: jsonl
    events:
      - type: content
        json_path: "$.content"
      - type: tool_use
        json_path: "$.tool_use"
      - type: permission_request
        json_path: "$.permission_request"
      - type: done
        json_path: "$.type"
        value: "result"

requires:
  - network
  - filesystem

resources:
  memory_mb: 2048
  timeout: 3600s
```

**Sandbox Mode Selection Logic:**

```
┌─────────────────────────────────────────────────────────────────────┐
│                  Permission Handling by Sandbox Mode                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Sandbox Mode            Agent Args               Permission Flow   │
│  ─────────────────────────────────────────────────────────────────  │
│                                                                     │
│  runner-is-sandbox       --dangerously-skip-     All tools auto-    │
│  (Docker, E2B, etc.)     permissions             approved           │
│                                                                     │
│  runner-creates-sandbox  --dangerously-skip-     All tools auto-    │
│  (gVisor, namespace)     permissions             approved           │
│                                                                     │
│  none                    --permission-mode=      Agent asks →       │
│  (bare metal pool)       bypasstool              Marionette →       │
│                                                   Server → User     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

DESIGN RATIONALE:

- sandbox_mode is the operator's explicit declaration of the isolation level
- If operator declares "runner-is-sandbox", they are responsible for ensuring
  actual isolation (Docker, E2B, Firecracker, etc.)
- Misconfiguration (e.g., claiming sandbox but having none) is an operator
  error, not something the system should second-guess
- This keeps the configuration simple and the responsibility clear
```

### OpenAI Codex

```yaml
# agents/codex.yaml
spec_version: "1"
name: codex
version: "1.0"
description: "OpenAI Codex CLI agent"

launch:
  command: "codex"
  args:
    - "--quiet"

  # Conditional args based on sandbox_mode
  conditional_args:
    runner-is-sandbox:
      - "--approval-mode=full-auto"
    runner-creates-sandbox:
      - "--approval-mode=full-auto"
    none:
      - "--approval-mode=suggest"

  env:
    OPENAI_API_KEY: "{{ .APIKey }}"
    OPENAI_MODEL: "{{ .Model | default \"o3-mini\" }}"
  workdir: "{{ .WorkspaceDir }}"

protocol:
  type: jsonl
  input:
    format: json
    template: |
      {"prompt": "{{ .Prompt }}"}
  output:
    format: jsonl
    events:
      - type: content
        json_path: "$.content"
      - type: done
        json_path: "$.status"
        value: "complete"

requires:
  - network
  - filesystem

resources:
  memory_mb: 1024
  timeout: 3600s
```

### Gemini CLI

```yaml
# agents/gemini.yaml
spec_version: "1"
name: gemini
version: "1.0"
description: "Gemini CLI - Google's AI coding agent"

launch:
  command: "gemini"
  args:
    - "--sandbox=permissive"
    - "--output=json"
  env:
    GEMINI_API_KEY: "{{ .APIKey }}"
    GEMINI_MODEL: "{{ .Model | default \"gemini-2.5-pro\" }}"
  workdir: "{{ .WorkspaceDir }}"

protocol:
  type: stream-json
  input:
    format: text
  output:
    format: jsonl
    events:
      - type: content
        json_path: "$.content"
      - type: tool_use
        json_path: "$.tool_call"
      - type: done
        json_path: "$.done"
        value: true

requires:
  - network
  - filesystem

resources:
  memory_mb: 2048
  timeout: 3600s
```

## Process Supervision

The runner uses a process supervisor to manage agent lifecycles:

```go
// pkg/agent/supervisor.go
package agent

import (
    "context"
    "os/exec"
    "syscall"
    "time"
)

// Supervisor manages agent process lifecycle
type Supervisor struct {
    instances map[string]*Instance
    mu        sync.RWMutex
}

// Monitor watches an agent process
func (s *Supervisor) Monitor(inst *Instance) {
    done := make(chan error, 1)

    go func() {
        done <- inst.cmd.Wait()
    }()

    select {
    case err := <-done:
        s.handleExit(inst, err)
    case <-inst.ctx.Done():
        s.terminate(inst)
    }
}

// terminate gracefully stops an agent
func (s *Supervisor) terminate(inst *Instance) {
    // Send SIGTERM first
    inst.Process.Signal(syscall.SIGTERM)

    // Wait up to 10s for graceful shutdown
    timer := time.NewTimer(10 * time.Second)
    done := make(chan struct{})

    go func() {
        inst.cmd.Wait()
        close(done)
    }()

    select {
    case <-done:
        timer.Stop()
    case <-timer.C:
        // Force kill after timeout
        inst.Process.Signal(syscall.SIGKILL)
    }
}

// handleExit processes agent exit
func (s *Supervisor) handleExit(inst *Instance, err error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    delete(s.instances, inst.ID)

    if err != nil {
        // Log non-zero exit for debugging
        log.Printf("agent %s exited with error: %v", inst.ID, err)
    }
}
```

## Custom Agent Development

Third parties can develop custom agents by:

1. Creating an executable that accepts prompts on stdin
2. Outputting events in a supported format (stream-json, jsonl, text)
3. Writing a YAML spec file

Example custom agent structure:

```
my-custom-agent/
├── bin/
│   └── my-agent          # Executable
├── spec.yaml             # Agent spec
└── README.md
```

Installation:

```bash
# Copy spec to agent specs directory
cp spec.yaml /etc/marionette/agents/my-agent.yaml

# Ensure binary is in PATH or use absolute path in spec
sudo cp bin/my-agent /usr/local/bin/
```

## Security Considerations

1. **Process Isolation**: Each agent runs as a separate process with its own memory space
2. **Credential Injection**: API keys are injected via environment variables, never stored in specs
3. **Resource Limits**: Memory and CPU limits prevent runaway agents
4. **Network Policy**: Agents inherit the session's network policy
5. **Filesystem Sandbox**: Agents only have access to the workspace directory

## Permission Audit Trail

Even when permissions are auto-approved (sandbox modes), ALL agent actions are logged for audit purposes.

### Audit Logging Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                   Permission & Action Audit Trail                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Sandbox Mode                    Permission Flow                    │
│  ─────────────────────────────────────────────────────────────────  │
│                                                                     │
│  runner-is-sandbox:              Agent action                       │
│  --dangerously-skip-permissions       │                             │
│                                       ▼                             │
│                               ┌───────────────┐                     │
│                               │ Auto-approved │                     │
│                               │ (no user wait)│                     │
│                               └───────┬───────┘                     │
│                                       │                             │
│                                       ▼                             │
│                               ┌───────────────┐                     │
│                               │  AUDIT LOG    │ ◄─── Still logged!  │
│                               │  action_logs  │                     │
│                               └───────────────┘                     │
│                                                                     │
│  none (no sandbox):           Agent action                          │
│  --permission-mode=bypasstool       │                               │
│                                     ▼                               │
│                             ┌───────────────┐                       │
│                             │ Permission    │                       │
│                             │ Request to    │                       │
│                             │ Server        │                       │
│                             └───────┬───────┘                       │
│                                     ▼                               │
│                             ┌───────────────┐                       │
│                             │ User Approval │                       │
│                             │ Required      │                       │
│                             └───────┬───────┘                       │
│                                     ▼                               │
│                             ┌───────────────┐                       │
│                             │  AUDIT LOG    │                       │
│                             │  + decision   │                       │
│                             └───────────────┘                       │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Action Log Integration

Agent tool invocations are recorded using the general `action_logs` table (see schema.sql).
Tool-specific details are stored in the `details` JSONB field.

```go
// pkg/agent/audit.go

// LogToolUse records an agent tool invocation to the action_logs table
func (a *AuditLogger) LogToolUse(ctx context.Context, audit ActionAudit) error {
    return a.store.CreateActionLog(ctx, &ActionLog{
        ActorType:    "runner",
        ActorID:      audit.RunnerID,
        ActorName:    audit.RunnerName,
        Action:       "tool." + audit.Tool, // e.g., "tool.bash", "tool.edit"
        ResourceType: "task_run",
        ResourceID:   audit.RunID,
        SessionID:    &audit.SessionID,
        TaskID:       &audit.TaskID,
        Details: map[string]interface{}{
            "tool":                 audit.Tool,
            "command":              audit.Action,
            "arguments":            audit.Arguments,
            "permission_mode":      audit.PermissionMode,
            "auto_approved_reason": audit.AutoApprovedReason,
            "exit_code":            audit.ExitCode,
            "execution_time_ms":    audit.ExecutionTimeMs,
        },
        Success:      audit.Success,
        ErrorMessage: audit.Error,
        TenantID:     audit.TenantID,
    })
}
```

**Action naming convention for tools:**
- `tool.bash` - Shell command execution
- `tool.edit` - File edit operation
- `tool.write` - File write operation
- `tool.read` - File read operation
- `tool.browser` - Browser automation

**Query examples:**
```sql
-- All bash commands in a session
SELECT action, details->>'command' as command, created_at
FROM action_logs
WHERE session_id = 'sess_xxx'
  AND action = 'tool.bash'
ORDER BY created_at;

-- Actions auto-approved in sandbox mode
SELECT COUNT(*), action, details->>'auto_approved_reason' as reason
FROM action_logs
WHERE tenant_id = 'tenant_xxx'
  AND details->>'permission_mode' = 'auto_approved'
  AND created_at > NOW() - INTERVAL '7 days'
GROUP BY action, reason;

-- High-risk commands (even if auto-approved)
SELECT *
FROM action_logs
WHERE action = 'tool.bash'
  AND (details->>'command' LIKE '%rm -rf%'
       OR details->>'command' LIKE '%sudo%'
       OR details->>'command' LIKE '%curl%|%sh%')
ORDER BY created_at DESC
LIMIT 100;
```

### Runner-Side Logging

```go
// pkg/agent/audit.go

// ActionAudit contains data for tool invocation audit
type ActionAudit struct {
    // Context
    SessionID   string
    TaskID      string
    RunID       string
    RunnerID    string
    RunnerName  string
    TenantID    string

    // Tool details
    Tool        string           // "bash", "edit", "write", etc.
    Action      string           // command or description
    Arguments   json.RawMessage  // tool-specific args

    // Permission tracking
    PermissionMode     string   // "auto_approved", "user_approved", "user_denied"
    AutoApprovedReason string   // "sandbox_mode", "allowlist", etc.

    // Outcome
    Success         bool
    Error           string
    ExitCode        int
    ExecutionTimeMs int64
}

// Wrapper for agent output parsing
func (m *Manager) StreamOutput(inst *Instance, events chan<- Event, auditor *AuditLogger) error {
    parser := NewProtocolParser(inst.Spec.Protocol)
    scanner := bufio.NewScanner(inst.Stdout)

    for scanner.Scan() {
        event, err := parser.Parse(scanner.Bytes())
        if err != nil {
            continue
        }

        // Log tool_use events to audit trail
        if event.Type == EventToolUse {
            auditor.LogToolUse(inst.ctx, ActionAudit{
                SessionID:          inst.SessionID,
                TaskID:             inst.TaskID,
                RunID:              inst.RunID,
                RunnerID:           inst.RunnerID,
                RunnerName:         inst.RunnerName,
                TenantID:           inst.TenantID,
                Tool:               event.ToolUse.Name,
                Action:             extractAction(event.ToolUse),
                Arguments:          event.ToolUse.Arguments,
                PermissionMode:     inst.PermissionMode,
                AutoApprovedReason: inst.SandboxMode,
            })
        }

        events <- event
    }
    return scanner.Err()
}
```

### Compliance Benefits

| Sandbox Mode | User Wait | Audit Logged | Searchable | Alertable |
|--------------|-----------|--------------|------------|-----------|
| runner-is-sandbox | No | ✓ Yes | ✓ Yes | ✓ Yes |
| runner-creates-sandbox | No | ✓ Yes | ✓ Yes | ✓ Yes |
| none | Yes | ✓ Yes | ✓ Yes | ✓ Yes |

**Key Point**: Skipping user approval for performance does NOT skip audit logging.
All actions are recorded for compliance, debugging, and security analysis.

## Future Enhancements

1. **WASM Support**: Run lightweight agents in a WASM sandbox for stronger isolation
2. **Container Plugins**: Support agents distributed as OCI containers
3. **Hot Reload**: Reload agent specs without restarting the runner
4. **Metrics**: Per-agent resource usage and performance metrics

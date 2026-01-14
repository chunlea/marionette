# Marionette

**Remote agent orchestration and observability platform for AI coding agents.**

Marionette enables you to deploy, manage, and observe AI coding agents (like Claude Code, Codex, etc.) running in isolated environments with enterprise-grade security and scalability.

<div class="grid cards" markdown>

-   :material-clock-fast:{ .lg .middle } __Quick Start__

    ---

    Get up and running with Marionette in under 5 minutes

    [:octicons-arrow-right-24: Getting Started](getting-started/installation.md)

-   :material-book-open-variant:{ .lg .middle } __Core Concepts__

    ---

    Understand the architecture, sessions, tasks, and providers

    [:octicons-arrow-right-24: Concepts](concepts/architecture.md)

-   :material-cog:{ .lg .middle } __Guides__

    ---

    CLI reference, API documentation, and integration guides

    [:octicons-arrow-right-24: Guides](guides/cli-reference.md)

-   :material-github:{ .lg .middle } __Open Source__

    ---

    MIT licensed, open source, and community-driven

    [:octicons-arrow-right-24: GitHub](https://github.com/chunlea/marionette)

</div>

## Features

- **Multi-Agent Support** - Run Claude Code, Codex, or custom AI coding agents
- **Flexible Deployment** - Docker, Kubernetes, E2B, Firecracker, or bare metal pools
- **Session Management** - Long-lived work contexts with suspend/resume capabilities
- **Workspace Persistence** - Encrypted storage with content-addressable deduplication
- **Real-time Streaming** - Live logs, permission requests, and progress updates
- **Port Forwarding** - HTTP tunnels, desktop streaming (WebRTC), mobile emulators
- **Multi-tenant** - Built for SaaS integration with tenant isolation
- **Observability** - Prometheus metrics, structured logging, OpenTelemetry tracing

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Server (Go)                               │
│  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐    │
│  │ SessionMgr  │ │  TaskMgr    │ │ RunnerMgr   │ │ TunnelMgr   │    │
│  └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘    │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Provider Registry (Docker, K8s, E2B, Pool, ...)            │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                              │                                      │
│  :9090 gRPC  │  :8080 Public API  │  :8081 Admin UI                 │
└──────────────┼────────────────────┼─────────────────────────────────┘
               │                    │
               ▼                    ▼
┌──────────────────────┐    ┌──────────────────────┐
│  Runner (isolated)   │    │  Runner (pool)       │
│  ┌────────────────┐  │    │  ┌────────────────┐  │
│  │marionette-agent│  │    │  │marionette-agent│  │
│  │ ┌────────────┐ │  │    │  └───────┬────────┘  │
│  │ │Claude Code │ │  │    │          │           │
│  │ └────────────┘ │  │    │  ┌───────▼────────┐  │
│  │ ┌────────────┐ │  │    │  │Sandbox (gVisor)│  │
│  │ │ /workspace │ │  │    │  │ Agent+Workspace│  │
│  │ └────────────┘ │  │    │  └────────────────┘  │
│  └────────────────┘  │    └──────────────────────┘
└──────────────────────┘
```

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Runner** | Execution environment (container, VM, or machine) |
| **Session** | Long-lived work context binding Runner + Workspace |
| **Task** | Unit of work (prompt) executed within a Session |
| **Workspace** | Persistent `/workspace` directory |
| **Agent** | AI coding agent (Claude Code, Codex, etc.) |

## Quick Example

=== "CLI"

    ```bash
    # Create a session with Claude Code
    mctl sessions create --agent claude --api-key $ANTHROPIC_API_KEY

    # Submit a task
    mctl tasks create --session $SESSION_ID --prompt "Build a REST API"

    # Follow the logs
    mctl tasks logs --follow $TASK_ID
    ```

=== "API"

    ```bash
    # Create a session
    curl -X POST http://localhost:8080/api/v1/sessions \
      -H "Authorization: Bearer $API_KEY" \
      -H "Content-Type: application/json" \
      -d '{"agent": "claude", "api_key": "sk-ant-xxx"}'

    # Submit a task
    curl -X POST http://localhost:8080/api/v1/sessions/$SESSION_ID/tasks \
      -H "Authorization: Bearer $API_KEY" \
      -d '{"prompt": "Build a REST API"}'
    ```

=== "Docker Compose"

    ```bash
    # Start everything
    docker compose up -d

    # Check status
    docker compose ps
    ```

## Next Steps

<div class="grid cards" markdown>

-   [:octicons-download-24: __Installation__](getting-started/installation.md)

    Install Marionette using Docker, Kubernetes, or from source

-   [:octicons-rocket-24: __Quick Start__](getting-started/quick-start.md)

    Run your first AI coding agent in 5 minutes

-   [:octicons-gear-24: __Configuration__](getting-started/configuration.md)

    Configure providers, storage, and security settings

-   [:octicons-book-24: __Architecture__](concepts/architecture.md)

    Deep dive into how Marionette works

</div>

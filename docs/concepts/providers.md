# Providers

Providers manage the lifecycle of Runners (execution environments).

## Provider Types

Marionette supports three provider modes:

| Mode | Description | Examples |
|------|-------------|----------|
| **Managed** | Server controls full lifecycle | Docker, Kubernetes, E2B, Firecracker |
| **Pool** | Runners join a pool, server assigns work | macOS Pool, GPU Pool, Bare Metal |
| **External** | Manual one-off registration | Self-hosted runners |

=== "Diagram"

    ```mermaid
    flowchart TB
        subgraph Managed["Managed (server controls lifecycle)"]
            Docker
            K8s[Kubernetes]
            E2B
            FC[Firecracker]
        end

        subgraph Pool["Pool (runners join, server assigns)"]
            macOS[macOS Pool]
            GPU[GPU Pool]
            Metal[Metal Pool]
        end

        subgraph External["External (manual registration)"]
            Self[Self-hosted runners]
        end

        Server[Marionette Server] --> Managed
        Server --> Pool
        Server --> External
    ```

=== "Text"

    ```
    ┌─────────────────────────────────────────────────────────────────────┐
    │                      Provider Types                                 │
    ├─────────────────────────────────────────────────────────────────────┤
    │                                                                     │
    │  Managed (lifecycle controlled by server)                           │
    │  ┌────────┐ ┌────────────┐ ┌─────┐ ┌───────────┐                    │
    │  │ Docker │ │ Kubernetes │ │ E2B │ │Firecracker│                    │
    │  └────────┘ └────────────┘ └─────┘ └───────────┘                    │
    │                                                                     │
    │  Pool (runners join, server assigns)                                │
    │  ┌─────────────┐ ┌─────────────┐ ┌─────────────┐                    │
    │  │ macOS Pool  │ │  GPU Pool   │ │ Metal Pool  │                    │
    │  └─────────────┘ └─────────────┘ └─────────────┘                    │
    │                                                                     │
    │  External (manual registration)                                     │
    │  ┌─────────────────────────────────────────────┐                    │
    │  │         Self-hosted runners                 │                    │
    │  └─────────────────────────────────────────────┘                    │
    │                                                                     │
    └─────────────────────────────────────────────────────────────────────┘
    ```

## Managed Providers

### Docker

Best for local development and single-machine deployments.

```yaml
providers:
  docker:
    host: "unix:///var/run/docker.sock"
    image: "marionette/agent:latest"
    network: "marionette-network"
    resources:
      memory: "4g"
      cpus: "2"
```

### Kubernetes

Best for production deployments with auto-scaling.

```yaml
providers:
  kubernetes:
    namespace: "marionette"
    image: "marionette/agent:latest"
    resources:
      requests:
        memory: "2Gi"
        cpu: "1"
      limits:
        memory: "8Gi"
        cpu: "4"
```

### E2B

Cloud sandboxes with fast cold start.

```yaml
providers:
  e2b:
    api_key: "${E2B_API_KEY}"
    template: "marionette-agent"
    timeout: 3600
```

## Pool Providers

Pool providers allow pre-provisioned machines to join and receive work.

### How Pools Work

=== "Diagram"

    ```mermaid
    flowchart LR
        subgraph Server
            Matcher
            Assigner
            Matcher --> Assigner
        end

        subgraph macOS["Pool: macos"]
            R1["Runner1<br/>idle"]
            R2["Runner2<br/>busy"]
            R3["Runner3<br/>idle"]
        end

        subgraph GPU["Pool: gpu"]
            R4["Runner4<br/>idle"]
            R5["Runner5<br/>idle"]
        end

        macOS <--> Matcher
        GPU <--> Assigner
    ```

=== "Text"

    ```
    ┌──────────────────────────────────────────────────────────────────┐
    │                        Pool Provider                             │
    ├──────────────────────────────────────────────────────────────────┤
    │                                                                  │
    │  ┌─────────────────┐      ┌─────────────────────────────────┐    │
    │  │     Server      │      │           Pool: macos           │    │
    │  │                 │      │  ┌───────┐ ┌───────┐ ┌───────┐  │    │
    │  │  ┌───────────┐  │      │  │Runner1│ │Runner2│ │Runner3│  │    │
    │  │  │  Matcher  │◄─┼──────┼──┤ idle  │ │ busy  │ │ idle  │  │    │
    │  │  └───────────┘  │      │  └───────┘ └───────┘ └───────┘  │    │
    │  │       │         │      └─────────────────────────────────┘    │
    │  │       ▼         │                                             │
    │  │  ┌───────────┐  │      ┌─────────────────────────────────┐    │
    │  │  │ Assigner  │──┼─────►│           Pool: gpu             │    │
    │  │  └───────────┘  │      │  ┌───────┐ ┌───────┐            │    │
    │  │                 │      │  │Runner4│ │Runner5│            │    │
    │  └─────────────────┘      │  │ idle  │ │ idle  │            │    │
    │                           │  └───────┘ └───────┘            │    │
    │                           └─────────────────────────────────┘    │
    └──────────────────────────────────────────────────────────────────┘
    ```

### Pool Configuration

```yaml
providers:
  pool:
    pools:
      macos:
        min_runners: 2
        max_runners: 10
        idle_timeout: 300
        selector:
          os: darwin
          arch: arm64
      gpu:
        min_runners: 1
        max_runners: 4
        selector:
          gpu: "nvidia"
          vram: ">=16GB"
```

### Runner Registration

Pool runners register with a token:

```bash
# On the pool machine
./bin/agent \
  --server marionette.example.com:9090 \
  --token $RUNNER_TOKEN \
  --pool macos \
  --labels os=darwin,arch=arm64
```

## Sandbox Modes

Two independent axes for configuring isolation:

### Runner Lifecycle

| Mode | Description |
|------|-------------|
| `per-session` | Runner reused across tasks (fast) |
| `per-task` | New runner per task (high isolation) |

### Sandbox Mode

| Mode | Description |
|------|-------------|
| `runner-is-sandbox` | Container/VM is the sandbox |
| `runner-creates-sandbox` | Runner creates nested sandbox (gVisor) |

### Common Configurations

| Configuration | Use Case |
|---------------|----------|
| per-session + runner-is-sandbox | Docker/E2B (default) |
| per-session + runner-creates-sandbox | macOS/GPU pools |
| per-task + runner-is-sandbox | Maximum isolation |

## Suspend Strategies

When a session suspends, the provider determines what happens to the runner:

| Strategy | Description | Providers |
|----------|-------------|-----------|
| `pause` | Freeze memory (CRIU) | Docker, Firecracker |
| `snapshot` | Save VM state | E2B, Firecracker |
| `terminate_preserve_storage` | Keep workspace only | All |
| `release_to_pool` | Return to pool | Pool providers |
| `terminate` | Full cleanup | All |

Configure per provider:

```yaml
providers:
  docker:
    suspend_config:
      strategy: pause
      fallback: terminate_preserve_storage
      min_duration: 60s
      max_duration: 24h
```

## Provider Selection

Sessions can request specific providers or capabilities:

```bash
# Request specific provider
mctl sessions create \
  --agent claude \
  --provider kubernetes

# Request by labels
mctl sessions create \
  --agent claude \
  --selector "gpu=nvidia,vram>=16GB"

# Use a profile
mctl sessions create \
  --agent claude \
  --profile gpu-large
```

## Next Steps

- [Workspaces](workspaces.md) - How workspace storage works
- [Storage Reference](../reference/storage.md) - Content-addressable storage details

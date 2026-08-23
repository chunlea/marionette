# Workspaces

Workspaces provide persistent storage for session files.

## Overview

Every session has a workspace mounted at `/workspace`. This directory:

- Persists across task executions
- Survives runner changes (suspend/resume)
- Can sync to object storage for cross-region mobility

!!! info "How much of this is wired"
    The workspace itself is real and persists. Content-addressable sync is
    **partly** wired: a runner configured with a storage backend chunks the
    workspace, stores a manifest, and restores it byte-for-byte on attach.
    What is missing is on the wire — the server does not yet tell the runner
    which workspace it holds or which snapshot to restore — so with a default
    configuration a suspend reports the workspace as **not** synced rather than
    implying a snapshot exists. Cross-region mobility follows from that work,
    and is not available today.

## Storage Types

| Type | Description | Use Case |
|------|-------------|----------|
| `volume` | Local or network volume | Single-region, fast access |
| `pvc` | Kubernetes PersistentVolumeClaim | Kubernetes deployments |
| `cas` | Content-Addressable Storage | Multi-region, deduplication |

## Content-Addressable Storage (CAS)

CAS provides efficient, deduplicated storage:

=== "Diagram"

    ```mermaid
    flowchart LR
        subgraph Workspace["/workspace"]
            src["src/"]
            main["main.go"]
            utils["utils.go"]
            mod["go.mod"]
            readme["README.md"]
            src --> main
            src --> utils
        end

        subgraph ChunkStore["Chunk Store (S3/Local)"]
            c1["abc123<br/>4KB"]
            c2["def456<br/>8KB"]
            c3["ghi789<br/>2KB"]
            c4["jkl012<br/>16KB"]
        end

        main --> c1
        utils --> c2
        mod --> c3
        readme --> c4
    ```

=== "Text"

    ```
    ┌─────────────────────────────────────────────────────────────────────┐
    │                    Content-Addressable Storage                      │
    ├─────────────────────────────────────────────────────────────────────┤
    │                                                                     │
    │   Workspace                    Chunks (deduplicated)                │
    │   ─────────                    ──────                               │
    │   /workspace/                  ┌─────────────────────────────┐      │
    │   ├── src/                     │ Chunk Store (S3/GCS/Local)  │      │
    │   │   ├── main.go ─────────────┤ ┌───────┐ ┌───────┐         │      │
    │   │   └── utils.go ────────────┤ │ abc123│ │ def456│         │      │
    │   ├── go.mod ──────────────────┤ │ (4KB) │ │ (8KB) │         │      │
    │   └── README.md ───────────────┤ └───────┘ └───────┘         │      │
    │                                │ ┌───────┐ ┌───────┐         │      │
    │   Manifest                     │ │ ghi789│ │ jkl012│         │      │
    │   ────────                     │ │ (2KB) │ │ (16KB)│         │      │
    │   {                            │ └───────┘ └───────┘         │      │
    │     "files": [                 └─────────────────────────────┘      │
    │       {"path": "src/main.go",                                       │
    │        "chunks": ["abc123"]},                                       │
    │       ...                                                           │
    │     ]                                                               │
    │   }                                                                 │
    │                                                                     │
    └─────────────────────────────────────────────────────────────────────┘
    ```

### Benefits

- **Deduplication**: Identical files/chunks stored once
- **Incremental Sync**: Only changed chunks transferred
- **Encryption**: Per-tenant encryption at rest
- **Integrity**: SHA-256 verification

## Workspace Mobility

Workspaces can be synced across regions/providers:

| Mobility | Description |
|----------|-------------|
| `local` | Pinned to specific runner/node |
| `shared` | Accessible from any runner in same region |
| `object_sync` | Synced to object storage (cross-region) |

### Example: Cross-Region Resume

!!! warning "Target design, not current behaviour"
    This is what `object_sync` mobility is for. See the note at the top of this
    page for what is wired today.

```
1. Session active in us-west-2
   └── Workspace on local volume

2. Session suspended
   └── Workspace synced to object storage

3. Session resumed in eu-west-1
   └── Workspace restored from object storage
```

## Configuration

### Local Storage

```yaml
storage:
  workspace:
    base_dir: "/var/marionette/workspaces"
    default_quota_mb: 10240  # 10GB
```

### S3 Storage

```yaml
storage:
  provider: s3
  s3:
    bucket: "marionette-workspaces"
    region: "us-west-2"
    prefix: "workspaces/"
```

### Runner-side sync

The runner needs its own backend to sync to, configured on `bin/agent`. It is
off by default:

```bash
./bin/agent ... \
  --storage-backend local \
  --storage-local-path /var/marionette/cas \
  --storage-encryption none
```

`--storage-encryption` has no default: storing workspace contents unencrypted is
a decision an operator makes explicitly. Per-tenant encryption is refused rather
than silently downgraded, because the runner has no way to obtain a tenant data
key yet. Only the `local` backend exists on the runner side today; it works
against a shared volume or a mounted object store.

### Workspace Options

!!! note "Not on the CLI yet"
    `mctl sessions create` has no workspace flags. Quota and mobility are set
    through the workspace API (`/api/v1/workspaces`).

## Workspace Lifecycle

=== "Diagram"

    ```mermaid
    flowchart TD
        A[Session Created] --> B[Workspace Created]
        B --> C[Tasks Execute]
        C --> D{Session State}
        D -->|Suspend| E[Workspace Synced]
        E --> F[Session Resumed]
        F --> G[Workspace Restored]
        G --> C
        D -->|Terminate| H[Session Terminated]
        H --> I[Workspace Cleanup]
    ```

=== "Text"

    ```
    Session Created
          │
          ▼
    Workspace Created (empty /workspace)
          │
          ▼
    Tasks Execute (files created/modified)
          │
          ├──► Session Suspended
          │         │
          │         ▼
          │    Workspace Synced (if object_sync)
          │         │
          │         ▼
          │    Session Resumed
          │         │
          │         ▼
          │    Workspace Restored
          │
          ▼
    Session Terminated
          │
          ▼
    Workspace Cleanup (configurable retention)
    ```

## Data Retention

Configure workspace retention after session termination:

```yaml
storage:
  workspace:
    retention:
      default: 7d      # Keep for 7 days after termination
      max: 30d         # Maximum retention
      min: 1d          # Minimum retention
```

## Next Steps

- [Storage Reference](../reference/storage.md) - Technical details on CAS
- [Configuration](../getting-started/configuration.md) - Storage configuration options

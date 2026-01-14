# Storage

Technical details on Marionette's storage system.

## Content-Addressable Storage (CAS)

CAS provides efficient, deduplicated storage for workspace files.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Content-Addressable Storage                      │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   File                                                              │
│   ────                                                              │
│   main.go (12KB)                                                    │
│      │                                                              │
│      ▼                                                              │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  Content-Defined Chunking (CDC)                             │   │
│   │  ┌───────────┐ ┌───────────┐ ┌───────────┐                  │   │
│   │  │ Chunk 1   │ │ Chunk 2   │ │ Chunk 3   │                  │   │
│   │  │ (4KB)     │ │ (4KB)     │ │ (4KB)     │                  │   │
│   │  └─────┬─────┘ └─────┬─────┘ └─────┬─────┘                  │   │
│   └────────┼─────────────┼─────────────┼────────────────────────┘   │
│            │             │             │                            │
│            ▼             ▼             ▼                            │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  SHA-256 Hash                                               │   │
│   │  abc123...    def456...    ghi789...                        │   │
│   └─────────────────────────────────────────────────────────────┘   │
│            │             │             │                            │
│            ▼             ▼             ▼                            │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  Compression (zstd)                                         │   │
│   └─────────────────────────────────────────────────────────────┘   │
│            │             │             │                            │
│            ▼             ▼             ▼                            │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  Encryption (AES-256-GCM, per-tenant keys)                  │   │
│   └─────────────────────────────────────────────────────────────┘   │
│            │             │             │                            │
│            ▼             ▼             ▼                            │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │  Object Storage (S3/GCS/Local)                              │   │
│   │  chunks/{tenant_id}/{hash}.zst.enc                          │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Deduplication

Chunks are deduplicated within a tenant:

```
Workspace A:           Workspace B:
├── main.go            ├── main.go (same)
├── utils.go           ├── utils.go (modified)
└── README.md          └── config.yaml

Storage:
├── chunk_abc123 (main.go - shared)
├── chunk_def456 (utils.go v1)
├── chunk_ghi789 (utils.go v2)
├── chunk_jkl012 (README.md)
└── chunk_mno345 (config.yaml)
```

### Manifests

Manifests track file-to-chunk mappings:

```json
{
  "id": "mfst_0002xK9mNpV1StGXR8",
  "workspace_id": "ws_xxx",
  "files": [
    {
      "path": "main.go",
      "size": 12288,
      "mode": 420,
      "chunks": ["abc123", "def456", "ghi789"]
    },
    {
      "path": "README.md",
      "size": 1024,
      "mode": 420,
      "chunks": ["jkl012"]
    }
  ],
  "total_size": 13312
}
```

## Encryption

### Per-Tenant Keys

Each tenant has a unique Data Encryption Key (DEK):

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Key Hierarchy                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   Key Encryption Key (KEK)          Master key from config          │
│          │                                                          │
│          ▼                                                          │
│   Data Encryption Keys (DEK)        Per-tenant, encrypted by KEK    │
│          │                                                          │
│          ▼                                                          │
│   Chunk Encryption                  AES-256-GCM with DEK            │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Algorithm

- **Encryption**: AES-256-GCM
- **Key Derivation**: HKDF-SHA256
- **Nonce**: Random 12 bytes per chunk

## Log Storage

### Hot Storage (PostgreSQL)

Recent logs stored in partitioned table:

```sql
CREATE TABLE logs (
  id TEXT,
  task_id TEXT,
  content TEXT,
  created_at TIMESTAMPTZ
) PARTITION BY RANGE (created_at);

-- Daily partitions
CREATE TABLE logs_20240115 PARTITION OF logs
  FOR VALUES FROM ('2024-01-15') TO ('2024-01-16');
```

### Cold Storage (Archive)

Old logs archived to object storage:

```
archives/{tenant_id}/sessions/{session_id}/logs.jsonl.zst.enc
```

## Workspace Sync

### Sync Flow

```
1. Task completes
2. Workspace diff calculated (CDC)
3. New chunks uploaded (deduped)
4. New manifest created
5. Old manifest marked for GC
```

### Incremental Sync

Only changed chunks are transferred:

```
Before:  [chunk_a, chunk_b, chunk_c]
After:   [chunk_a, chunk_b', chunk_c, chunk_d]

Transfer: [chunk_b', chunk_d]  // Only 2 chunks
```

## Garbage Collection

### Reference Counting

Chunks track reference count:

```sql
UPDATE chunks SET ref_count = ref_count - 1
WHERE hash = 'abc123';

-- GC job deletes chunks with ref_count = 0
DELETE FROM chunks WHERE ref_count = 0 AND deleted_at < NOW() - INTERVAL '7 days';
```

### Safe Deletion

Two-phase delete with grace period:

```
1. Mark chunk deleted_at = NOW()
2. Wait grace period (7 days default)
3. Actually delete if still unreferenced
```

## Configuration

```yaml
storage:
  provider: s3
  s3:
    bucket: "marionette-storage"
    region: "us-west-2"
    prefix: "data/"

  cas:
    chunk_size_min: 2KB
    chunk_size_avg: 8KB
    chunk_size_max: 32KB
    compression: zstd
    compression_level: 3

  gc:
    enabled: true
    interval: 1h
    grace_period: 7d
```

## Performance

### Benchmarks

| Operation | Throughput |
|-----------|------------|
| Chunk write | ~100 MB/s |
| Chunk read | ~150 MB/s |
| Manifest create | ~1000/s |
| Dedup ratio | 2-5x typical |

### Optimization Tips

- Use SSD-backed object storage
- Enable CDN for read-heavy workloads
- Tune chunk size for your workload
- Use regional buckets to minimize latency

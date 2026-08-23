# Storage System

Marionette uses Content-Addressable Storage (CAS) for workspaces and tiered storage for logs.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                    CAS Storage Architecture                         │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Workspace Data                                                     │
│       │                                                             │
│       ▼                                                             │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                     CAS Sync                                │    │
│  │  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐      │    │
│  │  │ < 100MB:    │    │  Compress   │    │  Encrypt    │      │    │
│  │  │ Single chunk│ -> │  (zstd)     │ -> │  (AES-GCM)  │      │    │
│  │  │ (tar.zst)   │    │             │    │  per-tenant │      │    │
│  │  ├─────────────┤    └─────────────┘    └─────────────┘      │    │
│  │  │ >= 100MB:   │           │                  │             │    │
│  │  │ CDC chunks  │           └──────────────────┼─────────┐   │    │
│  │  └─────────────┘                              ▼         ▼   │    │
│  │                                        ┌───────────────────┐│    │
│  │                                        │   Blob Store      ││    │
│  │                                        │ (tenant-scoped)   ││    │
│  │                                        └───────────────────┘│    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                               │                     │
│                     ┌─────────────────────────┼──────────────┐      │
│                     ▼                         ▼              ▼      │
│              ┌───────────┐            ┌───────────┐   ┌───────────┐ │
│              │   Local   │            │    S3     │   │    GCS    │ │
│              │  (dev)    │            │  (prod)   │   │  (prod)   │ │
│              └───────────┘            └───────────┘   └───────────┘ │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Storage Modes

| Workspace Size | Mode | Description |
|----------------|------|-------------|
| < `cdc_threshold` (100MB) | Single Chunk | Entire workspace as one tar.zst chunk |
| >= `cdc_threshold` (100MB) | CDC Chunking | Content-defined chunking with dedup |

Both modes use zstd compression for speed (3-5x faster than gzip).

The threshold is where holding a whole workspace in memory stops being
reasonable. Below it, one chunk is cheaper than thousands and a tar preserves
the tree for free. Above it, the sync has to stream, and everything in
[Bounded memory](#bounded-memory) applies.

`storage.cas.cdc_mode` overrides the choice: `auto` (default), `always` or
`never`. Tests use it to reach either path on a tree small enough to write in a
few lines; an operator who knows their workspaces can pin it.

### Archive fidelity

A snapshot has to restore the workspace that was taken, not an approximation of
it. Both modes record:

| Entry | Stored as | Restored as |
|-------|-----------|-------------|
| Regular file | chunk list + mode + mtime | written, chmod'd, mtime restored |
| Directory | mode | created with that mode, empty ones included |
| Symlink | target text | recreated verbatim, never followed |
| Socket, fifo, device | *skipped* | absent |

Symlinks are never resolved. Following one would inline the target's bytes under
the link's name, turning one file into two - and a link that dangles in the
workspace it came from has to dangle in the one it is restored into.

Sockets, fifos and devices carry nothing a snapshot can restore and mean nothing
on another machine, so they are left out rather than restored as empty files.

Modification times are restored because the incremental fast path depends on
them: a restore that reset every mtime would make the next sync re-chunk the
whole workspace.

## Tenant Isolation

**CRITICAL**: Chunks are NOT globally deduplicated across tenants. Each tenant has isolated storage:

- Chunks are keyed by `(tenant_id, hash)` - same content in different tenants = different chunks
- Each tenant has its own DEK (Data Encryption Key)
- No cross-tenant data leakage through hash collisions
- Deduplication only within a single tenant's workspaces

## Encryption

Envelope encryption with separate keys per tenant:

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Key Hierarchy                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  KEK (Key Encryption Key) - Master Key                              │
│  - Storage: env var / KMS / HSM / Vault                             │
│  - Purpose: encrypt DEKs                                            │
│                              │                                      │
│                    encrypts  │                                      │
│                              ▼                                      │
│  DEK (Data Encryption Key) - Per tenant                             │
│  - Unique key per tenant                                            │
│  - Storage: DB (encrypted by KEK)                                   │
│                              │                                      │
│                    encrypts  │                                      │
│                              ▼                                      │
│  Chunks (chunks/{tenant_id}/{hash}.blob.enc)                        │
│  - Storage: Local / S3 / GCS                                        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## BYOK Memory Security

In BYOK (Bring Your Own Key) mode, API keys are never persisted to disk and exist only in memory. Additional protections apply:

### Memory Protection Measures

1. **Memory Locking**: Sensitive data is locked in RAM to prevent swapping to disk
   ```go
   // Use mlock to prevent key from being swapped to disk
   unix.Mlock(keyBytes)
   defer unix.Munlock(keyBytes)
   ```

2. **Secure Zeroing**: Keys are explicitly zeroed before deallocation
   ```go
   // Zero memory before releasing
   for i := range keyBytes {
       keyBytes[i] = 0
   }
   ```

3. **Core Dump Prevention**: Process configured to exclude memory from core dumps
   ```go
   // Disable core dumps for this process
   unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
   ```

4. **Short-lived Keys**: BYOK API keys are held only for task duration
   - Key received via gRPC `AttachSession`
   - Injected as env var to sandbox process
   - Cleared from runner memory after task completion

### Threat Model

| Threat | Mitigation |
|--------|------------|
| Swap file exposure | mlock prevents swap |
| Core dump exposure | Disabled via prctl |
| Memory scanning | Short-lived, zeroed after use |
| Process inspection | Sandbox isolation, minimal privileges |
| gRPC interception | mTLS required |

### Configuration

```yaml
security:
  byok:
    # Lock sensitive memory (requires CAP_IPC_LOCK or root)
    mlock_enabled: true

    # Disable core dumps
    disable_core_dumps: true

    # Zero memory after use
    secure_zeroing: true

    # Maximum time to hold key in memory (seconds)
    key_ttl: 3600
```

### Limitations

- Memory locking requires `CAP_IPC_LOCK` capability or root
- mlock has system-wide limits (`ulimit -l`)
- Cannot protect against physical memory attacks (cold boot)
- In Kubernetes, requires `securityContext.capabilities.add: ["IPC_LOCK"]`

## Provider Interface

```go
// pkg/storage/storage.go

type StorageProvider interface {
    Name() string
    Upload(ctx context.Context, key string, r io.Reader, opts UploadOptions) error
    Download(ctx context.Context, key string) (io.ReadCloser, int64, error)
    Delete(ctx context.Context, key string) error
    Exists(ctx context.Context, key string) (bool, error)
}

type UploadOptions struct {
    ContentType string
    Metadata    map[string]string
}

// Local provider (development)
type LocalProvider struct {
    BasePath string  // /var/marionette/storage
}

// S3 provider (production)
type S3Provider struct {
    Client *s3.Client
    Bucket string
    Prefix string
    SSE    bool  // Server-Side Encryption
}

// GCS provider (production)
type GCSProvider struct {
    Client *storage.Client
    Bucket string
    Prefix string
}
```

## Configuration

```yaml
storage:
  # Storage provider
  provider: local  # local, s3, gcs

  local:
    path: /var/marionette/storage

  # Production: S3
  # provider: s3
  # s3:
  #   bucket: marionette-data
  #   region: us-west-2
  #   sse: true

  # Encryption (uses MARIONETTE_ENCRYPTION_KEY from env)
  encryption:
    enabled: true
    key_provider: local  # local, kms

  # Garbage collection (mark-and-sweep)
  gc:
    enabled: true
    interval: 24h
```

### Runner configuration

Workspace sync runs on the runner, so the chunking settings live in the
runner's config, not the server's:

```yaml
# marionette-agent config
storage:
  backend: local          # none (default) or local
  local_path: /var/marionette/cas
  encryption: none        # no default: storing workspaces unencrypted is a
                          # decision, not a fallback

  cas:
    cdc_threshold: 104857600   # 100 MB. Below this a workspace is one tar.zst
                               # chunk; at or above it, content-defined chunking.
    cdc_mode: auto             # auto | always | never
    max_concurrency: 10        # chunks in flight; see Bounded memory below
```

Every one of these may be omitted. `cdc_threshold: 0` and `max_concurrency: 0`
mean "unset" and take the defaults above; a negative value or an unknown
`cdc_mode` fails at startup rather than at the first suspend, where the only
symptom would be a workspace that quietly never got saved.

---

# CAS Implementation

For large workspaces (e.g., projects with `node_modules`, build artifacts, or ML models), full tar.zst archives become slow and bandwidth-intensive. Content-Addressable Storage (CAS) enables incremental sync by only transferring changed blocks.

## How It Works

1. **Content-Defined Chunking (CDC)**: Files are split into variable-size chunks using rolling hash (Rabin fingerprinting). Chunk boundaries are determined by content, not fixed offsets.

2. **Tenant-Scoped Deduplication**: Each chunk is hashed (SHA-256). Identical chunks within the same tenant are stored only once. **Chunks are NOT shared across tenants** for security isolation.

3. **Manifest**: A lightweight manifest file lists all chunks needed to reconstruct the workspace.

4. **Incremental Transfer**: On sync, only new/modified chunks are uploaded. On restore, only missing chunks are downloaded.

5. **Parent manifest reuse**: A sync given a previous manifest merges it against
   the walk. A file whose size, mode and modification time are unchanged carries
   its chunk list over without being read at all, so the second sync of a
   workspace costs the files that changed rather than the files that exist.
   Touching one file of a thousand uploads one file's chunks and asks storage
   about nothing else.

   Both sides are in directory-walk order, so this is a merge and not an index:
   the comparison holds two entries, not two manifests. Manifests written before
   the order was recorded set `ordered: false` in their header and are indexed in
   memory instead.

## CDC Implementation Details

We use the **restic chunker** algorithm (Rabin fingerprinting with polynomial `0x3DA3358B4DC173`).

### Bounded memory

CDC mode exists for workspaces too large to hold, so a sync must not hold one.
Nothing in the walk scales with the tree: files are streamed through the chunker
a chunk at a time, chunks are handed to an uploader that owns a fixed set of
buffers, and manifest entries are appended to the manifest object and forgotten.

What a sync costs is set by configuration:

| Term | Bound | Default |
|------|-------|---------|
| Chunk uploads in flight | `max_concurrency` x `chunk_max_size` | 10 x 8 MB |
| Chunker read buffer | `chunk_max_size` | 8 MB |
| Manifest frame | frame size | 4 MB |
| Dedup set | `MaxSeenChunks` x ~72 bytes | ~75 MB, capped |
| One file's chunk hashes | file size / `chunk_target_size` x 64 bytes | 6.4 MB per 100 GB file |

Only the last term depends on the workspace at all, and it depends on the
largest single file rather than on the tree.

The dedup set is the one structure that would otherwise grow without limit - one
hash per distinct chunk. It stops growing at `MaxSeenChunks`; past that,
deduplication falls back to asking storage whether a chunk is already there,
which is slower and equally correct.

A runner on a small box lowers `max_concurrency`. That is the term that matters:
each slot holds up to one maximum-sized chunk.

Restore is the same shape in reverse. Entries arrive from a streaming cursor and
chunks are fetched at most `max_concurrency` ahead of the write, so a restore's
memory is bounded the same way. There is no chunk cache: a workspace with the
same chunk in many files re-fetches it, which trades bandwidth for a bound.

Measured: a 512 MB tree costs what a 128 MB tree costs
(`TestCDC_MemoryDoesNotFollowWorkspaceSize`).

### Resumable restore

A restore that is interrupted converges when it runs again. Every file is
written to a scratch name in its own directory and renamed into place, so a file
that exists is a file that is complete; a re-run skips entries already on disk
with the recorded size, mode and modification time, and rewrites the rest.

### Library

```go
import "github.com/restic/chunker"
```

### Chunker Configuration

```go
// pkg/storage/cas/chunker.go
package cas

import (
    "io"
    "github.com/restic/chunker"
)

// ChunkerConfig defines CDC parameters
type ChunkerConfig struct {
    MinSize    uint // Minimum chunk size (default: 512 KB)
    MaxSize    uint // Maximum chunk size (default: 8 MB)
    TargetSize uint // Target average size (default: 1 MB)
}

var DefaultChunkerConfig = ChunkerConfig{
    MinSize:    512 * 1024,   // 512 KB
    MaxSize:    8 * 1024 * 1024, // 8 MB
    TargetSize: 1 * 1024 * 1024, // 1 MB (average)
}

// Polynomial for Rabin fingerprinting (same as restic)
// Chosen for good distribution and low collision rate
const RabinPolynomial = chunker.Pol(0x3DA3358B4DC173)

// Chunker wraps restic's chunker with our config
type Chunker struct {
    config ChunkerConfig
    pol    chunker.Pol
}

func NewChunker(config ChunkerConfig) *Chunker {
    return &Chunker{
        config: config,
        pol:    RabinPolynomial,
    }
}

// ChunkReader splits a reader into content-defined chunks
func (c *Chunker) ChunkReader(r io.Reader) <-chan []byte {
    chunks := make(chan []byte, 10)

    go func() {
        defer close(chunks)

        chnkr := chunker.New(r, c.pol)
        buf := make([]byte, c.config.MaxSize)

        for {
            chunk, err := chnkr.Next(buf)
            if err == io.EOF {
                break
            }
            if err != nil {
                return
            }

            // Copy chunk data (buffer is reused)
            data := make([]byte, chunk.Length)
            copy(data, chunk.Data)
            chunks <- data
        }
    }()

    return chunks
}

// ChunkFile splits a file into chunks and returns them with hashes
func (c *Chunker) ChunkFile(path string) ([]ChunkInfo, error) {
    f, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    var result []ChunkInfo
    for data := range c.ChunkReader(f) {
        hash := sha256.Sum256(data)
        result = append(result, ChunkInfo{
            Hash: hex.EncodeToString(hash[:]),
            Size: int64(len(data)),
            Data: data,
        })
    }
    return result, nil
}

type ChunkInfo struct {
    Hash string
    Size int64
    Data []byte
}
```

### Why Rabin Fingerprinting?

| Feature | Fixed-Size Chunks | Rabin CDC |
|---------|-------------------|-----------|
| Insert at file start | All chunks change | Only first chunk changes |
| Deduplication | Poor (offset-dependent) | Excellent (content-dependent) |
| Chunk size variance | None | Controlled (min/max bounds) |
| CPU overhead | Minimal | ~200 MB/s per core |

### Chunk Size Selection

| Size | Dedup Ratio | Overhead | Use Case |
|------|-------------|----------|----------|
| 64 KB | Highest | High metadata | Small files, frequent changes |
| 256 KB | High | Moderate | General purpose |
| 1 MB | Medium | Low | Large files, infrequent changes |
| 8 MB | Low | Minimal | Very large binary files |

Default: 1 MB target with 512 KB min, 8 MB max provides good balance.

## Data Model

```go
// Chunk represents a content-addressed block
type Chunk struct {
    Hash     string    // SHA-256 of compressed content
    TenantID string    // Tenant isolation
    Size     int64     // Compressed size in bytes
    RefCount int       // Number of manifests referencing this chunk
}

// Manifest represents a workspace snapshot
type Manifest struct {
    ID          string             // mfst_xxx (prefixed ID)
    WorkspaceID string             // ws_xxx
    TenantID    string             // Tenant isolation
    CreatedAt   time.Time
    TotalSize   int64              // Original size

    // Single chunk mode (for workspaces < 100MB)
    SingleChunk bool               // True if stored as single tar.zst chunk
    ChunkHash   string             // Hash of single chunk (if SingleChunk=true)

    // CDC mode (for workspaces >= 100MB)
    ChunkCount  int
    Files       []ManifestFile     // File list with chunk references
}

// ManifestFile represents a file in the manifest
type ManifestFile struct {
    Path     string      // Relative path from workspace root
    Mode     os.FileMode // Permissions
    ModTime  time.Time
    Size     int64
    Chunks   []string    // Ordered list of chunk hashes
}
```

## Database Schema

The authoritative schema is `migrations/*.up.sql`; `docs/schema.sql` renders it
(generated, drift-checked in CI). Key tables:

```sql
-- Tenant-scoped chunks (NOT globally deduplicated)
CREATE TABLE chunks (
    hash        TEXT NOT NULL,
    tenant_id   TEXT NOT NULL,
    size        BIGINT NOT NULL,
    ref_count   INT NOT NULL DEFAULT 1,
    deleted_at  TIMESTAMPTZ,           -- Soft delete for GC
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, hash)
);

-- Workspace manifests
CREATE TABLE manifests (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id),
    tenant_id       TEXT NOT NULL,
    single_chunk    BOOLEAN NOT NULL DEFAULT FALSE,
    chunk_hash      TEXT,
    -- ... other fields
);
```

## Storage Layout

```
storage/
├── chunks/
│   └── {tenant_id}/
│       ├── a1/
│       │   └── a1b2c3d4e5f6...abc.blob.enc    # Encrypted chunk
│       ├── b2/
│       │   └── b2c3d4e5f6a7...def.blob.enc
│       └── ...
└── manifests/
    └── {tenant_id}/
        └── {workspace_id}/
            ├── {manifest_id}.jsonl.zst.enc    # JSONL: streaming format
            └── ...
```

## Manifest Format (JSONL Streaming)

Manifests use **JSONL (JSON Lines)** format for memory-efficient streaming. Each line is a separate JSON object:

- **Line 1**: Manifest header (metadata without files)
- **Lines 2+**: One `ManifestFile` per line

A manifest lists every file in a workspace, so it is as large as the workspace is
wide. Encoding the JSONL, compressing it and encrypting it as three whole buffers
would make describing a workspace cost as much as storing it, so large manifests
are stored **framed**:

```
"MFSTF1\n"                     magic
uvarint len | frame            the header, exactly one JSON line
uvarint len | frame            entries, whole lines, 4 MB of JSONL each
...
```

Each frame goes through the same envelope encryptor - compressed with zstd, then
sealed - so the object is still JSONL, still zstd, still encrypted, still at
`manifests/{tenant}/{workspace}/{manifest}.jsonl.zst.enc`. A writer holds one
frame and so does a reader.

The header is written last and stored first. Its counts - how many chunks, how
many entries - are only known once the walk that produced them has finished, so
frames are spooled to an unlinked temporary file and the header is prepended at
upload. That also makes the upload the single durability point: nothing records
a manifest until its object is written.

Objects without the magic are the original single-buffer encoding and are read
the way they always were. Single-chunk manifests are still written that way,
because they have no file list to stream.

```
┌─────────────────────────────────────────────────────────────────────┐
│                    JSONL Manifest Format                            │
├─────────────────────────────────────────────────────────────────────┤
│  Line 1:  {"id":"mfst_xxx","workspace_id":"ws_xxx","tenant_id":...} │
│  Line 2:  {"path":"src/main.go","mode":420,"size":1234,"chunks":[]} │
│  Line 3:  {"path":"src/util.go","mode":420,"size":567,"chunks":[]}  │
│  ...                                                                │
│  Line N:  {"path":"README.md","mode":420,"size":89,"chunks":[]}     │
└─────────────────────────────────────────────────────────────────────┘
```

### Data Structures

```go
// ManifestHeader is the first line of JSONL manifest
type ManifestHeader struct {
    ID          string    `json:"id"`
    WorkspaceID string    `json:"workspace_id"`
    TenantID    string    `json:"tenant_id"`
    CreatedAt   time.Time `json:"created_at"`
    TotalSize   int64     `json:"total_size"`
    ChunkCount  int       `json:"chunk_count"`

    // Single chunk mode (for workspaces below cdc_threshold)
    SingleChunk bool   `json:"single_chunk,omitempty"`
    ChunkHash   string `json:"chunk_hash,omitempty"`

    // Ordered reports that the entries are in directory-walk order, which is
    // what lets an incremental sync merge against them instead of indexing
    // them. FileCount is how many follow; zero means a manifest written
    // before the field existed.
    Ordered   bool `json:"ordered,omitempty"`
    FileCount int  `json:"file_count,omitempty"`
}

// ManifestFile is each subsequent line
type ManifestFile struct {
    Path    string      `json:"path"`
    Mode    os.FileMode `json:"mode"`
    ModTime time.Time   `json:"mod_time"`
    Size    int64       `json:"size"`
    Chunks  []string    `json:"chunks"`

    // Type is "" (regular file), "d" (directory) or "l" (symlink). The empty
    // string means a file so that manifests written before directories and
    // symlinks were recorded still read correctly. Link is the symlink
    // target, stored verbatim and never resolved.
    Type string `json:"type,omitempty"`
    Link string `json:"link,omitempty"`
}
```

### Streaming Save (Memory-Efficient)

```go
// pkg/storage/cas/manifest.go

func (s *CASSync) saveManifest(ctx context.Context, manifest *Manifest) error {
    key := fmt.Sprintf("manifests/%s/%s/%s.jsonl.zst.enc",
        manifest.TenantID, manifest.WorkspaceID, manifest.ID)

    // Use io.Pipe for streaming: encode -> compress -> encrypt -> upload
    pr, pw := io.Pipe()

    var uploadErr error
    uploadDone := make(chan struct{})

    // Uploader goroutine
    go func() {
        defer close(uploadDone)
        uploadErr = s.storage.Upload(ctx, key, pr, storage.UploadOptions{
            ContentType: "application/x-ndjson+zstd",
        })
    }()

    // Producer: stream JSONL lines
    func() {
        defer pw.Close()

        // Encrypt the compressed stream
        encWriter := s.crypto.NewEncryptWriter(manifest.TenantID, pw)
        defer encWriter.Close()

        // Compress with zstd
        zw, _ := zstd.NewWriter(encWriter, zstd.WithEncoderLevel(zstd.SpeedDefault))
        defer zw.Close()

        enc := json.NewEncoder(zw)

        // Line 1: Header (manifest metadata without files)
        header := ManifestHeader{
            ID:          manifest.ID,
            WorkspaceID: manifest.WorkspaceID,
            TenantID:    manifest.TenantID,
            CreatedAt:   manifest.CreatedAt,
            TotalSize:   manifest.TotalSize,
            ChunkCount:  manifest.ChunkCount,
            SingleChunk: manifest.SingleChunk,
            ChunkHash:   manifest.ChunkHash,
        }
        if err := enc.Encode(header); err != nil {
            return
        }

        // Lines 2+: One ManifestFile per line
        for _, file := range manifest.Files {
            if err := enc.Encode(file); err != nil {
                return
            }
        }
    }()

    <-uploadDone
    return uploadErr
}
```

### Streaming Load (Memory-Efficient)

```go
func (s *CASSync) loadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*Manifest, error) {
    key := fmt.Sprintf("manifests/%s/%s/%s.jsonl.zst.enc",
        tenantID, workspaceID, manifestID)

    reader, _, err := s.storage.Download(ctx, key)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    // Decrypt -> decompress -> decode JSONL
    decReader, err := s.crypto.NewDecryptReader(tenantID, reader)
    if err != nil {
        return nil, err
    }
    defer decReader.Close()

    zr, err := zstd.NewReader(decReader)
    if err != nil {
        return nil, err
    }
    defer zr.Close()

    scanner := bufio.NewScanner(zr)
    scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line

    // Line 1: Header
    if !scanner.Scan() {
        return nil, fmt.Errorf("empty manifest")
    }

    var header ManifestHeader
    if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
        return nil, fmt.Errorf("invalid manifest header: %w", err)
    }

    manifest := &Manifest{
        ID:          header.ID,
        WorkspaceID: header.WorkspaceID,
        TenantID:    header.TenantID,
        CreatedAt:   header.CreatedAt,
        TotalSize:   header.TotalSize,
        ChunkCount:  header.ChunkCount,
        SingleChunk: header.SingleChunk,
        ChunkHash:   header.ChunkHash,
    }

    // Lines 2+: ManifestFiles
    for scanner.Scan() {
        var file ManifestFile
        if err := json.Unmarshal(scanner.Bytes(), &file); err != nil {
            return nil, fmt.Errorf("invalid manifest file entry: %w", err)
        }
        manifest.Files = append(manifest.Files, file)
    }

    if err := scanner.Err(); err != nil {
        return nil, fmt.Errorf("manifest read error: %w", err)
    }

    return manifest, nil
}

// StreamManifestFiles returns a channel for streaming files (ultra memory-efficient)
// Use this for very large manifests (100k+ files)
func (s *CASSync) StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan ManifestFile, *ManifestHeader, error) {
    key := fmt.Sprintf("manifests/%s/%s/%s.jsonl.zst.enc",
        tenantID, workspaceID, manifestID)

    reader, _, err := s.storage.Download(ctx, key)
    if err != nil {
        return nil, nil, err
    }

    decReader, err := s.crypto.NewDecryptReader(tenantID, reader)
    if err != nil {
        reader.Close()
        return nil, nil, err
    }

    zr, err := zstd.NewReader(decReader)
    if err != nil {
        decReader.Close()
        reader.Close()
        return nil, nil, err
    }

    scanner := bufio.NewScanner(zr)
    scanner.Buffer(make([]byte, 64*1024), 1024*1024)

    // Read header first (synchronously)
    if !scanner.Scan() {
        zr.Close()
        decReader.Close()
        reader.Close()
        return nil, nil, fmt.Errorf("empty manifest")
    }

    var header ManifestHeader
    if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
        zr.Close()
        decReader.Close()
        reader.Close()
        return nil, nil, err
    }

    // Stream files asynchronously
    ch := make(chan ManifestFile, 100)
    go func() {
        defer close(ch)
        defer zr.Close()
        defer decReader.Close()
        defer reader.Close()

        for scanner.Scan() {
            var file ManifestFile
            if err := json.Unmarshal(scanner.Bytes(), &file); err != nil {
                continue
            }

            select {
            case ch <- file:
            case <-ctx.Done():
                return
            }
        }
    }()

    return ch, &header, nil
}
```

### Compression Ratios

| Workspace | Files | Uncompressed | Compressed | Ratio |
|-----------|-------|--------------|------------|-------|
| Small project | 100 | 15 KB | 3 KB | 5x |
| Medium project | 1,000 | 150 KB | 20 KB | 7.5x |
| Large monorepo | 50,000 | 8 MB | 800 KB | 10x |
| node_modules heavy | 100,000 | 20 MB | 1.5 MB | 13x |

## Memory-Efficient Processing

**CRITICAL**: CAS operations must be memory-efficient for large workspaces. The implementations below use streaming to avoid loading entire workspaces/archives into memory.

### Memory Budget

| Operation | Max Memory | Strategy |
|-----------|------------|----------|
| Single file chunk | 8 MB | Max chunk size limit |
| Parallel uploads | 80 MB | 10 concurrent × 8 MB |
| Parallel downloads | 80 MB | 10 concurrent × 8 MB |
| Manifest save | 4 MB frame | Framed JSONL, spooled to a temp file |
| Manifest load | 4 MB frame | One frame decrypted at a time |
| Manifest stream | 4 MB frame | Cursor over the same frames |
| Dedup set | ~75 MB, capped | `MaxSeenChunks` hashes, then falls back to storage |
| Log archive | 64 KB buffer | Streaming with cursor-based pagination |

See [Bounded memory](#bounded-memory) for how these terms are enforced and what
`max_concurrency` changes.

### Key Principles

1. **Never load entire workspace into memory**
2. **Stream chunks to storage, don't buffer**
3. **Use temp files for encryption (encrypt-then-upload)**
4. **Paginate database queries**
5. **Use io.Pipe for concurrent processing**

## Sync Implementation (Memory-Efficient)

```go
// pkg/storage/cas/sync.go

import "github.com/chunlea/marionette/pkg/id"

type CASSync struct {
    store    store.Store
    storage  storage.StorageProvider
    crypto   *crypto.Service
    chunker  *Chunker
    tempDir  string  // For temporary encrypted chunks
}

// Sync performs incremental workspace sync with bounded memory
func (s *CASSync) Sync(ctx context.Context, workspaceID, tenantID, srcDir string) error {
    totalSize := calculateDirSize(srcDir)

    if totalSize < s.config.SingleChunkThreshold {
        return s.syncAsSingleChunk(ctx, workspaceID, tenantID, srcDir)
    }
    return s.syncWithCDC(ctx, workspaceID, tenantID, srcDir)
}

// syncWithCDC uses streaming to avoid loading all chunks into memory
func (s *CASSync) syncWithCDC(ctx context.Context, workspaceID, tenantID, srcDir string) error {
    // 1. Get previous manifest chunk hashes (just hashes, not data)
    prevManifest, _ := s.store.GetLatestManifest(ctx, workspaceID)
    prevChunks := make(map[string]bool)
    if prevManifest != nil {
        for _, f := range prevManifest.Files {
            for _, h := range f.Chunks {
                prevChunks[h] = true
            }
        }
    }

    // 2. Create manifest and chunk upload channel
    manifest := &Manifest{
        ID:          id.Manifest(),
        WorkspaceID: workspaceID,
        TenantID:    tenantID,
        CreatedAt:   time.Now(),
    }

    // Channel for chunks to upload (hash -> temp file path)
    // This avoids holding chunk data in memory
    type chunkJob struct {
        hash     string
        tempPath string
    }
    chunkJobs := make(chan chunkJob, 100)

    // 3. Walk files and write chunks to temp files (not memory)
    g, gctx := errgroup.WithContext(ctx)

    // Producer: chunk files and write to temp dir
    g.Go(func() error {
        defer close(chunkJobs)

        return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
            if err != nil || info.IsDir() {
                return err
            }

            relPath, _ := filepath.Rel(srcDir, path)
            file, err := os.Open(path)
            if err != nil {
                return err
            }
            defer file.Close()

            mf := ManifestFile{
                Path:    relPath,
                Mode:    info.Mode(),
                ModTime: info.ModTime(),
                Size:    info.Size(),
            }

            // Stream chunks, write new ones to temp files
            for chunk := range s.chunker.ChunkReader(file) {
                hash := sha256Hex(chunk)
                mf.Chunks = append(mf.Chunks, hash)

                // Only process if chunk is new
                if !prevChunks[hash] {
                    exists, _ := s.store.ChunkExists(gctx, tenantID, hash)
                    if !exists {
                        // Write encrypted chunk to temp file (not memory)
                        tempPath := filepath.Join(s.tempDir, hash+".tmp")
                        if err := s.writeEncryptedChunk(tenantID, chunk, tempPath); err != nil {
                            return err
                        }

                        select {
                        case chunkJobs <- chunkJob{hash: hash, tempPath: tempPath}:
                        case <-gctx.Done():
                            return gctx.Err()
                        }
                    }
                }
            }

            manifest.Files = append(manifest.Files, mf)
            manifest.TotalSize += info.Size()
            manifest.ChunkCount += len(mf.Chunks)

            return nil
        })
    })

    // Consumers: upload chunks from temp files
    sem := make(chan struct{}, 10) // Max 10 concurrent uploads
    for i := 0; i < 10; i++ {
        g.Go(func() error {
            for job := range chunkJobs {
                sem <- struct{}{}

                // Upload from temp file (streaming, not loading into memory)
                f, err := os.Open(job.tempPath)
                if err != nil {
                    <-sem
                    return err
                }

                key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc", tenantID, job.hash[:2], job.hash)
                err = s.storage.Upload(gctx, key, f, storage.UploadOptions{})
                f.Close()
                os.Remove(job.tempPath) // Clean up temp file

                <-sem
                if err != nil {
                    return err
                }
            }
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return err
    }

    // 4. Save manifest and update chunk ref counts
    return s.store.CreateManifest(ctx, manifest)
}

// writeEncryptedChunk encrypts chunk data and writes to temp file
func (s *CASSync) writeEncryptedChunk(tenantID string, data []byte, path string) error {
    encrypted := s.crypto.EncryptForTenant(tenantID, data)
    return os.WriteFile(path, encrypted, 0600)
}

// Restore downloads and reconstructs workspace with bounded memory
// Uses disk cache instead of loading all chunks into memory
func (s *CASSync) Restore(ctx context.Context, workspaceID, tenantID, dstDir string) error {
    manifest, err := s.store.GetLatestManifest(ctx, workspaceID)
    if err != nil {
        return err
    }

    // Handle single chunk mode
    if manifest.SingleChunk {
        return s.restoreFromSingleChunk(ctx, manifest, tenantID, dstDir)
    }

    // CDC mode: collect unique chunks needed
    neededChunks := make(map[string]bool)
    for _, f := range manifest.Files {
        for _, h := range f.Chunks {
            neededChunks[h] = true
        }
    }

    // Download chunks to temp dir (not memory)
    chunkDir := filepath.Join(s.tempDir, "restore-"+manifest.ID)
    os.MkdirAll(chunkDir, 0755)
    defer os.RemoveAll(chunkDir) // Clean up after restore

    g, gctx := errgroup.WithContext(ctx)
    sem := make(chan struct{}, 10) // Max 10 concurrent downloads

    for hash := range neededChunks {
        hash := hash
        g.Go(func() error {
            sem <- struct{}{}
            defer func() { <-sem }()

            chunkPath := filepath.Join(chunkDir, hash)

            // Download encrypted chunk to temp file
            key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc", tenantID, hash[:2], hash)
            reader, _, err := s.storage.Download(gctx, key)
            if err != nil {
                return err
            }

            // Stream decrypt to temp file (not memory)
            return s.streamDecryptToFile(tenantID, reader, chunkPath)
        })
    }
    if err := g.Wait(); err != nil {
        return err
    }

    // Reconstruct files by reading from temp chunk files
    for _, f := range manifest.Files {
        targetPath := filepath.Join(dstDir, f.Path)

        // Security: prevent path traversal
        if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(dstDir)) {
            return fmt.Errorf("invalid path: %s", f.Path)
        }

        os.MkdirAll(filepath.Dir(targetPath), 0755)

        file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode)
        if err != nil {
            return err
        }

        // Stream chunks from temp files to target file
        for _, hash := range f.Chunks {
            chunkPath := filepath.Join(chunkDir, hash)
            chunkFile, err := os.Open(chunkPath)
            if err != nil {
                file.Close()
                return err
            }
            io.Copy(file, chunkFile)
            chunkFile.Close()
        }

        file.Close()
        os.Chtimes(targetPath, f.ModTime, f.ModTime)
    }

    return nil
}

// streamDecryptToFile decrypts data from reader and writes to file
// Memory usage: O(block_size) not O(file_size)
func (s *CASSync) streamDecryptToFile(tenantID string, reader io.ReadCloser, path string) error {
    defer reader.Close()

    // For AES-GCM, we need to read the entire ciphertext to verify the tag
    // Use temp file for encrypted data, then decrypt
    tempPath := path + ".enc"
    tempFile, err := os.Create(tempPath)
    if err != nil {
        return err
    }

    _, err = io.Copy(tempFile, reader)
    tempFile.Close()
    if err != nil {
        os.Remove(tempPath)
        return err
    }

    // Read encrypted data (this is unavoidable with AES-GCM)
    encrypted, err := os.ReadFile(tempPath)
    os.Remove(tempPath)
    if err != nil {
        return err
    }

    // Decrypt and write to final path
    data := s.crypto.DecryptForTenant(tenantID, encrypted)
    return os.WriteFile(path, data, 0600)
}

// restoreFromSingleChunk handles single-chunk manifest restore
func (s *CASSync) restoreFromSingleChunk(ctx context.Context, manifest *Manifest, tenantID, dstDir string) error {
    // 1. Download the single chunk
    key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc", tenantID, manifest.ChunkHash[:2], manifest.ChunkHash)
    reader, _, err := s.storage.Download(ctx, key)
    if err != nil {
        return err
    }
    defer reader.Close()

    encrypted, _ := io.ReadAll(reader)
    data := s.crypto.DecryptForTenant(tenantID, encrypted)

    // 2. Decompress and untar
    zr, _ := zstd.NewReader(bytes.NewReader(data))
    defer zr.Close()

    tr := tar.NewReader(zr)
    for {
        header, err := tr.Next()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }

        targetPath := filepath.Join(dstDir, header.Name)

        // Security: prevent path traversal
        if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(dstDir)) {
            return fmt.Errorf("invalid path in archive: %s", header.Name)
        }

        switch header.Typeflag {
        case tar.TypeDir:
            os.MkdirAll(targetPath, os.FileMode(header.Mode))
        case tar.TypeReg:
            os.MkdirAll(filepath.Dir(targetPath), 0755)
            f, _ := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
            io.Copy(f, tr)
            f.Close()
            os.Chtimes(targetPath, header.ModTime, header.ModTime)
        }
    }

    return nil
}
```

## Small Workspace Optimization

For small workspaces (below `cdc_threshold`), CAS stores the entire workspace as
a single chunk:

```go
func (s *CASSync) syncAsSingleChunk(ctx context.Context, workspaceID, tenantID, srcDir string) error {
    // 1. Create tar.zst archive in memory
    buf := new(bytes.Buffer)
    zw, _ := zstd.NewWriter(buf)
    tw := tar.NewWriter(zw)

    filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        relPath, _ := filepath.Rel(srcDir, path)

        // The link target has to be read and passed in: tar.FileInfoHeader
        // cannot read it, and "" records a link that points nowhere.
        link := ""
        if info.Mode()&os.ModeSymlink != 0 {
            link, _ = os.Readlink(path)
        }

        header, _ := tar.FileInfoHeader(info, link)
        header.Name = relPath
        tw.WriteHeader(header)

        if !info.IsDir() {
            f, _ := os.Open(path)
            io.Copy(tw, f)
            f.Close()
        }
        return nil
    })

    tw.Close()
    zw.Close()

    // 2. Hash compressed data, then encrypt with tenant key
    data := buf.Bytes()
    hash := sha256Hex(data)
    encrypted := s.crypto.EncryptForTenant(tenantID, data)

    // 3. Upload as single chunk (tenant-scoped path)
    key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc", tenantID, hash[:2], hash)
    s.storage.Upload(ctx, key, bytes.NewReader(encrypted), storage.UploadOptions{})

    // 4. Create manifest with single chunk
    manifest := &Manifest{
        ID:          id.Manifest(),
        WorkspaceID: workspaceID,
        TenantID:    tenantID,
        SingleChunk: true,
        ChunkHash:   hash,
        TotalSize:   int64(len(data)),
    }

    return s.store.CreateManifest(ctx, manifest)
}
```

## Performance Comparison

| Scenario | Single Chunk (< 100MB) | CDC Chunking (≥ 100MB) |
|----------|------------------------|------------------------|
| Initial sync | Fast (single upload) | Parallel chunk upload |
| Small code change | Re-upload entire chunk | Upload only changed chunks |
| Resume unchanged | Download single chunk | Manifest check only |
| Cross-workspace dedup | Within tenant only | Within tenant only |

---

# Garbage Collection (Mark-and-Sweep)

Chunk GC uses a safe mark-and-sweep algorithm with soft delete to prevent race conditions.

## Algorithm

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Mark-and-Sweep GC                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Phase 1: Mark (soft delete)                                        │
│  ─────────────────────────────────────────────────────────────────  │
│  1. Find chunks where ref_count = 0                                 │
│  2. Set deleted_at = NOW() (soft delete)                            │
│  3. Do NOT delete from object storage yet                           │
│                                                                     │
│  Phase 2: Sweep (after grace period)                                │
│  ─────────────────────────────────────────────────────────────────  │
│  1. Find chunks where deleted_at < NOW() - grace_period             │
│  2. Re-check ref_count = 0 (in case of concurrent sync)             │
│  3. If still orphaned: delete from S3/GCS                           │
│  4. Delete from database                                            │
│                                                                     │
│  Grace Period: 7 days (configurable)                                │
│  - Protects against: sync in progress, retries, race conditions     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Implementation

```go
// pkg/jobs/chunk_gc.go

type ChunkGC struct {
    store       store.Store
    storage     storage.StorageProvider
    gracePeriod time.Duration  // Default: 7 days
}

func (gc *ChunkGC) Run(ctx context.Context) error {
    // Phase 1: Mark orphaned chunks for deletion (soft delete)
    if err := gc.markOrphans(ctx); err != nil {
        return fmt.Errorf("mark phase failed: %w", err)
    }

    // Phase 2: Sweep chunks past grace period
    if err := gc.sweepExpired(ctx); err != nil {
        return fmt.Errorf("sweep phase failed: %w", err)
    }

    return nil
}

func (gc *ChunkGC) markOrphans(ctx context.Context) error {
    // Mark chunks with ref_count=0 that haven't been marked yet
    return gc.store.Exec(ctx, `
        UPDATE chunks
        SET deleted_at = NOW()
        WHERE ref_count = 0
          AND deleted_at IS NULL
    `)
}

func (gc *ChunkGC) sweepExpired(ctx context.Context) error {
    // Find chunks marked for deletion past grace period
    cutoff := time.Now().Add(-gc.gracePeriod)
    chunks, err := gc.store.Query(ctx, `
        SELECT tenant_id, hash
        FROM chunks
        WHERE deleted_at < $1
          AND ref_count = 0  -- Re-check in case ref was added
    `, cutoff)
    if err != nil {
        return err
    }

    // Delete from object storage first
    for _, chunk := range chunks {
        key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc",
            chunk.TenantID, chunk.Hash[:2], chunk.Hash)
        if err := gc.storage.Delete(ctx, key); err != nil {
            // Log but continue - object may not exist
            log.Warn("failed to delete chunk from storage", "key", key, "err", err)
        }
    }

    // Delete from database
    return gc.store.Exec(ctx, `
        DELETE FROM chunks
        WHERE deleted_at < $1
          AND ref_count = 0
    `, cutoff)
}

// UnmarkIfReferenced is called when a new manifest references a chunk
// This "resurrects" a chunk that was marked for deletion
func (gc *ChunkGC) UnmarkIfReferenced(ctx context.Context, tenantID, hash string) error {
    return gc.store.Exec(ctx, `
        UPDATE chunks
        SET deleted_at = NULL,
            ref_count = ref_count + 1
        WHERE tenant_id = $1 AND hash = $2
    `, tenantID, hash)
}
```

## Safety Guarantees

1. **Race Condition Prevention**: Grace period (7 days) ensures in-flight syncs complete
2. **Double-Check**: Sweep phase re-verifies ref_count=0 before deletion
3. **Resurrection**: Chunks can be unmarked if referenced during grace period
4. **Idempotent**: Multiple GC runs are safe

## Manifest-Chunk Reference Integrity

**Problem**: If a manifest references chunks, and GC runs before the manifest is committed, chunks could be deleted prematurely.

**Solution**: Manifests maintain ref_count on chunks. The sync process must:

1. **Increment ref_count BEFORE uploading chunks** (optimistic)
2. **Commit manifest atomically**
3. **Decrement ref_count on rollback**

```go
// pkg/storage/cas/sync.go

func (s *CASSync) CreateManifest(ctx context.Context, manifest *Manifest) error {
    // 1. Collect all chunk hashes
    chunkHashes := manifest.CollectChunkHashes()

    // 2. Begin transaction
    tx, err := s.store.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 3. Increment ref_count for all chunks (or resurrect if soft-deleted)
    for _, hash := range chunkHashes {
        _, err := tx.Exec(ctx, `
            INSERT INTO chunks (tenant_id, hash, size, ref_count, deleted_at)
            VALUES ($1, $2, $3, 1, NULL)
            ON CONFLICT (tenant_id, hash) DO UPDATE
            SET ref_count = chunks.ref_count + 1,
                deleted_at = NULL  -- Resurrect if was marked for deletion
        `, manifest.TenantID, hash, 0)
        if err != nil {
            return err
        }
    }

    // 4. Insert manifest
    if err := tx.InsertManifest(ctx, manifest); err != nil {
        return err
    }

    // 5. Commit transaction
    return tx.Commit()
}

// Called when manifest is deleted (e.g., workspace cleanup)
func (s *CASSync) DeleteManifest(ctx context.Context, manifestID string) error {
    manifest, err := s.store.GetManifest(ctx, manifestID)
    if err != nil {
        return err
    }

    tx, err := s.store.BeginTx(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Decrement ref_count for all chunks
    for _, hash := range manifest.CollectChunkHashes() {
        _, err := tx.Exec(ctx, `
            UPDATE chunks
            SET ref_count = ref_count - 1
            WHERE tenant_id = $1 AND hash = $2
        `, manifest.TenantID, hash)
        if err != nil {
            return err
        }
    }

    // Delete manifest
    if err := tx.DeleteManifest(ctx, manifestID); err != nil {
        return err
    }

    return tx.Commit()
}
```

### Pre-Restore Validation

Before restoring a workspace from a manifest, verify all chunks exist:

```go
func (s *CASSync) ValidateManifest(ctx context.Context, manifest *Manifest) error {
    chunkHashes := manifest.CollectChunkHashes()

    // Check all chunks exist in database
    missing, err := s.store.FindMissingChunks(ctx, manifest.TenantID, chunkHashes)
    if err != nil {
        return err
    }

    if len(missing) > 0 {
        return &ChunksMissingError{
            ManifestID: manifest.ID,
            Missing:    missing,
        }
    }

    // Verify chunks exist in object storage (optional, for paranoid mode)
    if s.config.ValidateStorage {
        for _, hash := range chunkHashes {
            key := fmt.Sprintf("chunks/%s/%s/%s.blob.enc",
                manifest.TenantID, hash[:2], hash)
            exists, err := s.storage.Exists(ctx, key)
            if err != nil {
                return err
            }
            if !exists {
                return &ChunksMissingError{
                    ManifestID: manifest.ID,
                    Missing:    []string{hash},
                    InStorage:  true,
                }
            }
        }
    }

    return nil
}

type ChunksMissingError struct {
    ManifestID string
    Missing    []string
    InStorage  bool // True if missing from storage, false if missing from DB
}

func (e *ChunksMissingError) Error() string {
    loc := "database"
    if e.InStorage {
        loc = "object storage"
    }
    return fmt.Sprintf("manifest %s references %d chunks missing from %s",
        e.ManifestID, len(e.Missing), loc)
}
```

### Suspended Session Protection

Long-suspended sessions need special handling:

```go
// Called when session is suspended
func (s *SessionService) Suspend(ctx context.Context, sessionID string) error {
    session, _ := s.store.GetSession(ctx, sessionID)

    // Sync workspace to ensure manifest exists
    if err := s.cas.Sync(ctx, session.WorkspaceID, session.TenantID, workspacePath); err != nil {
        return err
    }

    // Get latest manifest
    manifest, _ := s.store.GetLatestManifest(ctx, session.WorkspaceID)

    // Mark session as suspended with manifest reference
    return s.store.UpdateSession(ctx, sessionID, map[string]interface{}{
        "status":            "suspended",
        "suspended_at":      time.Now(),
        "context_snapshot": map[string]interface{}{
            "manifest_id": manifest.ID,
            // ... other context
        },
    })
}

// Called when session is resumed
func (s *SessionService) Resume(ctx context.Context, sessionID string) error {
    session, _ := s.store.GetSession(ctx, sessionID)

    // Extract manifest ID from context
    manifestID := session.ContextSnapshot["manifest_id"].(string)

    // Validate all chunks still exist
    manifest, _ := s.store.GetManifest(ctx, manifestID)
    if err := s.cas.ValidateManifest(ctx, manifest); err != nil {
        return fmt.Errorf("cannot resume: %w", err)
    }

    // Restore workspace
    return s.cas.Restore(ctx, session.WorkspaceID, session.TenantID, workspacePath)
}
```

---

# Log Storage

Logs use tiered storage: hot data in PostgreSQL, cold data in object storage.

## Strategy

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Tiered Log Storage                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Scale        Volume/day    Recommended                             │
│  ─────────────────────────────────────────────────────────────────  │
│  Small        < 1GB         PostgreSQL only                         │
│  Medium       1-50GB        PostgreSQL + S3 archive                 │
│  Large        > 50GB        Consider ClickHouse/Loki                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘

  Hot (PostgreSQL)  ───7d───>  Cold (S3)  ───90d───>  Delete
  - Partitioned by day         - JSONL.zst compressed
  - Real-time queries          - On-demand retrieval
  - Streaming to clients
```

## Log Archiver Job (Memory-Efficient)

The log archiver uses cursor-based pagination and streaming to avoid loading all logs into memory.

```go
// pkg/jobs/log_archiver.go

type LogArchiver struct {
    store   store.Store
    storage storage.StorageProvider
    crypto  *crypto.Service
    config  LogArchiverConfig
}

type LogArchiverConfig struct {
    RetentionHot  time.Duration // PostgreSQL: 7 days
    RetentionCold time.Duration // S3: 90 days
    BatchSize     int           // Sessions per run
    LogBatchSize  int           // Logs per query (default: 1000)
}

func (j *LogArchiver) Run(ctx context.Context) error {
    // 1. Find sessions to archive (terminated > RetentionHot ago)
    sessions, _ := j.store.GetSessionsForLogArchive(ctx, j.config.RetentionHot)

    for _, session := range sessions {
        if err := j.archiveSessionLogs(ctx, session); err != nil {
            log.Error("failed to archive logs", "session", session.ID, "err", err)
            continue // Don't fail entire job
        }
    }

    // 2. Drop old partitions
    j.dropOldPartitions(ctx)

    // 3. Clean expired S3 archives
    j.cleanupExpiredArchives(ctx)

    return nil
}

// archiveSessionLogs uses streaming to avoid loading all logs into memory
func (j *LogArchiver) archiveSessionLogs(ctx context.Context, session *store.Session) error {
    // Count logs first to check if any exist
    count, err := j.store.CountSessionLogs(ctx, session.ID)
    if err != nil || count == 0 {
        return err
    }

    key := fmt.Sprintf("logs/%s/%s.jsonl.zst", session.TenantID, session.ID)

    // Use io.Pipe for concurrent streaming: DB -> compress -> encrypt -> S3
    pr, pw := io.Pipe()

    var uploadErr error
    uploadDone := make(chan struct{})

    // Uploader goroutine
    go func() {
        defer close(uploadDone)
        uploadErr = j.storage.Upload(ctx, key, pr, storage.UploadOptions{
            ContentType: "application/x-zstd",
        })
    }()

    // Producer: paginate through logs, stream to pipe
    func() {
        defer pw.Close()

        zw, _ := zstd.NewWriter(pw)
        defer zw.Close()

        enc := json.NewEncoder(zw)

        // Cursor-based pagination - never load all logs at once
        var cursor string
        batchSize := j.config.LogBatchSize
        if batchSize == 0 {
            batchSize = 1000
        }

        for {
            // Fetch batch of logs using cursor (sequence-based)
            logs, nextCursor, err := j.store.GetSessionLogsBatch(ctx, session.ID, cursor, batchSize)
            if err != nil {
                log.Error("failed to fetch logs", "session", session.ID, "err", err)
                return
            }

            if len(logs) == 0 {
                break
            }

            // Stream each log (don't accumulate in memory)
            for _, logEntry := range logs {
                if err := enc.Encode(logEntry); err != nil {
                    log.Error("failed to encode log", "err", err)
                    return
                }
            }

            if nextCursor == "" {
                break
            }
            cursor = nextCursor
        }
    }()

    // Wait for upload to complete
    <-uploadDone
    if uploadErr != nil {
        return uploadErr
    }

    // Record archive metadata
    j.store.CreateLogArchive(ctx, &store.LogArchive{
        SessionID:  session.ID,
        TenantID:   session.TenantID,
        StorageKey: key,
        LogCount:   count,
        ExpiresAt:  time.Now().Add(j.config.RetentionCold),
    })

    // Delete logs from PostgreSQL (in batches to avoid long locks)
    return j.store.DeleteSessionLogsBatched(ctx, session.ID, 10000)
}
```

### Store Interface for Paginated Logs

```go
// pkg/store/logs.go

// GetSessionLogsBatch returns logs with cursor-based pagination
// cursor is the last sequence number seen (empty for first page)
// Returns logs, next cursor, error
func (s *Store) GetSessionLogsBatch(
    ctx context.Context,
    sessionID string,
    cursor string,
    limit int,
) ([]Log, string, error) {
    query := `
        SELECT id, task_id, run_id, stream, level, content, sequence,
               timestamp_unix_ms, metadata
        FROM logs
        WHERE session_id = $1
    `
    args := []interface{}{sessionID}

    if cursor != "" {
        query += ` AND sequence > $2`
        args = append(args, cursor)
    }

    query += ` ORDER BY sequence ASC LIMIT $` + strconv.Itoa(len(args)+1)
    args = append(args, limit+1) // Fetch one extra to detect if more exist

    rows, err := s.db.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, "", err
    }
    defer rows.Close()

    var logs []Log
    for rows.Next() {
        var log Log
        if err := rows.Scan(&log.ID, &log.TaskID, &log.RunID, &log.Stream,
            &log.Level, &log.Content, &log.Sequence, &log.TimestampUnixMs,
            &log.Metadata); err != nil {
            return nil, "", err
        }
        logs = append(logs, log)
    }

    // Determine next cursor
    var nextCursor string
    if len(logs) > limit {
        nextCursor = strconv.FormatInt(logs[limit-1].Sequence, 10)
        logs = logs[:limit] // Remove extra element
    }

    return logs, nextCursor, nil
}

// DeleteSessionLogsBatched deletes logs in batches to avoid long locks
func (s *Store) DeleteSessionLogsBatched(ctx context.Context, sessionID string, batchSize int) error {
    for {
        result, err := s.db.ExecContext(ctx, `
            DELETE FROM logs
            WHERE id IN (
                SELECT id FROM logs
                WHERE session_id = $1
                LIMIT $2
            )
        `, sessionID, batchSize)
        if err != nil {
            return err
        }

        affected, _ := result.RowsAffected()
        if affected == 0 {
            break
        }

        // Small sleep to allow other transactions
        time.Sleep(10 * time.Millisecond)
    }
    return nil
}
```

## Retrieving Archived Logs (Streaming)

```go
// GetSessionLogs returns logs, streaming from archive if needed
// For large archives, use GetSessionLogsStream instead
func (s *LogService) GetSessionLogs(ctx context.Context, sessionID string) ([]Log, error) {
    // Try PostgreSQL first (hot data)
    logs, _ := s.store.GetSessionLogs(ctx, sessionID)
    if len(logs) > 0 {
        return logs, nil
    }

    // Check archive (cold data)
    archive, _ := s.store.GetLogArchive(ctx, sessionID)
    if archive == nil {
        return []Log{}, nil
    }

    // Stream from archive with memory limit
    return s.streamLogsFromArchive(ctx, archive, 0) // 0 = no limit
}

// GetSessionLogsStream returns a channel for streaming logs (memory-efficient)
func (s *LogService) GetSessionLogsStream(ctx context.Context, sessionID string) (<-chan Log, error) {
    archive, err := s.store.GetLogArchive(ctx, sessionID)
    if err != nil {
        return nil, err
    }
    if archive == nil {
        // Return empty channel
        ch := make(chan Log)
        close(ch)
        return ch, nil
    }

    ch := make(chan Log, 100) // Buffered for performance

    go func() {
        defer close(ch)

        reader, _, err := s.storage.Download(ctx, archive.StorageKey)
        if err != nil {
            return
        }
        defer reader.Close()

        zr, _ := zstd.NewReader(reader)
        defer zr.Close()

        scanner := bufio.NewScanner(zr)
        // Increase buffer for large log lines
        scanner.Buffer(make([]byte, 64*1024), 1024*1024)

        for scanner.Scan() {
            var log Log
            if err := json.Unmarshal(scanner.Bytes(), &log); err != nil {
                continue
            }

            select {
            case ch <- log:
            case <-ctx.Done():
                return
            }
        }
    }()

    return ch, nil
}

// streamLogsFromArchive reads logs with optional limit
func (s *LogService) streamLogsFromArchive(ctx context.Context, archive *LogArchive, limit int) ([]Log, error) {
    reader, _, err := s.storage.Download(ctx, archive.StorageKey)
    if err != nil {
        return nil, err
    }
    defer reader.Close()

    zr, _ := zstd.NewReader(reader)
    defer zr.Close()

    var logs []Log
    scanner := bufio.NewScanner(zr)
    scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line

    for scanner.Scan() {
        var log Log
        if err := json.Unmarshal(scanner.Bytes(), &log); err != nil {
            continue
        }
        logs = append(logs, log)

        if limit > 0 && len(logs) >= limit {
            break
        }
    }

    return logs, scanner.Err()
}
```

## Configuration

```yaml
logs:
  retention_hot: 168h   # 7 days in PostgreSQL
  retention_cold: 2160h # 90 days in S3
  archive_schedule: "0 3 * * *"  # Daily at 3 AM
  partitions_ahead: 7   # Create 7 days ahead
```

## Security Layers

| Layer | Protection | Description |
|-------|------------|-------------|
| TLS | Transport | HTTPS / gRPC TLS |
| S3 SSE | At-rest (optional) | AWS managed |
| AES-256-GCM | At-rest | Application-level (per-tenant) |
| KEK/DEK | Key isolation | Per-tenant keys |

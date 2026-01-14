# ID Generation

Marionette uses Stripe-style prefixed IDs for all resources.

## Format

```
{prefix}_{base62_timestamp}{nanoid}
         └──────────────────────────┘
              16 characters total
```

**Total length:** ~21 characters

### Components

| Component | Size | Description |
|-----------|------|-------------|
| Prefix | 2-5 chars | Resource type identifier |
| Separator | 1 char | Underscore `_` |
| Timestamp | 8 chars | Base62-encoded milliseconds |
| Random | 8 chars | Nanoid random characters |

### Example

```
sess_0002xK9mNpV1StGXR8
│    │       │
│    │       └── nanoid (8 chars, random)
│    └── base62 timestamp (8 chars, time-ordered)
└── prefix (identifies resource type)
```

## Prefixes

| Prefix | Resource | Example |
|--------|----------|---------|
| `run_` | Runner | `run_0002xK9mNoT0RsEWQ7` |
| `sess_` | Session | `sess_0002xK9mNpV1StGXR8` |
| `task_` | Task | `task_0002xK9mNqW2TuHYS9` |
| `trun_` | Task Run | `trun_0002xK9mNrX3UvIZT0` |
| `stsk_` | Scheduled Task | `stsk_0002xK9mNsY4VwJaU1` |
| `perm_` | Permission Request | `perm_0002xK9mNtZ5WxKbV2` |
| `ws_` | Workspace | `ws_0002xK9mNuA6XyLcW3` |
| `key_` | API Key | `key_0002xK9mNvB7YzMdX4` |
| `rtok_` | Runner Token | `rtok_0002xK9mNwC8ZaNfY5` |
| `ttok_` | Tunnel Token | `ttok_0002xK9mNxD9AbOgZ6` |
| `dek_` | Data Key | `dek_0002xK9mNyE0BcPhA7` |
| `arch_` | Log Archive | `arch_0002xK9mNzF1CdQiB8` |
| `acfg_` | Agent Config | `acfg_0002xK9mOaG2DeRjC9` |
| `pcfg_` | Provider Config | `pcfg_0002xK9mObH3EfSkD0` |
| `prof_` | Profile | `prof_0002xK9mOcI4FgTlE1` |
| `snap_` | Snapshot | `snap_0002xK9mOdJ5GhUmF2` |
| `tun_` | Tunnel | `tun_0002xK9mOeK6HiVnG3` |
| `mfst_` | Manifest | `mfst_0002xK9mOfL7IjWoH4` |
| `alog_` | Action Log | `alog_0002xK9mOgM8JkXpI5` |

## Benefits

### Human-Readable

Type is visible in the ID itself:

```
sess_0002xK9mNpV1StGXR8  // Clearly a session
task_0002xK9mNqW2TuHYS9  // Clearly a task
```

### Time-Ordered

Base62 timestamp ensures lexicographic ordering matches chronological ordering:

```sql
-- These are equivalent
ORDER BY id ASC
ORDER BY created_at ASC
```

### Short

~21 characters vs UUID's 36:

```
sess_0002xK9mNpV1StGXR8     -- 21 chars
550e8400-e29b-41d4-a716-446655440000  -- 36 chars
```

### URL-Safe

No special characters (only alphanumeric and underscore).

### Collision-Resistant

- 8-char timestamp: unique per millisecond
- 8-char nanoid: 62^8 = 218 trillion combinations per ms

## Usage

### Go Package

```go
import "github.com/chunlea/marionette/pkg/id"

// Generate new ID
sessionID := id.New("sess")  // sess_0002xK9mNpV1StGXR8

// Parse ID
prefix, err := id.ParsePrefix(sessionID)  // "sess"

// Validate ID
if err := id.Validate(sessionID, "sess"); err != nil {
    // Invalid ID or wrong prefix
}
```

### Database Indexes

IDs work well with B-tree indexes:

```sql
CREATE INDEX idx_sessions_id ON sessions(id);
-- Efficient range queries due to time-ordering
SELECT * FROM sessions WHERE id > 'sess_0002xK' AND id < 'sess_0002xL';
```

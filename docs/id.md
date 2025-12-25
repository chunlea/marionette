# ID Generation

Marionette uses Stripe-style prefixed IDs for all resources.

## Format

```
{prefix}_{base62_timestamp}{nanoid}
```

Example: `sess_0002xK9mNpV1StGXR8`

### Components

| Part | Description | Length |
|------|-------------|--------|
| prefix | Resource type identifier | 2-4 chars |
| `_` | Separator | 1 char |
| timestamp | Base62-encoded milliseconds since epoch | 8 chars (zero-padded) |
| nanoid | Random string (base62 alphabet) | 8 chars |

Total: ~21 characters

### Time Ordering

The timestamp component uses **fixed-width zero-padded base62** encoding to ensure:
- Lexicographic order equals chronological order
- BTREE indexes maintain time-based clustering
- IDs remain sortable without parsing

Without padding, `9` (single digit) would sort after `10` (two digits) lexicographically, breaking time ordering.

8 characters of base62 can represent up to `62^8 = 218,340,105,584,896` milliseconds (~6,900 years from epoch), which is sufficient for any practical use.

### Prefixes

| Prefix | Resource |
|--------|----------|
| `run_` | Runner |
| `sess_` | Session |
| `task_` | Task |
| `trun_` | Task Run |
| `stsk_` | Scheduled Task |
| `perm_` | Permission Request |
| `ws_` | Workspace |
| `key_` | API Key |
| `rtok_` | Runner Token |
| `dek_` | Data Key |
| `log_` | Log Entry |
| `arch_` | Log Archive |
| `acfg_` | Agent Config |
| `pcfg_` | Provider Config |
| `prof_` | Profile |
| `snap_` | Snapshot |
| `tun_` | Tunnel |
| `mfst_` | Manifest |
| `alog_` | Action Log |

## Benefits

- **Human-readable**: Type visible in ID (`sess_xxx` vs UUID)
- **Time-ordered**: Fixed-width base62 ensures lexicographic = chronological order
- **Short**: ~21 chars vs UUID's 36
- **URL-safe**: No special characters
- **Debuggable**: Easy to identify in logs

## Implementation

```go
// pkg/id/id.go
package id

import (
    "time"

    nanoid "github.com/matoous/go-nanoid/v2"
)

const (
    alphabet      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
    timestampLen  = 8  // Fixed width for proper lexicographic ordering
)

// New generates a time-ordered prefixed ID
func New(prefix string) string {
    ts := encodeBase62Padded(time.Now().UnixMilli(), timestampLen)
    rand, _ := nanoid.Generate(alphabet, 8)
    return prefix + "_" + ts + rand
}

// encodeBase62Padded converts an int64 to fixed-width base62 string
// Zero-pads to ensure lexicographic order equals chronological order
func encodeBase62Padded(n int64, width int) string {
    result := make([]byte, width)
    for i := width - 1; i >= 0; i-- {
        result[i] = alphabet[n%62]
        n /= 62
    }
    return string(result)
}

// Convenience functions
func Session() string           { return New("sess") }
func Task() string              { return New("task") }
func TaskRun() string           { return New("trun") }
func ScheduledTask() string     { return New("stsk") }
func PermissionRequest() string { return New("perm") }
func Runner() string            { return New("run") }
func Workspace() string         { return New("ws") }
func APIKey() string            { return New("key") }
func RunnerToken() string       { return New("rtok") }
func DataKey() string           { return New("dek") }
func Log() string               { return New("log") }
func LogArchive() string        { return New("arch") }
func ActionLog() string         { return New("alog") }
func AgentConfig() string       { return New("acfg") }
func ProviderConfig() string    { return New("pcfg") }
func Profile() string           { return New("prof") }
func Snapshot() string          { return New("snap") }
func Tunnel() string            { return New("tun") }
func Manifest() string          { return New("mfst") }
```

## Validation

```go
// pkg/id/validate.go
package id

import (
    "errors"
    "strings"
    "time"
)

var ErrInvalidID = errors.New("invalid id format")

// Parse extracts prefix and value from an ID
func Parse(id string) (prefix, value string, err error) {
    parts := strings.SplitN(id, "_", 2)
    if len(parts) != 2 {
        return "", "", ErrInvalidID
    }
    return parts[0], parts[1], nil
}

// ExtractTime extracts the timestamp from an ID
// Returns zero time if parsing fails
func ExtractTime(id string) time.Time {
    _, value, err := Parse(id)
    if err != nil || len(value) < timestampLen {
        return time.Time{}
    }
    ts := decodeBase62(value[:timestampLen])
    return time.UnixMilli(ts)
}

// decodeBase62 converts a base62 string back to int64
func decodeBase62(s string) int64 {
    var n int64
    for _, c := range s {
        n *= 62
        switch {
        case c >= '0' && c <= '9':
            n += int64(c - '0')
        case c >= 'A' && c <= 'Z':
            n += int64(c - 'A' + 10)
        case c >= 'a' && c <= 'z':
            n += int64(c - 'a' + 36)
        }
    }
    return n
}

// IsSession returns true if id is a session ID
func IsSession(id string) bool {
    return strings.HasPrefix(id, "sess_")
}

// IsTask returns true if id is a task ID
func IsTask(id string) bool {
    return strings.HasPrefix(id, "task_")
}

// IsRunner returns true if id is a runner ID
func IsRunner(id string) bool {
    return strings.HasPrefix(id, "run_")
}

// IsWorkspace returns true if id is a workspace ID
func IsWorkspace(id string) bool {
    return strings.HasPrefix(id, "ws_")
}

// IsManifest returns true if id is a manifest ID
func IsManifest(id string) bool {
    return strings.HasPrefix(id, "mfst_")
}
```

## Testing

```go
// pkg/id/id_test.go
package id

import (
    "testing"
    "time"
)

func TestTimeOrdering(t *testing.T) {
    // Generate IDs across time
    id1 := Session()
    time.Sleep(1 * time.Millisecond)
    id2 := Session()
    time.Sleep(1 * time.Millisecond)
    id3 := Session()

    // Lexicographic order should equal chronological order
    if id1 >= id2 || id2 >= id3 {
        t.Errorf("IDs not in chronological order: %s, %s, %s", id1, id2, id3)
    }
}

func TestExtractTime(t *testing.T) {
    before := time.Now()
    id := Session()
    after := time.Now()

    extracted := ExtractTime(id)
    if extracted.Before(before.Truncate(time.Millisecond)) || extracted.After(after) {
        t.Errorf("Extracted time %v not between %v and %v", extracted, before, after)
    }
}

func TestPaddingWidth(t *testing.T) {
    id := Session()
    _, value, _ := Parse(id)

    // Should always be 16 chars (8 timestamp + 8 random)
    if len(value) != 16 {
        t.Errorf("Expected value length 16, got %d: %s", len(value), value)
    }
}
```

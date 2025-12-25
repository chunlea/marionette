# ID Generation

Marionette uses Stripe-style prefixed IDs for all resources.

## Format

```
{prefix}_{timestamp}{random}
```

Example: `sess_BlShKHtNJr5K5iRW`

### Components

| Part | Description | Length |
|------|-------------|--------|
| prefix | Resource type identifier | 2-4 chars |
| `_` | Separator | 1 char |
| timestamp | Base52-encoded milliseconds since epoch (letters only) | 8 chars |
| random | Random string (base62 alphabet) | 8 chars |

Total: ~21 characters

### Time Ordering

The timestamp component uses **letters-only base52** encoding (A-Za-z) to ensure:
- Lexicographic order equals chronological order
- BTREE indexes maintain time-based clustering
- IDs remain sortable without parsing
- No ugly leading zeros (current timestamps start with 'B')

8 characters of base52 can represent up to `52^8 = 53,459,728,531,456` milliseconds (~1,700 years from epoch), which is sufficient for any practical use.

### Why Letters Only?

With base62 (including digits), current Unix timestamps (~1.7 trillion ms) produce IDs like `sess_0000UXpT...` with leading zeros. Using base52 (letters only), the same timestamp produces `sess_BlShKH...` - much cleaner.

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
| `ttok_` | Tunnel Token |
| `mfst_` | Manifest |
| `alog_` | Action Log |

## Benefits

- **Human-readable**: Type visible in ID (`sess_xxx` vs UUID)
- **Time-ordered**: Fixed-width base52 ensures lexicographic = chronological order
- **Short**: ~21 chars vs UUID's 36
- **URL-safe**: No special characters
- **Debuggable**: Easy to identify in logs
- **Clean**: No leading zeros

## Implementation

```go
// pkg/id/id.go
package id

import (
    "time"

    nanoid "github.com/matoous/go-nanoid/v2"
)

const (
    // timestampAlphabet uses letters only (base52) for timestamp encoding.
    // This avoids leading zeros since current Unix milliseconds (~1.7 trillion)
    // starts with 'B' in base52, making IDs more readable.
    timestampAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

    // randomAlphabet uses full base62 for the random suffix.
    randomAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

    timestampLen = 8
    randomLen    = 8
)

// New generates a time-ordered prefixed ID
func New(prefix string) string {
    ts := encodeTimestamp(time.Now().UnixMilli())
    rand, _ := nanoid.Generate(randomAlphabet, randomLen)
    return prefix + "_" + ts + rand
}

// encodeTimestamp converts milliseconds to a fixed-width base52 string.
func encodeTimestamp(n int64) string {
    const base = 52
    result := make([]byte, timestampLen)
    for i := timestampLen - 1; i >= 0; i-- {
        result[i] = timestampAlphabet[n%base]
        n /= base
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
func TunnelToken() string       { return New("ttok") }
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
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return "", "", ErrInvalidID
    }
    return parts[0], parts[1], nil
}

// ExtractTime extracts the timestamp from an ID
func ExtractTime(id string) time.Time {
    _, value, err := Parse(id)
    if err != nil || len(value) < timestampLen {
        return time.Time{}
    }
    ts := decodeTimestamp(value[:timestampLen])
    return time.UnixMilli(ts)
}

// decodeTimestamp converts a base52 timestamp string back to int64.
func decodeTimestamp(s string) int64 {
    const base = 52
    var n int64
    for _, c := range s {
        n *= base
        switch {
        case c >= 'A' && c <= 'Z':
            n += int64(c - 'A')
        case c >= 'a' && c <= 'z':
            n += int64(c - 'a' + 26)
        }
    }
    return n
}

// Type checking functions
func IsSession(id string) bool   { return strings.HasPrefix(id, "sess_") }
func IsTask(id string) bool      { return strings.HasPrefix(id, "task_") }
func IsRunner(id string) bool    { return strings.HasPrefix(id, "run_") }
func IsWorkspace(id string) bool { return strings.HasPrefix(id, "ws_") }
func IsManifest(id string) bool  { return strings.HasPrefix(id, "mfst_") }
// ... and more for each resource type
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
    id1 := Session()
    time.Sleep(2 * time.Millisecond)
    id2 := Session()
    time.Sleep(2 * time.Millisecond)
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

func TestBase52Roundtrip(t *testing.T) {
    tests := []int64{0, 1, 51, 52, 2703, 2704, 1000000, time.Now().UnixMilli()}
    for _, tt := range tests {
        encoded := encodeTimestamp(tt)
        decoded := decodeTimestamp(encoded)
        if tt != decoded {
            t.Errorf("roundtrip failed for %d: encoded=%s, decoded=%d", tt, encoded, decoded)
        }
    }
}
```

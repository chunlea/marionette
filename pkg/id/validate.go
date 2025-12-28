package id

import (
	"errors"
	"strings"
	"time"
)

// ErrInvalidID is returned when an ID does not match the expected format.
var ErrInvalidID = errors.New("invalid id format")

// Parse extracts the prefix and value from an ID.
// Returns ErrInvalidID if the ID does not contain exactly one underscore separator.
func Parse(id string) (prefix, value string, err error) {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", ErrInvalidID
	}
	return parts[0], parts[1], nil
}

// ExtractTime extracts the timestamp from an ID.
// Returns zero time if parsing fails or the ID is malformed.
func ExtractTime(id string) time.Time {
	_, value, err := Parse(id)
	if err != nil || len(value) < timestampLen {
		return time.Time{}
	}
	ts := decodeTimestamp(value[:timestampLen])
	return time.UnixMilli(ts)
}

// decodeTimestamp converts a base52 timestamp string back to int64.
// The alphabet is A-Za-z (letters only).
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

// Type checking functions for each resource type.

// IsSession returns true if id is a session ID.
func IsSession(id string) bool {
	return strings.HasPrefix(id, "sess_")
}

// IsTask returns true if id is a task ID.
func IsTask(id string) bool {
	return strings.HasPrefix(id, "task_")
}

// IsTaskRun returns true if id is a task run ID.
func IsTaskRun(id string) bool {
	return strings.HasPrefix(id, "trun_")
}

// IsScheduledTask returns true if id is a scheduled task ID.
func IsScheduledTask(id string) bool {
	return strings.HasPrefix(id, "stsk_")
}

// IsPermissionRequest returns true if id is a permission request ID.
func IsPermissionRequest(id string) bool {
	return strings.HasPrefix(id, "perm_")
}

// IsRunner returns true if id is a runner ID.
func IsRunner(id string) bool {
	return strings.HasPrefix(id, "run_")
}

// IsWorkspace returns true if id is a workspace ID.
func IsWorkspace(id string) bool {
	return strings.HasPrefix(id, "ws_")
}

// IsAPIKey returns true if id is an API key ID.
func IsAPIKey(id string) bool {
	return strings.HasPrefix(id, "key_")
}

// IsRunnerToken returns true if id is a runner token ID.
func IsRunnerToken(id string) bool {
	return strings.HasPrefix(id, "rtok_")
}

// IsDataKey returns true if id is a data key ID.
func IsDataKey(id string) bool {
	return strings.HasPrefix(id, "dek_")
}

// IsLog returns true if id is a log entry ID.
// Deprecated: Use IsRawLog instead.
func IsLog(id string) bool {
	return strings.HasPrefix(id, "log_") || strings.HasPrefix(id, "rlog_")
}

// IsRawLog returns true if id is a raw log entry ID.
func IsRawLog(id string) bool {
	return strings.HasPrefix(id, "rlog_")
}

// IsAgentEvent returns true if id is an agent event ID.
func IsAgentEvent(id string) bool {
	return strings.HasPrefix(id, "evt_")
}

// IsLogArchive returns true if id is a log archive ID.
func IsLogArchive(id string) bool {
	return strings.HasPrefix(id, "arch_")
}

// IsActionLog returns true if id is an action log ID.
func IsActionLog(id string) bool {
	return strings.HasPrefix(id, "alog_")
}

// IsAgentConfig returns true if id is an agent config ID.
func IsAgentConfig(id string) bool {
	return strings.HasPrefix(id, "acfg_")
}

// IsProviderConfig returns true if id is a provider config ID.
func IsProviderConfig(id string) bool {
	return strings.HasPrefix(id, "pcfg_")
}

// IsProfile returns true if id is a profile ID.
func IsProfile(id string) bool {
	return strings.HasPrefix(id, "prof_")
}

// IsSnapshot returns true if id is a snapshot ID.
func IsSnapshot(id string) bool {
	return strings.HasPrefix(id, "snap_")
}

// IsTunnel returns true if id is a tunnel ID.
func IsTunnel(id string) bool {
	return strings.HasPrefix(id, "tun_")
}

// IsTunnelToken returns true if id is a tunnel token ID.
func IsTunnelToken(id string) bool {
	return strings.HasPrefix(id, "ttok_")
}

// IsManifest returns true if id is a manifest ID.
func IsManifest(id string) bool {
	return strings.HasPrefix(id, "mfst_")
}

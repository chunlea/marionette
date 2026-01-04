// Package id provides Stripe-style prefixed ID generation for Marionette resources.
//
// IDs follow the format: {prefix}_{timestamp}{random}
// Example: sess_BxKmNpVq1StGXR8a
//
// The timestamp component uses letters-only base52 encoding (A-Za-z) to ensure
// lexicographic order equals chronological order, enabling efficient BTREE indexing.
// Using letters avoids ugly leading zeros that occur with base62.
package id

import (
	"time"

	nanoid "github.com/matoous/go-nanoid/v2"
)

const (
	// timestampAlphabet uses letters only (base52) for timestamp encoding.
	// This avoids leading zeros since current Unix milliseconds (~1.7 trillion)
	// starts with 'B' in base52, making IDs more readable.
	// 52^8 = 53,459,728,531,456 milliseconds (~1,700 years from epoch).
	timestampAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// randomAlphabet uses full base62 for the random suffix.
	randomAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// timestampLen is the fixed width for the base52 timestamp component.
	timestampLen = 8

	// randomLen is the length of the random nanoid suffix.
	randomLen = 8
)

// New generates a time-ordered prefixed ID.
// The returned ID has the format: {prefix}_{timestamp}{random}
func New(prefix string) string {
	ts := encodeTimestamp(time.Now().UnixMilli())
	rand, _ := nanoid.Generate(randomAlphabet, randomLen)
	return prefix + "_" + ts + rand
}

// encodeTimestamp converts milliseconds to a fixed-width base52 string.
// Uses letters only (A-Za-z) to avoid leading zeros.
func encodeTimestamp(n int64) string {
	const base = 52
	result := make([]byte, timestampLen)
	for i := timestampLen - 1; i >= 0; i-- {
		result[i] = timestampAlphabet[n%base]
		n /= base
	}
	return string(result)
}

// Convenience functions for generating IDs for each resource type.
// These match the prefixes defined in docs/schema.sql.

// Session generates a session ID (prefix: sess_).
func Session() string { return New("sess") }

// Task generates a task ID (prefix: task_).
func Task() string { return New("task") }

// TaskRun generates a task run ID (prefix: trun_).
func TaskRun() string { return New("trun") }

// ScheduledTask generates a scheduled task ID (prefix: stsk_).
func ScheduledTask() string { return New("stsk") }

// PermissionRequest generates a permission request ID (prefix: perm_).
func PermissionRequest() string { return New("perm") }

// Runner generates a runner ID (prefix: run_).
func Runner() string { return New("run") }

// Workspace generates a workspace ID (prefix: ws_).
func Workspace() string { return New("ws") }

// APIKey generates an API key ID (prefix: key_).
func APIKey() string { return New("key") }

// RunnerToken generates a runner token ID (prefix: rtok_).
func RunnerToken() string { return New("rtok") }

// DataKey generates a data key ID (prefix: dek_).
func DataKey() string { return New("dek") }

// Log generates a log entry ID (prefix: log_).
func Log() string { return New("log") }

// RawLog generates a raw log entry ID (prefix: rlog_).
func RawLog() string { return New("rlog") }

// LogArchive generates a log archive ID (prefix: arch_).
func LogArchive() string { return New("arch") }

// ActionLog generates an action log ID (prefix: alog_).
func ActionLog() string { return New("alog") }

// AgentConfig generates an agent config ID (prefix: acfg_).
func AgentConfig() string { return New("acfg") }

// ProviderConfig generates a provider config ID (prefix: pcfg_).
func ProviderConfig() string { return New("pcfg") }

// Profile generates a profile ID (prefix: prof_).
func Profile() string { return New("prof") }

// Snapshot generates a snapshot ID (prefix: snap_).
func Snapshot() string { return New("snap") }

// Tunnel generates a tunnel ID (prefix: tun_).
func Tunnel() string { return New("tun") }

// TunnelToken generates a tunnel token ID (prefix: ttok_).
func TunnelToken() string { return New("ttok") }

// Manifest generates a manifest ID (prefix: mfst_).
func Manifest() string { return New("mfst") }

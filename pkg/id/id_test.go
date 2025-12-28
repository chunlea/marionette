package id

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	id := New("test")

	// Should have correct prefix
	assert.True(t, strings.HasPrefix(id, "test_"))

	// Should have correct format: prefix_timestampRandom
	prefix, value, err := Parse(id)
	require.NoError(t, err)
	assert.Equal(t, "test", prefix)
	assert.Len(t, value, 16) // 8 timestamp + 8 random
}

func TestTimeOrdering(t *testing.T) {
	// Generate IDs across time
	id1 := Session()
	time.Sleep(2 * time.Millisecond)
	id2 := Session()
	time.Sleep(2 * time.Millisecond)
	id3 := Session()

	// Lexicographic order should equal chronological order
	assert.Less(t, id1, id2, "id1 should be less than id2")
	assert.Less(t, id2, id3, "id2 should be less than id3")
}

func TestExtractTime(t *testing.T) {
	before := time.Now()
	id := Session()
	after := time.Now()

	extracted := ExtractTime(id)

	// Extracted time should be between before and after
	// Truncate before to millisecond precision since IDs use milliseconds
	assert.False(t, extracted.Before(before.Truncate(time.Millisecond)),
		"extracted time should not be before generation time")
	assert.False(t, extracted.After(after),
		"extracted time should not be after generation completed")
}

func TestExtractTimeInvalid(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no underscore", "sessabc123"},
		{"empty prefix", "_abc123"},
		{"empty value", "sess_"},
		{"short value", "sess_abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extracted := ExtractTime(tt.id)
			assert.True(t, extracted.IsZero(), "should return zero time for invalid ID")
		})
	}
}

func TestPaddingWidth(t *testing.T) {
	// Generate many IDs and ensure consistent length
	for i := 0; i < 100; i++ {
		id := Session()
		_, value, err := Parse(id)
		require.NoError(t, err)

		// Should always be 16 chars (8 timestamp + 8 random)
		assert.Len(t, value, 16, "value should always be 16 characters")
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		wantPrefix string
		wantValue  string
		wantErr    bool
	}{
		{
			name:       "valid session id",
			id:         "sess_0002xK9mNpV1StGX",
			wantPrefix: "sess",
			wantValue:  "0002xK9mNpV1StGX",
			wantErr:    false,
		},
		{
			name:       "valid short prefix",
			id:         "ws_abc123def456ghij",
			wantPrefix: "ws",
			wantValue:  "abc123def456ghij",
			wantErr:    false,
		},
		{
			name:       "valid long prefix",
			id:         "acfg_1234567890abcdef",
			wantPrefix: "acfg",
			wantValue:  "1234567890abcdef",
			wantErr:    false,
		},
		{
			name:    "empty string",
			id:      "",
			wantErr: true,
		},
		{
			name:    "no underscore",
			id:      "sessabc123",
			wantErr: true,
		},
		{
			name:    "empty prefix",
			id:      "_abc123",
			wantErr: true,
		},
		{
			name:    "empty value",
			id:      "sess_",
			wantErr: true,
		},
		{
			name:       "multiple underscores",
			id:         "sess_abc_def_123",
			wantPrefix: "sess",
			wantValue:  "abc_def_123",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, value, err := Parse(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, ErrInvalidID)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPrefix, prefix)
				assert.Equal(t, tt.wantValue, value)
			}
		})
	}
}

func TestConvenienceFunctions(t *testing.T) {
	tests := []struct {
		name       string
		fn         func() string
		wantPrefix string
		checkFn    func(string) bool
	}{
		{"Session", Session, "sess_", IsSession},
		{"Task", Task, "task_", IsTask},
		{"TaskRun", TaskRun, "trun_", IsTaskRun},
		{"ScheduledTask", ScheduledTask, "stsk_", IsScheduledTask},
		{"PermissionRequest", PermissionRequest, "perm_", IsPermissionRequest},
		{"Runner", Runner, "run_", IsRunner},
		{"Workspace", Workspace, "ws_", IsWorkspace},
		{"APIKey", APIKey, "key_", IsAPIKey},
		{"RunnerToken", RunnerToken, "rtok_", IsRunnerToken},
		{"DataKey", DataKey, "dek_", IsDataKey},
		{"RawLog", RawLog, "rlog_", IsRawLog},
		{"LogArchive", LogArchive, "arch_", IsLogArchive},
		{"ActionLog", ActionLog, "alog_", IsActionLog},
		{"AgentConfig", AgentConfig, "acfg_", IsAgentConfig},
		{"ProviderConfig", ProviderConfig, "pcfg_", IsProviderConfig},
		{"Profile", Profile, "prof_", IsProfile},
		{"Snapshot", Snapshot, "snap_", IsSnapshot},
		{"Tunnel", Tunnel, "tun_", IsTunnel},
		{"TunnelToken", TunnelToken, "ttok_", IsTunnelToken},
		{"Manifest", Manifest, "mfst_", IsManifest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.fn()

			// Check prefix
			assert.True(t, strings.HasPrefix(id, tt.wantPrefix),
				"ID should have prefix %s, got %s", tt.wantPrefix, id)

			// Check type detection function
			assert.True(t, tt.checkFn(id),
				"Type check function should return true for generated ID")

			// Check format
			_, value, err := Parse(id)
			require.NoError(t, err)
			assert.Len(t, value, 16, "value should be 16 characters")
		})
	}
}

func TestTypeCheckingNegative(t *testing.T) {
	// Generate a session ID
	sessionID := Session()

	// It should not match other types
	assert.False(t, IsTask(sessionID))
	assert.False(t, IsRunner(sessionID))
	assert.False(t, IsWorkspace(sessionID))
	assert.False(t, IsManifest(sessionID))
}

func TestBase52Encoding(t *testing.T) {
	// Test specific values to ensure encoding is correct
	// Base52 alphabet: A-Z (0-25), a-z (26-51)
	tests := []struct {
		input int64
		want  string
	}{
		{0, "AAAAAAAA"},
		{25, "AAAAAAAZ"},
		{26, "AAAAAAAa"},
		{51, "AAAAAAAz"},
		{52, "AAAAAABA"},
		{2703, "AAAAAAzz"}, // 52*52 - 1
		{2704, "AAAAABAA"}, // 52*52
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			result := encodeTimestamp(tt.input)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestBase52Decoding(t *testing.T) {
	// Test roundtrip
	tests := []int64{0, 1, 25, 26, 51, 52, 2703, 2704, 1000000, time.Now().UnixMilli()}

	for _, tt := range tests {
		encoded := encodeTimestamp(tt)
		decoded := decodeTimestamp(encoded)
		assert.Equal(t, tt, decoded, "roundtrip failed for %d", tt)
	}
}

func TestUniqueness(t *testing.T) {
	// Generate many IDs and ensure they're unique
	seen := make(map[string]bool)
	count := 10000

	for i := 0; i < count; i++ {
		id := Session()
		assert.False(t, seen[id], "duplicate ID generated: %s", id)
		seen[id] = true
	}
}

func TestIDCharacters(t *testing.T) {
	// All generated IDs should only contain valid characters
	validChars := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_"

	for i := 0; i < 100; i++ {
		id := Session()
		for _, c := range id {
			assert.True(t, strings.ContainsRune(validChars, c),
				"ID contains invalid character: %c in %s", c, id)
		}
	}
}

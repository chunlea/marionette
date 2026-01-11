package streaming

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		st       StreamType
		expected bool
	}{
		{"desktop is valid", StreamTypeDesktop, true},
		{"browser is valid", StreamTypeBrowser, true},
		{"ios is valid", StreamTypeIOS, true},
		{"android is valid", StreamTypeAndroid, true},
		{"empty is invalid", StreamType(""), false},
		{"unknown is invalid", StreamType("unknown"), false},
		{"random is invalid", StreamType("random123"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.st.IsValid())
		})
	}
}

func TestStreamType_String(t *testing.T) {
	assert.Equal(t, "desktop", StreamTypeDesktop.String())
	assert.Equal(t, "browser", StreamTypeBrowser.String())
	assert.Equal(t, "ios", StreamTypeIOS.String())
	assert.Equal(t, "android", StreamTypeAndroid.String())
}

func TestStreamState_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		ss       StreamState
		expected bool
	}{
		{"pending is valid", StreamStatePending, true},
		{"starting is valid", StreamStateStarting, true},
		{"active is valid", StreamStateActive, true},
		{"paused is valid", StreamStatePaused, true},
		{"stopping is valid", StreamStateStopping, true},
		{"stopped is valid", StreamStateStopped, true},
		{"error is valid", StreamStateError, true},
		{"empty is invalid", StreamState(""), false},
		{"unknown is invalid", StreamState("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ss.IsValid())
		})
	}
}

func TestStreamState_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		ss       StreamState
		expected bool
	}{
		{"pending is not terminal", StreamStatePending, false},
		{"starting is not terminal", StreamStateStarting, false},
		{"active is not terminal", StreamStateActive, false},
		{"paused is not terminal", StreamStatePaused, false},
		{"stopping is not terminal", StreamStateStopping, false},
		{"stopped is terminal", StreamStateStopped, true},
		{"error is terminal", StreamStateError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ss.IsTerminal())
		})
	}
}

func TestStreamState_IsActive(t *testing.T) {
	tests := []struct {
		name     string
		ss       StreamState
		expected bool
	}{
		{"pending is not active", StreamStatePending, false},
		{"starting is not active", StreamStateStarting, false},
		{"active is active", StreamStateActive, true},
		{"paused is active", StreamStatePaused, true},
		{"stopping is not active", StreamStateStopping, false},
		{"stopped is not active", StreamStateStopped, false},
		{"error is not active", StreamStateError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ss.IsActive())
		})
	}
}

func TestStreamState_String(t *testing.T) {
	assert.Equal(t, "pending", StreamStatePending.String())
	assert.Equal(t, "active", StreamStateActive.String())
	assert.Equal(t, "stopped", StreamStateStopped.String())
}

func TestResolution_IsZero(t *testing.T) {
	tests := []struct {
		name     string
		r        Resolution
		expected bool
	}{
		{"zero resolution", Resolution{0, 0}, true},
		{"non-zero width", Resolution{1920, 0}, false},
		{"non-zero height", Resolution{0, 1080}, false},
		{"non-zero both", Resolution{1920, 1080}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.r.IsZero())
		})
	}
}

func TestStreamOptions_Validate(t *testing.T) {
	tests := []struct {
		name      string
		opts      StreamOptions
		expectErr error
	}{
		{
			name: "valid options",
			opts: StreamOptions{
				SessionID: "sess_123",
				Type:      StreamTypeDesktop,
			},
			expectErr: nil,
		},
		{
			name: "missing session ID",
			opts: StreamOptions{
				Type: StreamTypeDesktop,
			},
			expectErr: ErrSessionRequired,
		},
		{
			name: "invalid stream type",
			opts: StreamOptions{
				SessionID: "sess_123",
				Type:      StreamType("invalid"),
			},
			expectErr: ErrInvalidStreamType,
		},
		{
			name: "empty stream type",
			opts: StreamOptions{
				SessionID: "sess_123",
				Type:      StreamType(""),
			},
			expectErr: ErrInvalidStreamType,
		},
		{
			name: "full valid options",
			opts: StreamOptions{
				SessionID:    "sess_123",
				RunnerID:     "run_456",
				TenantID:     "tenant_789",
				Type:         StreamTypeAndroid,
				Resolution:   Resolution{1920, 1080},
				FrameRate:    60,
				BitRate:      8000000,
				AudioEnabled: true,
				InputEnabled: true,
				ICEServers: []ICEServer{
					{URLs: []string{"stun:stun.l.google.com:19302"}},
				},
				Metadata: map[string]string{"key": "value"},
			},
			expectErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.expectErr != nil {
				assert.ErrorIs(t, err, tt.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStream_Fields(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour)

	stream := Stream{
		ID:               "strm_123",
		SessionID:        "sess_456",
		RunnerID:         "run_789",
		TenantID:         "tenant_abc",
		Type:             StreamTypeDesktop,
		State:            StreamStateActive,
		SignalingURL:     "ws://localhost:8080/signaling",
		ICEServers:       []ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
		Resolution:       Resolution{1920, 1080},
		FrameRate:        60,
		BitRate:          8000000,
		VideoCodec:       "H264",
		AudioCodec:       "opus",
		AudioEnabled:     true,
		InputEnabled:     true,
		ProviderName:     "selkies",
		ProviderStreamID: "provider_123",
		Error:            "",
		Metadata:         map[string]string{"key": "value"},
		CreatedAt:        now,
		UpdatedAt:        now,
		StartedAt:        &now,
		StoppedAt:        nil,
		ExpiresAt:        &expiresAt,
	}

	assert.Equal(t, "strm_123", stream.ID)
	assert.Equal(t, "sess_456", stream.SessionID)
	assert.Equal(t, "run_789", stream.RunnerID)
	assert.Equal(t, StreamTypeDesktop, stream.Type)
	assert.Equal(t, StreamStateActive, stream.State)
	assert.Equal(t, 1920, stream.Resolution.Width)
	assert.Equal(t, 1080, stream.Resolution.Height)
	assert.True(t, stream.AudioEnabled)
	assert.True(t, stream.InputEnabled)
	require.NotNil(t, stream.StartedAt)
	assert.Nil(t, stream.StoppedAt)
}

func TestStreamInfo_Fields(t *testing.T) {
	info := StreamInfo{
		ID:           "provider_stream_123",
		SignalingURL: "ws://localhost:8080/signaling",
		Resolution:   Resolution{1280, 720},
		FrameRate:    30,
		BitRate:      4000000,
		VideoCodec:   "VP8",
		AudioCodec:   "opus",
		Metadata:     map[string]string{"device": "pixel"},
	}

	assert.Equal(t, "provider_stream_123", info.ID)
	assert.Equal(t, "ws://localhost:8080/signaling", info.SignalingURL)
	assert.Equal(t, 1280, info.Resolution.Width)
	assert.Equal(t, 720, info.Resolution.Height)
	assert.Equal(t, 30, info.FrameRate)
	assert.Equal(t, "VP8", info.VideoCodec)
}

func TestICEServer_Fields(t *testing.T) {
	server := ICEServer{
		URLs:       []string{"stun:stun.l.google.com:19302", "turn:turn.example.com:3478"},
		Username:   "user",
		Credential: "pass",
	}

	assert.Len(t, server.URLs, 2)
	assert.Equal(t, "user", server.Username)
	assert.Equal(t, "pass", server.Credential)
}

func TestValidStreamTypes(t *testing.T) {
	// Ensure all valid types are in the list
	assert.Len(t, ValidStreamTypes, 4)
	assert.Contains(t, ValidStreamTypes, StreamTypeDesktop)
	assert.Contains(t, ValidStreamTypes, StreamTypeBrowser)
	assert.Contains(t, ValidStreamTypes, StreamTypeIOS)
	assert.Contains(t, ValidStreamTypes, StreamTypeAndroid)
}

func TestValidStreamStates(t *testing.T) {
	// Ensure all valid states are in the list
	assert.Len(t, ValidStreamStates, 7)
	assert.Contains(t, ValidStreamStates, StreamStatePending)
	assert.Contains(t, ValidStreamStates, StreamStateStarting)
	assert.Contains(t, ValidStreamStates, StreamStateActive)
	assert.Contains(t, ValidStreamStates, StreamStatePaused)
	assert.Contains(t, ValidStreamStates, StreamStateStopping)
	assert.Contains(t, ValidStreamStates, StreamStateStopped)
	assert.Contains(t, ValidStreamStates, StreamStateError)
}

package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStreamState_IsValid(t *testing.T) {
	tests := []struct {
		state StreamState
		valid bool
	}{
		{StreamStateIdle, true},
		{StreamStateStarting, true},
		{StreamStateActive, true},
		{StreamStatePaused, true},
		{StreamStateStopping, true},
		{StreamStateStopped, true},
		{StreamStateError, true},
		{StreamState("invalid"), false},
		{StreamState(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.state.IsValid())
		})
	}
}

func TestStreamState_String(t *testing.T) {
	assert.Equal(t, "active", StreamStateActive.String())
	assert.Equal(t, "idle", StreamStateIdle.String())
}

func TestFrameFormat_IsValid(t *testing.T) {
	tests := []struct {
		format FrameFormat
		valid  bool
	}{
		{FormatJPEG, true},
		{FormatPNG, true},
		{FormatWebP, true},
		{FrameFormat("gif"), false},
		{FrameFormat(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.format.IsValid())
		})
	}
}

func TestFrameFormat_String(t *testing.T) {
	assert.Equal(t, "jpeg", FormatJPEG.String())
	assert.Equal(t, "png", FormatPNG.String())
}

func TestInputEventType_IsValid(t *testing.T) {
	tests := []struct {
		eventType InputEventType
		valid     bool
	}{
		{InputEventMouseMove, true},
		{InputEventMouseDown, true},
		{InputEventMouseUp, true},
		{InputEventMouseWheel, true},
		{InputEventKeyDown, true},
		{InputEventKeyUp, true},
		{InputEventType("invalid"), false},
		{InputEventType(""), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.eventType.IsValid())
		})
	}
}

func TestMouseButton_IsValid(t *testing.T) {
	tests := []struct {
		button MouseButton
		valid  bool
	}{
		{MouseButtonLeft, true},
		{MouseButtonMiddle, true},
		{MouseButtonRight, true},
		{MouseButton(""), true}, // Empty is valid
		{MouseButton("back"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.button), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.button.IsValid())
		})
	}
}

func TestModifiers_ToCDPModifiers(t *testing.T) {
	tests := []struct {
		name      string
		modifiers *Modifiers
		expected  int
	}{
		{
			name:      "nil modifiers",
			modifiers: nil,
			expected:  0,
		},
		{
			name:      "no modifiers",
			modifiers: &Modifiers{},
			expected:  0,
		},
		{
			name:      "alt only",
			modifiers: &Modifiers{Alt: true},
			expected:  1,
		},
		{
			name:      "ctrl only",
			modifiers: &Modifiers{Ctrl: true},
			expected:  2,
		},
		{
			name:      "meta only",
			modifiers: &Modifiers{Meta: true},
			expected:  4,
		},
		{
			name:      "shift only",
			modifiers: &Modifiers{Shift: true},
			expected:  8,
		},
		{
			name:      "all modifiers",
			modifiers: &Modifiers{Alt: true, Ctrl: true, Meta: true, Shift: true},
			expected:  15, // 1 + 2 + 4 + 8
		},
		{
			name:      "ctrl+shift",
			modifiers: &Modifiers{Ctrl: true, Shift: true},
			expected:  10, // 2 + 8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.modifiers.ToCDPModifiers())
		})
	}
}

func TestNavigateRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		request NavigateRequest
		wantErr error
	}{
		{
			name:    "valid URL",
			request: NavigateRequest{URL: "https://example.com"},
			wantErr: nil,
		},
		{
			name:    "empty URL",
			request: NavigateRequest{URL: ""},
			wantErr: ErrEmptyURL,
		},
		{
			name:    "with referrer",
			request: NavigateRequest{URL: "https://example.com", Referrer: "https://google.com"},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

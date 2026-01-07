package browser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStreamState_IsValid(t *testing.T) {
	tests := []struct {
		state StreamState
		want  bool
	}{
		{StreamStateIdle, true},
		{StreamStateStarting, true},
		{StreamStateActive, true},
		{StreamStatePaused, true},
		{StreamStateStopping, true},
		{StreamStateStopped, true},
		{StreamStateError, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsValid(); got != tt.want {
				t.Errorf("StreamState(%q).IsValid() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

func TestStreamState_String(t *testing.T) {
	if got := StreamStateActive.String(); got != "active" {
		t.Errorf("StreamStateActive.String() = %q, want %q", got, "active")
	}
}

func TestFrameFormat_IsValid(t *testing.T) {
	tests := []struct {
		format FrameFormat
		want   bool
	}{
		{FormatJPEG, true},
		{FormatPNG, true},
		{FormatWebP, true},
		{"gif", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			if got := tt.format.IsValid(); got != tt.want {
				t.Errorf("FrameFormat(%q).IsValid() = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestFrameFormat_String(t *testing.T) {
	if got := FormatJPEG.String(); got != "jpeg" {
		t.Errorf("FormatJPEG.String() = %q, want %q", got, "jpeg")
	}
}

func TestStreamOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    StreamOptions
		wantErr error
	}{
		{
			name:    "defaults",
			opts:    StreamOptions{},
			wantErr: nil,
		},
		{
			name: "valid custom options",
			opts: StreamOptions{
				Quality:       90,
				MaxFPS:        60,
				Format:        FormatPNG,
				MaxWidth:      1920,
				MaxHeight:     1080,
				EveryNthFrame: 2,
			},
			wantErr: nil,
		},
		{
			name:    "invalid quality negative",
			opts:    StreamOptions{Quality: -1},
			wantErr: ErrInvalidQuality,
		},
		{
			name:    "invalid quality over 100",
			opts:    StreamOptions{Quality: 101},
			wantErr: ErrInvalidQuality,
		},
		{
			name:    "invalid fps negative",
			opts:    StreamOptions{MaxFPS: -1},
			wantErr: ErrInvalidFPS,
		},
		{
			name:    "invalid format",
			opts:    StreamOptions{Format: "gif"},
			wantErr: ErrInvalidFormat,
		},
		{
			name:    "invalid max width",
			opts:    StreamOptions{MaxWidth: -1},
			wantErr: ErrInvalidDimensions,
		},
		{
			name:    "invalid max height",
			opts:    StreamOptions{MaxHeight: -1},
			wantErr: ErrInvalidDimensions,
		},
		{
			name:    "invalid every nth frame",
			opts:    StreamOptions{EveryNthFrame: -1},
			wantErr: ErrInvalidEveryNthFrame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if err != tt.wantErr {
				t.Errorf("StreamOptions.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStreamOptions_Validate_SetsDefaults(t *testing.T) {
	opts := StreamOptions{}
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if opts.Quality != DefaultQuality {
		t.Errorf("Quality = %d, want %d", opts.Quality, DefaultQuality)
	}
	if opts.MaxFPS != DefaultMaxFPS {
		t.Errorf("MaxFPS = %d, want %d", opts.MaxFPS, DefaultMaxFPS)
	}
	if opts.Format != FormatJPEG {
		t.Errorf("Format = %q, want %q", opts.Format, FormatJPEG)
	}
	if opts.EveryNthFrame != 1 {
		t.Errorf("EveryNthFrame = %d, want 1", opts.EveryNthFrame)
	}
}

func TestStreamOptions_Validate_CapsFPS(t *testing.T) {
	opts := StreamOptions{MaxFPS: 120}
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if opts.MaxFPS != MaxAllowedFPS {
		t.Errorf("MaxFPS = %d, want %d (capped)", opts.MaxFPS, MaxAllowedFPS)
	}
}

func TestStreamOptions_Clone(t *testing.T) {
	original := &StreamOptions{
		TargetURL:     "http://example.com",
		Quality:       90,
		MaxFPS:        30,
		Format:        FormatPNG,
		MaxWidth:      1920,
		MaxHeight:     1080,
		EveryNthFrame: 2,
	}

	cloned := original.Clone()

	if cloned == original {
		t.Error("Clone() returned same pointer")
	}

	if cloned.TargetURL != original.TargetURL {
		t.Errorf("TargetURL = %q, want %q", cloned.TargetURL, original.TargetURL)
	}
	if cloned.Quality != original.Quality {
		t.Errorf("Quality = %d, want %d", cloned.Quality, original.Quality)
	}

	// Modify clone should not affect original
	cloned.Quality = 50
	if original.Quality == 50 {
		t.Error("modifying clone affected original")
	}
}

func TestStreamOptions_Clone_Nil(t *testing.T) {
	var opts *StreamOptions
	if opts.Clone() != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestFrame_Size(t *testing.T) {
	frame := &Frame{Data: make([]byte, 1024)}
	if got := frame.Size(); got != 1024 {
		t.Errorf("Frame.Size() = %d, want 1024", got)
	}
}

func TestFrame_Size_Nil(t *testing.T) {
	var frame *Frame
	if got := frame.Size(); got != 0 {
		t.Errorf("nil Frame.Size() = %d, want 0", got)
	}
}

func TestFrame_Clone(t *testing.T) {
	original := &Frame{
		Data:      []byte{1, 2, 3, 4, 5},
		Format:    FormatJPEG,
		Width:     1920,
		Height:    1080,
		Timestamp: time.Now(),
		Sequence:  42,
		Metadata:  json.RawMessage(`{"key":"value"}`),
	}

	cloned := original.Clone()

	if cloned == original {
		t.Error("Clone() returned same pointer")
	}

	if string(cloned.Data) != string(original.Data) {
		t.Error("Data not equal")
	}
	if cloned.Format != original.Format {
		t.Errorf("Format = %q, want %q", cloned.Format, original.Format)
	}
	if cloned.Sequence != original.Sequence {
		t.Errorf("Sequence = %d, want %d", cloned.Sequence, original.Sequence)
	}

	// Modify clone should not affect original
	cloned.Data[0] = 99
	if original.Data[0] == 99 {
		t.Error("modifying clone Data affected original")
	}

	cloned.Metadata[0] = '!'
	if original.Metadata[0] == '!' {
		t.Error("modifying clone Metadata affected original")
	}
}

func TestFrame_Clone_Nil(t *testing.T) {
	var frame *Frame
	if frame.Clone() != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestFrame_Clone_NilMetadata(t *testing.T) {
	original := &Frame{
		Data:   []byte{1, 2, 3},
		Format: FormatJPEG,
	}

	cloned := original.Clone()
	if cloned.Metadata != nil {
		t.Error("Clone() should have nil Metadata when original has nil")
	}
}

func TestStreamStats_Clone(t *testing.T) {
	now := time.Now()
	original := &StreamStats{
		FramesSent:       100,
		FramesDropped:    5,
		BytesSent:        102400,
		AverageFPS:       29.5,
		CurrentFPS:       30.0,
		AverageFrameSize: 1024,
		LastFrameAt:      &now,
		Latency:          50,
	}

	cloned := original.Clone()

	if cloned == original {
		t.Error("Clone() returned same pointer")
	}

	if cloned.FramesSent != original.FramesSent {
		t.Errorf("FramesSent = %d, want %d", cloned.FramesSent, original.FramesSent)
	}

	if cloned.LastFrameAt == original.LastFrameAt {
		t.Error("LastFrameAt should be a new pointer")
	}

	if *cloned.LastFrameAt != *original.LastFrameAt {
		t.Error("LastFrameAt values should be equal")
	}
}

func TestStreamStats_Clone_Nil(t *testing.T) {
	var stats *StreamStats
	if stats.Clone() != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestStreamStats_Clone_NilLastFrameAt(t *testing.T) {
	original := &StreamStats{
		FramesSent: 100,
	}

	cloned := original.Clone()
	if cloned.LastFrameAt != nil {
		t.Error("Clone() should have nil LastFrameAt when original has nil")
	}
}

func TestInputEventType_IsValid(t *testing.T) {
	tests := []struct {
		eventType InputEventType
		want      bool
	}{
		{InputEventMouseMove, true},
		{InputEventMouseDown, true},
		{InputEventMouseUp, true},
		{InputEventMouseWheel, true},
		{InputEventKeyDown, true},
		{InputEventKeyUp, true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if got := tt.eventType.IsValid(); got != tt.want {
				t.Errorf("InputEventType(%q).IsValid() = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestMouseButton_IsValid(t *testing.T) {
	tests := []struct {
		button MouseButton
		want   bool
	}{
		{MouseButtonLeft, true},
		{MouseButtonMiddle, true},
		{MouseButtonRight, true},
		{"", true}, // empty is valid (for move events)
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.button), func(t *testing.T) {
			if got := tt.button.IsValid(); got != tt.want {
				t.Errorf("MouseButton(%q).IsValid() = %v, want %v", tt.button, got, tt.want)
			}
		})
	}
}

func TestModifiers_ToCDPModifiers(t *testing.T) {
	tests := []struct {
		name      string
		modifiers *Modifiers
		want      int
	}{
		{
			name:      "nil modifiers",
			modifiers: nil,
			want:      0,
		},
		{
			name:      "no modifiers",
			modifiers: &Modifiers{},
			want:      0,
		},
		{
			name:      "alt only",
			modifiers: &Modifiers{Alt: true},
			want:      1,
		},
		{
			name:      "ctrl only",
			modifiers: &Modifiers{Ctrl: true},
			want:      2,
		},
		{
			name:      "meta only",
			modifiers: &Modifiers{Meta: true},
			want:      4,
		},
		{
			name:      "shift only",
			modifiers: &Modifiers{Shift: true},
			want:      8,
		},
		{
			name:      "all modifiers",
			modifiers: &Modifiers{Alt: true, Ctrl: true, Meta: true, Shift: true},
			want:      15, // 1 + 2 + 4 + 8
		},
		{
			name:      "ctrl+shift",
			modifiers: &Modifiers{Ctrl: true, Shift: true},
			want:      10, // 2 + 8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.modifiers.ToCDPModifiers(); got != tt.want {
				t.Errorf("Modifiers.ToCDPModifiers() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNavigateRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     NavigateRequest
		wantErr error
	}{
		{
			name:    "valid url",
			req:     NavigateRequest{URL: "https://example.com"},
			wantErr: nil,
		},
		{
			name:    "valid with referrer",
			req:     NavigateRequest{URL: "https://example.com", Referrer: "https://google.com"},
			wantErr: nil,
		},
		{
			name:    "empty url",
			req:     NavigateRequest{URL: ""},
			wantErr: ErrEmptyURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if err != tt.wantErr {
				t.Errorf("NavigateRequest.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Verify constants have reasonable values
	if DefaultQuality < 1 || DefaultQuality > 100 {
		t.Errorf("DefaultQuality = %d, want between 1 and 100", DefaultQuality)
	}
	if DefaultMaxFPS < 1 || DefaultMaxFPS > MaxAllowedFPS {
		t.Errorf("DefaultMaxFPS = %d, want between 1 and %d", DefaultMaxFPS, MaxAllowedFPS)
	}
	if DefaultBufferSize < 1 {
		t.Errorf("DefaultBufferSize = %d, want >= 1", DefaultBufferSize)
	}
	if MaxBufferSize < DefaultBufferSize {
		t.Errorf("MaxBufferSize = %d, should be >= DefaultBufferSize = %d", MaxBufferSize, DefaultBufferSize)
	}
}

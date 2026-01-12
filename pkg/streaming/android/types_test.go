package android

import (
	"encoding/json"
	"testing"
)

func TestDeviceState_IsConnected(t *testing.T) {
	tests := []struct {
		state    DeviceState
		expected bool
	}{
		{DeviceStateDevice, true},
		{DeviceStateOffline, false},
		{DeviceStateUnauthorized, false},
		{DeviceStateBootloader, false},
		{DeviceStateRecovery, false},
		{DeviceStateSideload, false},
		{DeviceStateNoDevice, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsConnected(); got != tt.expected {
				t.Errorf("DeviceState(%q).IsConnected() = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestDeviceState_String(t *testing.T) {
	tests := []struct {
		state    DeviceState
		expected string
	}{
		{DeviceStateDevice, "device"},
		{DeviceStateOffline, "offline"},
		{DeviceStateUnauthorized, "unauthorized"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("DeviceState.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStreamState_IsActive(t *testing.T) {
	tests := []struct {
		state    StreamState
		expected bool
	}{
		{StreamStateStarting, true},
		{StreamStateRunning, true},
		{StreamStatePaused, false},
		{StreamStateStopped, false},
		{StreamStateError, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsActive(); got != tt.expected {
				t.Errorf("StreamState(%q).IsActive() = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}

func TestStreamState_String(t *testing.T) {
	tests := []struct {
		state    StreamState
		expected string
	}{
		{StreamStateStarting, "starting"},
		{StreamStateRunning, "running"},
		{StreamStatePaused, "paused"},
		{StreamStateStopped, "stopped"},
		{StreamStateError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("StreamState.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStreamOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    StreamOptions
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid minimal",
			opts:    StreamOptions{DeviceSerial: "emulator-5554"},
			wantErr: false,
		},
		{
			name: "valid full",
			opts: StreamOptions{
				DeviceSerial: "emulator-5554",
				MaxWidth:     1920,
				MaxHeight:    1080,
				Bitrate:      8_000_000,
				MaxFPS:       60,
				Rotation:     90,
				VideoCodec:   "h264",
				AudioEnabled: true,
				AudioCodec:   "opus",
			},
			wantErr: false,
		},
		{
			name:    "missing device serial",
			opts:    StreamOptions{},
			wantErr: true,
			errMsg:  "device serial is required",
		},
		{
			name:    "negative max width",
			opts:    StreamOptions{DeviceSerial: "test", MaxWidth: -1},
			wantErr: true,
			errMsg:  "max width must be non-negative",
		},
		{
			name:    "negative max height",
			opts:    StreamOptions{DeviceSerial: "test", MaxHeight: -1},
			wantErr: true,
			errMsg:  "max height must be non-negative",
		},
		{
			name:    "negative bitrate",
			opts:    StreamOptions{DeviceSerial: "test", Bitrate: -1},
			wantErr: true,
			errMsg:  "bitrate must be non-negative",
		},
		{
			name:    "negative max fps",
			opts:    StreamOptions{DeviceSerial: "test", MaxFPS: -1},
			wantErr: true,
			errMsg:  "max FPS must be non-negative",
		},
		{
			name:    "invalid rotation",
			opts:    StreamOptions{DeviceSerial: "test", Rotation: 45},
			wantErr: true,
			errMsg:  "rotation must be 0, 90, 180, or 270",
		},
		{
			name:    "valid rotation 0",
			opts:    StreamOptions{DeviceSerial: "test", Rotation: 0},
			wantErr: false,
		},
		{
			name:    "valid rotation 90",
			opts:    StreamOptions{DeviceSerial: "test", Rotation: 90},
			wantErr: false,
		},
		{
			name:    "valid rotation 180",
			opts:    StreamOptions{DeviceSerial: "test", Rotation: 180},
			wantErr: false,
		},
		{
			name:    "valid rotation 270",
			opts:    StreamOptions{DeviceSerial: "test", Rotation: 270},
			wantErr: false,
		},
		{
			name:    "invalid video codec",
			opts:    StreamOptions{DeviceSerial: "test", VideoCodec: "vp9"},
			wantErr: true,
			errMsg:  "video codec must be h264, h265, or av1",
		},
		{
			name:    "valid video codec h264",
			opts:    StreamOptions{DeviceSerial: "test", VideoCodec: "h264"},
			wantErr: false,
		},
		{
			name:    "valid video codec h265",
			opts:    StreamOptions{DeviceSerial: "test", VideoCodec: "h265"},
			wantErr: false,
		},
		{
			name:    "valid video codec av1",
			opts:    StreamOptions{DeviceSerial: "test", VideoCodec: "av1"},
			wantErr: false,
		},
		{
			name:    "invalid audio codec",
			opts:    StreamOptions{DeviceSerial: "test", AudioCodec: "mp3"},
			wantErr: true,
			errMsg:  "audio codec must be opus, aac, flac, or raw",
		},
		{
			name:    "valid audio codec opus",
			opts:    StreamOptions{DeviceSerial: "test", AudioCodec: "opus"},
			wantErr: false,
		},
		{
			name:    "valid audio codec aac",
			opts:    StreamOptions{DeviceSerial: "test", AudioCodec: "aac"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestStreamOptions_WithDefaults(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		opts := StreamOptions{DeviceSerial: "test"}.WithDefaults()

		if opts.Bitrate != 8_000_000 {
			t.Errorf("WithDefaults() Bitrate = %d, want %d", opts.Bitrate, 8_000_000)
		}
		if opts.MaxFPS != 60 {
			t.Errorf("WithDefaults() MaxFPS = %d, want %d", opts.MaxFPS, 60)
		}
		if opts.VideoCodec != "h264" {
			t.Errorf("WithDefaults() VideoCodec = %q, want %q", opts.VideoCodec, "h264")
		}
		// AudioCodec should not be set if AudioEnabled is false
		if opts.AudioCodec != "" {
			t.Errorf("WithDefaults() AudioCodec = %q, want empty", opts.AudioCodec)
		}
	})

	t.Run("preserves existing values", func(t *testing.T) {
		opts := StreamOptions{
			DeviceSerial: "test",
			Bitrate:      4_000_000,
			MaxFPS:       30,
			VideoCodec:   "h265",
		}.WithDefaults()

		if opts.Bitrate != 4_000_000 {
			t.Errorf("WithDefaults() Bitrate = %d, want %d", opts.Bitrate, 4_000_000)
		}
		if opts.MaxFPS != 30 {
			t.Errorf("WithDefaults() MaxFPS = %d, want %d", opts.MaxFPS, 30)
		}
		if opts.VideoCodec != "h265" {
			t.Errorf("WithDefaults() VideoCodec = %q, want %q", opts.VideoCodec, "h265")
		}
	})

	t.Run("sets audio codec when audio enabled", func(t *testing.T) {
		opts := StreamOptions{
			DeviceSerial: "test",
			AudioEnabled: true,
		}.WithDefaults()

		if opts.AudioCodec != "opus" {
			t.Errorf("WithDefaults() AudioCodec = %q, want %q", opts.AudioCodec, "opus")
		}
	})
}

func TestInputEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   InputEvent
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid tap",
			event:   InputEvent{Type: InputTypeTap, X: 100, Y: 200},
			wantErr: false,
		},
		{
			name:    "tap negative x",
			event:   InputEvent{Type: InputTypeTap, X: -1, Y: 200},
			wantErr: true,
			errMsg:  "coordinates must be non-negative",
		},
		{
			name:    "tap negative y",
			event:   InputEvent{Type: InputTypeTap, X: 100, Y: -1},
			wantErr: true,
			errMsg:  "coordinates must be non-negative",
		},
		{
			name:    "valid long press",
			event:   InputEvent{Type: InputTypeLongPress, X: 100, Y: 200, Duration: 500},
			wantErr: false,
		},
		{
			name:    "long press negative coords",
			event:   InputEvent{Type: InputTypeLongPress, X: -1, Y: 200},
			wantErr: true,
			errMsg:  "coordinates must be non-negative",
		},
		{
			name:    "valid swipe",
			event:   InputEvent{Type: InputTypeSwipe, X: 100, Y: 200, EndX: 300, EndY: 400, Duration: 200},
			wantErr: false,
		},
		{
			name:    "swipe negative start coords",
			event:   InputEvent{Type: InputTypeSwipe, X: -1, Y: 200, EndX: 300, EndY: 400},
			wantErr: true,
			errMsg:  "coordinates must be non-negative",
		},
		{
			name:    "swipe negative end coords",
			event:   InputEvent{Type: InputTypeSwipe, X: 100, Y: 200, EndX: -1, EndY: 400},
			wantErr: true,
			errMsg:  "coordinates must be non-negative",
		},
		{
			name:    "swipe negative duration",
			event:   InputEvent{Type: InputTypeSwipe, X: 100, Y: 200, EndX: 300, EndY: 400, Duration: -1},
			wantErr: true,
			errMsg:  "duration must be non-negative",
		},
		{
			name:    "valid text",
			event:   InputEvent{Type: InputTypeText, Text: "hello"},
			wantErr: false,
		},
		{
			name:    "text empty",
			event:   InputEvent{Type: InputTypeText, Text: ""},
			wantErr: true,
			errMsg:  "text is required",
		},
		{
			name:    "valid key with code",
			event:   InputEvent{Type: InputTypeKey, KeyCode: 66},
			wantErr: false,
		},
		{
			name:    "valid key with name",
			event:   InputEvent{Type: InputTypeKey, KeyName: "ENTER"},
			wantErr: false,
		},
		{
			name:    "key missing code and name",
			event:   InputEvent{Type: InputTypeKey},
			wantErr: true,
			errMsg:  "key code or key name is required",
		},
		{
			name:    "valid scroll",
			event:   InputEvent{Type: InputTypeScroll, X: 100, Y: 200, ScrollX: 0, ScrollY: -100},
			wantErr: false,
		},
		{
			name:    "valid pinch",
			event:   InputEvent{Type: InputTypePinch, X: 100, Y: 200, Scale: 1.5},
			wantErr: false,
		},
		{
			name:    "pinch zero scale",
			event:   InputEvent{Type: InputTypePinch, X: 100, Y: 200, Scale: 0},
			wantErr: true,
			errMsg:  "scale must be positive",
		},
		{
			name:    "pinch negative scale",
			event:   InputEvent{Type: InputTypePinch, X: 100, Y: 200, Scale: -1},
			wantErr: true,
			errMsg:  "scale must be positive",
		},
		{
			name:    "unknown type",
			event:   InputEvent{Type: "unknown"},
			wantErr: true,
			errMsg:  "unknown input type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestInputType_String(t *testing.T) {
	tests := []struct {
		inputType InputType
		expected  string
	}{
		{InputTypeTap, "tap"},
		{InputTypeSwipe, "swipe"},
		{InputTypeLongPress, "long_press"},
		{InputTypeText, "text"},
		{InputTypeKey, "key"},
		{InputTypeScroll, "scroll"},
		{InputTypePinch, "pinch"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.inputType.String(); got != tt.expected {
				t.Errorf("InputType.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestInputEvent_MarshalJSON(t *testing.T) {
	event := InputEvent{
		Type:     InputTypeTap,
		X:        100,
		Y:        200,
		Duration: 50,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	var decoded InputEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("UnmarshalJSON() error: %v", err)
	}

	if decoded.Type != event.Type {
		t.Errorf("Type = %q, want %q", decoded.Type, event.Type)
	}
	if decoded.X != event.X {
		t.Errorf("X = %d, want %d", decoded.X, event.X)
	}
	if decoded.Y != event.Y {
		t.Errorf("Y = %d, want %d", decoded.Y, event.Y)
	}
}

func TestDevice_JSON(t *testing.T) {
	device := Device{
		Serial:         "emulator-5554",
		Model:          "Pixel_4_API_30",
		Product:        "sdk_gphone_x86_64",
		Device:         "generic_x86_64",
		TransportID:    "1",
		State:          DeviceStateDevice,
		IsEmulator:     true,
		AndroidVersion: "30",
		ScreenSize:     "1080x1920",
		ScreenDensity:  420,
	}

	data, err := json.Marshal(device)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var decoded Device
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if decoded.Serial != device.Serial {
		t.Errorf("Serial = %q, want %q", decoded.Serial, device.Serial)
	}
	if decoded.State != device.State {
		t.Errorf("State = %q, want %q", decoded.State, device.State)
	}
	if decoded.IsEmulator != device.IsEmulator {
		t.Errorf("IsEmulator = %v, want %v", decoded.IsEmulator, device.IsEmulator)
	}
}

func TestStreamInfo_JSON(t *testing.T) {
	info := StreamInfo{
		ID:        "astr_test123",
		SessionID: "sess_test456",
		Device: &Device{
			Serial: "emulator-5554",
			State:  DeviceStateDevice,
		},
		State: StreamStateRunning,
		Options: &StreamOptions{
			DeviceSerial: "emulator-5554",
			MaxFPS:       60,
		},
		LocalPort: 8080,
		Width:     1080,
		Height:    1920,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var decoded StreamInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if decoded.ID != info.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, info.ID)
	}
	if decoded.State != info.State {
		t.Errorf("State = %q, want %q", decoded.State, info.State)
	}
	if decoded.Device.Serial != info.Device.Serial {
		t.Errorf("Device.Serial = %q, want %q", decoded.Device.Serial, info.Device.Serial)
	}
}

func TestStreamStats_JSON(t *testing.T) {
	stats := StreamStats{
		FramesSent:     1000,
		BytesSent:      1024000,
		DroppedFrames:  5,
		AverageFPS:     59.5,
		CurrentBitrate: 8000000,
		Latency:        15,
	}

	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}

	var decoded StreamStats
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}

	if decoded.FramesSent != stats.FramesSent {
		t.Errorf("FramesSent = %d, want %d", decoded.FramesSent, stats.FramesSent)
	}
	if decoded.AverageFPS != stats.AverageFPS {
		t.Errorf("AverageFPS = %f, want %f", decoded.AverageFPS, stats.AverageFPS)
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

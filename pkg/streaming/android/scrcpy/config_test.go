package scrcpy

import (
	"testing"
	"time"
)

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{}
	result := cfg.WithDefaults()

	if result.BasePort != DefaultBasePort {
		t.Errorf("BasePort = %d, want %d", result.BasePort, DefaultBasePort)
	}
	if result.ServerStartTimeout != DefaultServerStartTimeout {
		t.Errorf("ServerStartTimeout = %v, want %v", result.ServerStartTimeout, DefaultServerStartTimeout)
	}
	if result.ConnectionTimeout != DefaultConnectionTimeout {
		t.Errorf("ConnectionTimeout = %v, want %v", result.ConnectionTimeout, DefaultConnectionTimeout)
	}
	if result.VideoBitrate != DefaultVideoBitrate {
		t.Errorf("VideoBitrate = %d, want %d", result.VideoBitrate, DefaultVideoBitrate)
	}
	if result.MaxFPS != DefaultMaxFPS {
		t.Errorf("MaxFPS = %d, want %d", result.MaxFPS, DefaultMaxFPS)
	}
	if result.VideoCodec != DefaultVideoCodec {
		t.Errorf("VideoCodec = %s, want %s", result.VideoCodec, DefaultVideoCodec)
	}
	if result.AudioCodec != DefaultAudioCodec {
		t.Errorf("AudioCodec = %s, want %s", result.AudioCodec, DefaultAudioCodec)
	}
	if result.Logger == nil {
		t.Error("Logger should not be nil after WithDefaults")
	}
}

func TestConfig_WithDefaults_PreservesSet(t *testing.T) {
	cfg := Config{
		BasePort:           12345,
		ServerStartTimeout: 10 * time.Second,
		VideoBitrate:       4_000_000,
		VideoCodec:         "h265",
	}
	result := cfg.WithDefaults()

	if result.BasePort != 12345 {
		t.Errorf("BasePort = %d, want %d", result.BasePort, 12345)
	}
	if result.ServerStartTimeout != 10*time.Second {
		t.Errorf("ServerStartTimeout = %v, want %v", result.ServerStartTimeout, 10*time.Second)
	}
	if result.VideoBitrate != 4_000_000 {
		t.Errorf("VideoBitrate = %d, want %d", result.VideoBitrate, 4_000_000)
	}
	if result.VideoCodec != "h265" {
		t.Errorf("VideoCodec = %s, want %s", result.VideoCodec, "h265")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default",
			config:  Config{}.WithDefaults(),
			wantErr: false,
		},
		{
			name:    "invalid base port - too low",
			config:  Config{BasePort: 0},
			wantErr: true,
		},
		{
			name:    "invalid base port - too high",
			config:  Config{BasePort: 70000},
			wantErr: true,
		},
		{
			name:    "negative server start timeout",
			config:  Config{BasePort: DefaultBasePort, ServerStartTimeout: -1},
			wantErr: true,
		},
		{
			name:    "negative connection timeout",
			config:  Config{BasePort: DefaultBasePort, ConnectionTimeout: -1},
			wantErr: true,
		},
		{
			name:    "invalid video codec",
			config:  Config{BasePort: DefaultBasePort, VideoCodec: "invalid"},
			wantErr: true,
		},
		{
			name:    "invalid audio codec",
			config:  Config{BasePort: DefaultBasePort, AudioCodec: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid h265 codec",
			config:  Config{BasePort: DefaultBasePort, VideoCodec: "h265"},
			wantErr: false,
		},
		{
			name:    "valid av1 codec",
			config:  Config{BasePort: DefaultBasePort, VideoCodec: "av1"},
			wantErr: false,
		},
		{
			name:    "valid opus audio codec",
			config:  Config{BasePort: DefaultBasePort, AudioCodec: "opus"},
			wantErr: false,
		},
		{
			name:    "valid aac audio codec",
			config:  Config{BasePort: DefaultBasePort, AudioCodec: "aac"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

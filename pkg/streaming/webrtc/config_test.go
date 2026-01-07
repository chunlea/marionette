package webrtc

import (
	"testing"
	"time"
)

func TestConfig_WithDefaults(t *testing.T) {
	cfg := Config{}
	result := cfg.WithDefaults()

	if len(result.STUNServers) == 0 {
		t.Error("expected default STUN servers")
	}
	if result.ICEConnectionTimeout != DefaultICEConnectionTimeout {
		t.Errorf("ICEConnectionTimeout = %v, want %v", result.ICEConnectionTimeout, DefaultICEConnectionTimeout)
	}
	if result.PeerConnectionTimeout != DefaultPeerConnectionTimeout {
		t.Errorf("PeerConnectionTimeout = %v, want %v", result.PeerConnectionTimeout, DefaultPeerConnectionTimeout)
	}
	if result.VideoMTU != DefaultVideoMTU {
		t.Errorf("VideoMTU = %d, want %d", result.VideoMTU, DefaultVideoMTU)
	}
	if result.AudioMTU != DefaultAudioMTU {
		t.Errorf("AudioMTU = %d, want %d", result.AudioMTU, DefaultAudioMTU)
	}
	if result.VideoClockRate != DefaultVideoClockRate {
		t.Errorf("VideoClockRate = %d, want %d", result.VideoClockRate, DefaultVideoClockRate)
	}
	if result.AudioClockRate != DefaultAudioClockRate {
		t.Errorf("AudioClockRate = %d, want %d", result.AudioClockRate, DefaultAudioClockRate)
	}
	if result.Logger == nil {
		t.Error("Logger should not be nil after WithDefaults")
	}
}

func TestConfig_WithDefaults_PreservesSet(t *testing.T) {
	cfg := Config{
		STUNServers:          []string{"stun:custom.example.com:3478"},
		ICEConnectionTimeout: 10 * time.Second,
		VideoMTU:             1200,
	}
	result := cfg.WithDefaults()

	if len(result.STUNServers) != 1 || result.STUNServers[0] != "stun:custom.example.com:3478" {
		t.Error("expected custom STUN server to be preserved")
	}
	if result.ICEConnectionTimeout != 10*time.Second {
		t.Errorf("ICEConnectionTimeout = %v, want %v", result.ICEConnectionTimeout, 10*time.Second)
	}
	if result.VideoMTU != 1200 {
		t.Errorf("VideoMTU = %d, want %d", result.VideoMTU, 1200)
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
			name:    "negative ICE timeout",
			config:  Config{ICEConnectionTimeout: -1},
			wantErr: true,
		},
		{
			name:    "negative peer timeout",
			config:  Config{PeerConnectionTimeout: -1},
			wantErr: true,
		},
		{
			name:    "video MTU too low",
			config:  Config{VideoMTU: 100},
			wantErr: true,
		},
		{
			name:    "video MTU too high",
			config:  Config{VideoMTU: 100000},
			wantErr: true,
		},
		{
			name:    "audio MTU too low",
			config:  Config{AudioMTU: 50},
			wantErr: true,
		},
		{
			name:    "zero video clock rate",
			config:  Config{VideoClockRate: 0},
			wantErr: true,
		},
		{
			name:    "zero audio clock rate",
			config:  Config{AudioClockRate: 0},
			wantErr: true,
		},
		{
			name: "valid custom config",
			config: Config{
				VideoMTU:       1000,
				AudioMTU:       500,
				VideoClockRate: 90000,
				AudioClockRate: 48000,
			},
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

func TestConfig_ToWebRTCConfiguration(t *testing.T) {
	cfg := Config{
		STUNServers: []string{"stun:stun.example.com:3478"},
		TURNServers: []TURNServer{
			{
				URLs:       []string{"turn:turn.example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		},
	}

	webrtcCfg := cfg.ToWebRTCConfiguration()

	if len(webrtcCfg.ICEServers) != 2 {
		t.Errorf("expected 2 ICE servers, got %d", len(webrtcCfg.ICEServers))
	}

	// Check STUN server
	if len(webrtcCfg.ICEServers[0].URLs) != 1 || webrtcCfg.ICEServers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Error("STUN server not correctly configured")
	}

	// Check TURN server
	if len(webrtcCfg.ICEServers[1].URLs) != 1 || webrtcCfg.ICEServers[1].URLs[0] != "turn:turn.example.com:3478" {
		t.Error("TURN server not correctly configured")
	}
	if webrtcCfg.ICEServers[1].Username != "user" {
		t.Error("TURN username not correctly configured")
	}
}

func TestVideoCodecCapability(t *testing.T) {
	tests := []struct {
		codec    string
		wantMime string
	}{
		{"h264", "video/H264"},
		{"H264", "video/H264"},
		{"vp8", "video/VP8"},
		{"vp9", "video/VP9"},
		{"av1", "video/AV1"},
		{"unknown", "video/H264"}, // defaults to H264
	}

	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			cap := VideoCodecCapability(tt.codec)
			if cap.MimeType != tt.wantMime {
				t.Errorf("VideoCodecCapability(%s) = %s, want %s", tt.codec, cap.MimeType, tt.wantMime)
			}
		})
	}
}

func TestAudioCodecCapability(t *testing.T) {
	tests := []struct {
		codec    string
		wantMime string
	}{
		{"opus", "audio/opus"},
		{"OPUS", "audio/opus"},
		{"pcmu", "audio/PCMU"},
		{"pcma", "audio/PCMA"},
		{"unknown", "audio/opus"}, // defaults to Opus
	}

	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			cap := AudioCodecCapability(tt.codec)
			if cap.MimeType != tt.wantMime {
				t.Errorf("AudioCodecCapability(%s) = %s, want %s", tt.codec, cap.MimeType, tt.wantMime)
			}
		})
	}
}

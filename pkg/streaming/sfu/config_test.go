package sfu

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.True(t, cfg.EnableTWCC)
	assert.True(t, cfg.EnableRTCPReports)
	assert.Equal(t, uint16(3), cfg.PLIInterval) // 3 seconds
	assert.Equal(t, uint64(1024*1024), cfg.MaxBufferedAmount)
	// Default includes Google STUN server
	require.Len(t, cfg.ICEServers, 1)
	assert.Equal(t, []string{"stun:stun.l.google.com:19302"}, cfg.ICEServers[0].URLs)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "default config is valid",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "valid config with ICE servers",
			config: Config{
				ICEServers: []webrtc.ICEServer{
					{URLs: []string{"stun:stun.l.google.com:19302"}},
				},
				EnableTWCC:        true,
				EnableRTCPReports: true,
				PLIInterval:       500,
				MaxBufferedAmount: 512 * 1024,
			},
			wantErr: false,
		},
		{
			name: "valid config with TURN server",
			config: Config{
				ICEServers: []webrtc.ICEServer{
					{
						URLs:           []string{"turn:turn.example.com:3478"},
						Username:       "user",
						Credential:     "pass",
						CredentialType: webrtc.ICECredentialTypePassword,
					},
				},
				EnableTWCC:        true,
				EnableRTCPReports: true,
				PLIInterval:       1000,
				MaxBufferedAmount: 1024 * 1024,
			},
			wantErr: false,
		},
		{
			name: "zero PLI interval is valid (disabled)",
			config: Config{
				EnableTWCC:        true,
				EnableRTCPReports: true,
				PLIInterval:       0,
				MaxBufferedAmount: 1024 * 1024,
			},
			wantErr: false,
		},
		{
			name: "zero MaxBufferedAmount is valid",
			config: Config{
				EnableTWCC:        true,
				EnableRTCPReports: true,
				PLIInterval:       1000,
				MaxBufferedAmount: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_WebRTCConfig(t *testing.T) {
	cfg := Config{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{
				URLs:           []string{"turn:turn.example.com:3478"},
				Username:       "user",
				Credential:     "pass",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
		EnableTWCC:        true,
		EnableRTCPReports: true,
	}

	webrtcCfg := cfg.WebRTCConfig()

	require.Len(t, webrtcCfg.ICEServers, 2)
	assert.Equal(t, []string{"stun:stun.l.google.com:19302"}, webrtcCfg.ICEServers[0].URLs)
	assert.Equal(t, []string{"turn:turn.example.com:3478"}, webrtcCfg.ICEServers[1].URLs)
	assert.Equal(t, "user", webrtcCfg.ICEServers[1].Username)
	assert.Equal(t, "pass", webrtcCfg.ICEServers[1].Credential)
}

func TestConfig_WebRTCConfig_EmptyICEServers(t *testing.T) {
	cfg := Config{
		ICEServers:        nil,
		EnableTWCC:        true,
		EnableRTCPReports: true,
	}

	webrtcCfg := cfg.WebRTCConfig()

	assert.Empty(t, webrtcCfg.ICEServers)
}

func TestConfig_WithICEServers(t *testing.T) {
	cfg := Config{} // Start with empty config
	assert.Empty(t, cfg.ICEServers)

	servers := []webrtc.ICEServer{
		{URLs: []string{"stun:stun.example.com:19302"}},
	}
	cfg.ICEServers = servers

	assert.Len(t, cfg.ICEServers, 1)
	assert.Equal(t, "stun:stun.example.com:19302", cfg.ICEServers[0].URLs[0])
}

func TestConfig_AllFieldsCovered(t *testing.T) {
	// Test that all fields can be set and read
	cfg := Config{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:test.com:19302"}},
		},
		EnableTWCC:        false,
		EnableRTCPReports: false,
		PLIInterval:       2000,
		MaxBufferedAmount: 2 * 1024 * 1024,
	}

	assert.Len(t, cfg.ICEServers, 1)
	assert.False(t, cfg.EnableTWCC)
	assert.False(t, cfg.EnableRTCPReports)
	assert.Equal(t, uint16(2000), cfg.PLIInterval)
	assert.Equal(t, uint64(2*1024*1024), cfg.MaxBufferedAmount)
}

func TestConfig_ICEServerTypes(t *testing.T) {
	tests := []struct {
		name   string
		server webrtc.ICEServer
	}{
		{
			name: "STUN server",
			server: webrtc.ICEServer{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
		{
			name: "TURN TCP server",
			server: webrtc.ICEServer{
				URLs:           []string{"turn:turn.example.com:3478?transport=tcp"},
				Username:       "user",
				Credential:     "pass",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
		{
			name: "TURN UDP server",
			server: webrtc.ICEServer{
				URLs:           []string{"turn:turn.example.com:3478?transport=udp"},
				Username:       "user",
				Credential:     "pass",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
		{
			name: "TURNS server",
			server: webrtc.ICEServer{
				URLs:           []string{"turns:turn.example.com:443"},
				Username:       "user",
				Credential:     "pass",
				CredentialType: webrtc.ICECredentialTypePassword,
			},
		},
		{
			name: "Multiple URLs",
			server: webrtc.ICEServer{
				URLs: []string{
					"stun:stun1.example.com:19302",
					"stun:stun2.example.com:19302",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				ICEServers: []webrtc.ICEServer{tt.server},
			}
			webrtcCfg := cfg.WebRTCConfig()
			require.Len(t, webrtcCfg.ICEServers, 1)
			assert.Equal(t, tt.server.URLs, webrtcCfg.ICEServers[0].URLs)
		})
	}
}

func TestDefaultConfig_IsReasonable(t *testing.T) {
	cfg := DefaultConfig()

	// PLI interval should be reasonable (1-10 seconds typically)
	assert.GreaterOrEqual(t, cfg.PLIInterval, uint16(1))
	assert.LessOrEqual(t, cfg.PLIInterval, uint16(10))

	// MaxBufferedAmount should be at least 256KB
	assert.GreaterOrEqual(t, cfg.MaxBufferedAmount, uint64(256*1024))

	// TWCC and RTCP reports should be enabled by default for better quality
	assert.True(t, cfg.EnableTWCC)
	assert.True(t, cfg.EnableRTCPReports)

	// Should have at least one ICE server
	assert.NotEmpty(t, cfg.ICEServers)
}

func BenchmarkConfig_Validate(b *testing.B) {
	cfg := DefaultConfig()
	cfg.ICEServers = []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.Validate()
	}
}

func BenchmarkConfig_WebRTCConfig(b *testing.B) {
	cfg := DefaultConfig()
	cfg.ICEServers = []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		{URLs: []string{"turn:turn.example.com:3478"}, Username: "u", Credential: "p"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.WebRTCConfig()
	}
}

// Test Config persistence (for future serialization if needed)
func TestConfig_TimingValues(t *testing.T) {
	// Verify PLI interval is in seconds (as documented in config.go)
	cfg := DefaultConfig()

	// 3 seconds is the default PLI interval
	pliDuration := time.Duration(cfg.PLIInterval) * time.Second
	assert.Equal(t, 3*time.Second, pliDuration)
}

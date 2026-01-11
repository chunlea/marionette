package manager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/chunlea/marionette/pkg/streaming"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 30*time.Second, cfg.DefaultTimeout)
	assert.Equal(t, 5*time.Minute, cfg.CleanupInterval)
	assert.Equal(t, time.Duration(0), cfg.StreamExpiry)
	assert.Equal(t, 1920, cfg.DefaultResolution.Width)
	assert.Equal(t, 1080, cfg.DefaultResolution.Height)
	assert.Equal(t, 30, cfg.DefaultFrameRate)
	assert.Equal(t, 4_000_000, cfg.DefaultBitRate)
	assert.Len(t, cfg.DefaultICEServers, 1)
	assert.Contains(t, cfg.DefaultICEServers[0].URLs[0], "stun:")
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
			name: "custom config is valid",
			config: Config{
				DefaultTimeout:    10 * time.Second,
				CleanupInterval:   1 * time.Minute,
				DefaultFrameRate:  60,
				DefaultBitRate:    8_000_000,
				DefaultResolution: streaming.Resolution{Width: 3840, Height: 2160},
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

func TestConfig_WithMethods(t *testing.T) {
	cfg := DefaultConfig()

	// Test WithDefaultProvider
	cfg = cfg.WithDefaultProvider("test-provider")
	assert.Equal(t, "test-provider", cfg.DefaultProvider)

	// Test WithDefaultTimeout
	cfg = cfg.WithDefaultTimeout(1 * time.Minute)
	assert.Equal(t, 1*time.Minute, cfg.DefaultTimeout)

	// Test WithDefaultICEServers
	servers := []streaming.ICEServer{{URLs: []string{"stun:custom.server:3478"}}}
	cfg = cfg.WithDefaultICEServers(servers)
	assert.Equal(t, servers, cfg.DefaultICEServers)

	// Test WithDefaultResolution
	res := streaming.Resolution{Width: 2560, Height: 1440}
	cfg = cfg.WithDefaultResolution(res)
	assert.Equal(t, res, cfg.DefaultResolution)

	// Test WithDefaultFrameRate
	cfg = cfg.WithDefaultFrameRate(60)
	assert.Equal(t, 60, cfg.DefaultFrameRate)

	// Test WithDefaultBitRate
	cfg = cfg.WithDefaultBitRate(8_000_000)
	assert.Equal(t, 8_000_000, cfg.DefaultBitRate)

	// Test WithCleanupInterval
	cfg = cfg.WithCleanupInterval(10 * time.Minute)
	assert.Equal(t, 10*time.Minute, cfg.CleanupInterval)

	// Test WithStreamExpiry
	cfg = cfg.WithStreamExpiry(1 * time.Hour)
	assert.Equal(t, 1*time.Hour, cfg.StreamExpiry)
}

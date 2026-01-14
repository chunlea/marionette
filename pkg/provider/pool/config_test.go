package pool

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 0, cfg.MinRunners)
	assert.Equal(t, 100, cfg.MaxRunners)
	assert.Equal(t, 30*time.Minute, cfg.IdleTimeout)
	assert.Equal(t, 30*time.Second, cfg.HealthCheckInterval)
	assert.Equal(t, 90*time.Second, cfg.StaleThreshold)
	assert.Equal(t, 5*time.Minute, cfg.InitScriptTimeout)
	assert.Equal(t, 5*time.Minute, cfg.CleanupScriptTimeout)
	assert.Equal(t, 0, cfg.MaxTasksPerRunner)
	assert.Equal(t, "lru", cfg.SelectionStrategy)
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    func(*Config) bool
		wantErr bool
	}{
		{
			name:  "empty config uses defaults",
			input: "{}",
			want: func(c *Config) bool {
				return c.MinRunners == 0 && c.MaxRunners == 100
			},
		},
		{
			name:  "null config uses defaults",
			input: "null",
			want: func(c *Config) bool {
				return c.SelectionStrategy == "lru"
			},
		},
		{
			name:  "empty string uses defaults",
			input: "",
			want: func(c *Config) bool {
				return c.IdleTimeout == 30*time.Minute
			},
		},
		{
			name:  "parses pool_name",
			input: `{"pool_name": "gpu-pool"}`,
			want: func(c *Config) bool {
				return c.PoolName == "gpu-pool"
			},
		},
		{
			name:  "parses required_labels",
			input: `{"pool_name": "test", "required_labels": {"gpu": "nvidia", "region": "us-west"}}`,
			want: func(c *Config) bool {
				return c.RequiredLabels["gpu"] == "nvidia" && c.RequiredLabels["region"] == "us-west"
			},
		},
		{
			name:  "parses required_capabilities",
			input: `{"pool_name": "test", "required_capabilities": ["cuda", "docker"]}`,
			want: func(c *Config) bool {
				return len(c.RequiredCapabilities) == 2 &&
					c.RequiredCapabilities[0] == "cuda" &&
					c.RequiredCapabilities[1] == "docker"
			},
		},
		{
			name:  "parses min_runners",
			input: `{"pool_name": "test", "min_runners": 5}`,
			want: func(c *Config) bool {
				return c.MinRunners == 5
			},
		},
		{
			name:  "parses max_runners",
			input: `{"pool_name": "test", "max_runners": 50}`,
			want: func(c *Config) bool {
				return c.MaxRunners == 50
			},
		},
		{
			name:  "parses durations",
			input: `{"pool_name": "test", "idle_timeout": "1h", "health_check_interval": "1m", "stale_threshold": "2m"}`,
			want: func(c *Config) bool {
				return c.IdleTimeout == time.Hour &&
					c.HealthCheckInterval == time.Minute &&
					c.StaleThreshold == 2*time.Minute
			},
		},
		{
			name:  "parses script timeouts",
			input: `{"pool_name": "test", "init_script_timeout": "10m", "cleanup_script_timeout": "3m"}`,
			want: func(c *Config) bool {
				return c.InitScriptTimeout == 10*time.Minute &&
					c.CleanupScriptTimeout == 3*time.Minute
			},
		},
		{
			name:  "parses max_tasks_per_runner",
			input: `{"pool_name": "test", "max_tasks_per_runner": 100}`,
			want: func(c *Config) bool {
				return c.MaxTasksPerRunner == 100
			},
		},
		{
			name:  "parses selection_strategy",
			input: `{"pool_name": "test", "selection_strategy": "random"}`,
			want: func(c *Config) bool {
				return c.SelectionStrategy == "random"
			},
		},
		{
			name:    "invalid duration",
			input:   `{"pool_name": "test", "idle_timeout": "invalid"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig(json.RawMessage(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, tt.want(cfg))
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr string
	}{
		{
			name: "valid config",
			config: &Config{
				PoolName:          "test-pool",
				MinRunners:        5,
				MaxRunners:        10,
				SelectionStrategy: "lru",
			},
			wantErr: "",
		},
		{
			name: "missing pool_name",
			config: &Config{
				MinRunners: 0,
				MaxRunners: 10,
			},
			wantErr: "pool_name is required",
		},
		{
			name: "max_runners less than min_runners",
			config: &Config{
				PoolName:   "test",
				MinRunners: 10,
				MaxRunners: 5,
			},
			wantErr: "max_runners (5) must be >= min_runners (10)",
		},
		{
			name: "invalid selection_strategy",
			config: &Config{
				PoolName:          "test",
				MinRunners:        0,
				MaxRunners:        10,
				SelectionStrategy: "invalid",
			},
			wantErr: "invalid selection_strategy",
		},
		{
			name: "valid lru strategy",
			config: &Config{
				PoolName:          "test",
				SelectionStrategy: "lru",
			},
		},
		{
			name: "valid random strategy",
			config: &Config{
				PoolName:          "test",
				SelectionStrategy: "random",
			},
		},
		{
			name: "valid round_robin strategy",
			config: &Config{
				PoolName:          "test",
				SelectionStrategy: "round_robin",
			},
		},
		{
			name: "empty strategy is valid",
			config: &Config{
				PoolName:          "test",
				SelectionStrategy: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDefaultSuspendConfig(t *testing.T) {
	cfg := DefaultSuspendConfig()

	assert.Equal(t, provider.SuspendStrategyReleaseToPool, cfg.Strategy)
	assert.Equal(t, 60*time.Second, cfg.MinDuration)
	assert.Equal(t, 24*time.Hour, cfg.MaxDuration)
	assert.True(t, cfg.SyncWorkspace)
}

func TestParseSuspendConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    func(*SuspendConfig) bool
		wantErr bool
	}{
		{
			name:  "empty config uses defaults",
			input: "{}",
			want: func(c *SuspendConfig) bool {
				return c.Strategy == provider.SuspendStrategyReleaseToPool
			},
		},
		{
			name:  "null config uses defaults",
			input: "null",
			want: func(c *SuspendConfig) bool {
				return c.MinDuration == 60*time.Second
			},
		},
		{
			name:  "parses strategy",
			input: `{"strategy": "terminate"}`,
			want: func(c *SuspendConfig) bool {
				return c.Strategy == provider.SuspendStrategyTerminate
			},
		},
		{
			name:  "parses durations",
			input: `{"min_duration": "30s", "max_duration": "12h"}`,
			want: func(c *SuspendConfig) bool {
				return c.MinDuration == 30*time.Second && c.MaxDuration == 12*time.Hour
			},
		},
		{
			name:  "parses sync_workspace",
			input: `{"sync_workspace": false}`,
			want: func(c *SuspendConfig) bool {
				return !c.SyncWorkspace
			},
		},
		{
			name:    "invalid duration",
			input:   `{"min_duration": "invalid"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseSuspendConfig(json.RawMessage(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, tt.want(cfg))
		})
	}
}

func TestSuspendConfigToProviderSuspendConfig(t *testing.T) {
	cfg := &SuspendConfig{
		Strategy:      provider.SuspendStrategyReleaseToPool,
		MinDuration:   30 * time.Second,
		MaxDuration:   12 * time.Hour,
		SyncWorkspace: true,
	}

	providerCfg := cfg.ToProviderSuspendConfig()

	assert.Equal(t, provider.SuspendStrategyReleaseToPool, providerCfg.Strategy)
	assert.Equal(t, 30*time.Second, providerCfg.MinDuration)
	assert.Equal(t, 12*time.Hour, providerCfg.MaxDuration)
	assert.True(t, providerCfg.SyncWorkspace)
}

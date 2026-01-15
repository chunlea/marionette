package e2b

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *Config
		wantErr bool
	}{
		{
			name:  "empty config applies defaults",
			input: `{}`,
			want: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: DefaultTimeoutSeconds,
				LabelPrefix:    DefaultLabelPrefix,
			},
		},
		{
			name: "custom config",
			input: `{
				"api_url": "https://custom.e2b.dev",
				"api_key": "test-key",
				"template": "custom-template",
				"timeout_seconds": 600,
				"label_prefix": "custom.prefix"
			}`,
			want: &Config{
				APIURL:         "https://custom.e2b.dev",
				APIKey:         "test-key",
				Template:       "custom-template",
				TimeoutSeconds: 600,
				LabelPrefix:    "custom.prefix",
			},
		},
		{
			name: "with resources",
			input: `{
				"resources": {
					"vcpu": 4,
					"memory_mb": 8192,
					"disk_mb": 10240
				}
			}`,
			want: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: DefaultTimeoutSeconds,
				LabelPrefix:    DefaultLabelPrefix,
				Resources: ResourceConfig{
					VCPU:     4,
					MemoryMB: 8192,
					DiskMB:   10240,
				},
			},
		},
		{
			name: "with custom domain",
			input: `{
				"domain": "custom.e2b.example.com"
			}`,
			want: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: DefaultTimeoutSeconds,
				LabelPrefix:    DefaultLabelPrefix,
				Domain:         "custom.e2b.example.com",
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
		{
			name: "timeout exceeds max",
			input: `{
				"timeout_seconds": 100000
			}`,
			wantErr: true,
		},
		{
			name: "negative timeout",
			input: `{
				"timeout_seconds": -1
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConfig(json.RawMessage(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseConfigNilData(t *testing.T) {
	cfg, err := ParseConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, DefaultAPIURL, cfg.APIURL)
	assert.Equal(t, DefaultTemplate, cfg.Template)
	assert.Equal(t, DefaultTimeoutSeconds, cfg.TimeoutSeconds)
}

func TestParseSuspendConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *SuspendConfig
		wantErr bool
	}{
		{
			name:  "empty config applies defaults",
			input: `{}`,
			want: &SuspendConfig{
				Strategy:    provider.SuspendStrategyPause,
				MinDuration: Duration(60 * time.Second),
				MaxDuration: Duration(30 * 24 * time.Hour),
				Fallback:    provider.SuspendStrategyTerminate,
			},
		},
		{
			name: "custom strategy",
			input: `{
				"strategy": "terminate",
				"min_duration": "30s",
				"max_duration": "1h",
				"fallback": "pause"
			}`,
			want: &SuspendConfig{
				Strategy:    provider.SuspendStrategyTerminate,
				MinDuration: Duration(30 * time.Second),
				MaxDuration: Duration(1 * time.Hour),
				Fallback:    provider.SuspendStrategyPause,
			},
		},
		{
			name: "duration as number (seconds)",
			input: `{
				"min_duration": 120,
				"max_duration": 3600
			}`,
			want: &SuspendConfig{
				Strategy:    provider.SuspendStrategyPause,
				MinDuration: Duration(120 * time.Second),
				MaxDuration: Duration(3600 * time.Second),
				Fallback:    provider.SuspendStrategyTerminate,
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSuspendConfig(json.RawMessage(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseSuspendConfigNilData(t *testing.T) {
	cfg, err := ParseSuspendConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, cfg.Strategy)
	assert.Equal(t, Duration(60*time.Second), cfg.MinDuration)
}

func TestSuspendConfigToProviderSuspendConfig(t *testing.T) {
	cfg := &SuspendConfig{
		Strategy:    provider.SuspendStrategyPause,
		MinDuration: Duration(2 * time.Minute),
		MaxDuration: Duration(1 * time.Hour),
		Fallback:    provider.SuspendStrategyTerminate,
	}

	providerCfg := cfg.ToProviderSuspendConfig()

	assert.Equal(t, provider.SuspendStrategyPause, providerCfg.Strategy)
	assert.Equal(t, 2*time.Minute, providerCfg.MinDuration)
	assert.Equal(t, 1*time.Hour, providerCfg.MaxDuration)
	assert.Equal(t, provider.SuspendStrategyTerminate, providerCfg.Fallback)
}

func TestDurationMarshalJSON(t *testing.T) {
	d := Duration(5 * time.Minute)
	data, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, `"5m0s"`, string(data))
}

func TestDurationUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Duration
		wantErr bool
	}{
		{
			name:  "string duration",
			input: `"5m"`,
			want:  Duration(5 * time.Minute),
		},
		{
			name:  "integer seconds",
			input: `300`,
			want:  Duration(300 * time.Second),
		},
		{
			name:    "invalid string",
			input:   `"invalid"`,
			wantErr: true,
		},
		{
			name:    "invalid type",
			input:   `true`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tt.input), &d)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d)
		})
	}
}

func TestDurationDuration(t *testing.T) {
	d := Duration(5 * time.Minute)
	assert.Equal(t, 5*time.Minute, d.Duration())
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: 300,
			},
			wantErr: false,
		},
		{
			name: "empty api_url after defaults",
			config: &Config{
				APIURL:         "",
				Template:       DefaultTemplate,
				TimeoutSeconds: 300,
			},
			wantErr: true,
			errMsg:  "api_url",
		},
		{
			name: "negative timeout",
			config: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: -1,
			},
			wantErr: true,
			errMsg:  "timeout_seconds",
		},
		{
			name: "timeout exceeds max",
			config: &Config{
				APIURL:         DefaultAPIURL,
				Template:       DefaultTemplate,
				TimeoutSeconds: MaxTimeoutPro + 1,
			},
			wantErr: true,
			errMsg:  "timeout_seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.validate()
			if tt.wantErr {
				require.Error(t, err)
				var cfgErr *provider.ErrInvalidConfig
				if assert.ErrorAs(t, err, &cfgErr) {
					assert.Contains(t, cfgErr.Field, tt.errMsg)
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

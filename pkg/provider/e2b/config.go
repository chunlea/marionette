package e2b

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
)

const (
	// DefaultAPIURL is the default E2B API endpoint.
	DefaultAPIURL = "https://api.e2b.dev"

	// DefaultTemplate is the default E2B sandbox template.
	DefaultTemplate = "base"

	// DefaultTimeoutSeconds is the default sandbox timeout (5 minutes).
	DefaultTimeoutSeconds = 300

	// MaxTimeoutHobby is the maximum timeout for hobby tier (1 hour).
	MaxTimeoutHobby = 3600

	// MaxTimeoutPro is the maximum timeout for pro tier (24 hours).
	MaxTimeoutPro = 86400

	// DefaultLabelPrefix is the prefix for E2B sandbox metadata.
	DefaultLabelPrefix = "marionette.dev"
)

// Config holds E2B provider settings parsed from provider_configs.config JSON.
type Config struct {
	// APIURL is the E2B API endpoint.
	// Default: https://api.e2b.dev
	APIURL string `json:"api_url"`

	// APIKey is the E2B API key.
	// Can be set in config or via MARIONETTE_E2B_API_KEY environment variable.
	APIKey string `json:"api_key"`

	// Template is the E2B sandbox template to use.
	// Default: "base"
	Template string `json:"template"`

	// TimeoutSeconds is the sandbox timeout in seconds.
	// Default: 300 (5 minutes)
	// Max: 3600 for hobby tier, 86400 for pro tier
	TimeoutSeconds int `json:"timeout_seconds"`

	// LabelPrefix is the prefix for E2B sandbox metadata.
	// Default: "marionette.dev"
	LabelPrefix string `json:"label_prefix"`

	// Resources holds default resource limits (optional).
	Resources ResourceConfig `json:"resources"`

	// Domain is a custom E2B deployment domain (optional).
	// For on-premise E2B deployments.
	Domain string `json:"domain,omitempty"`
}

// ResourceConfig holds default resource limits for E2B sandboxes.
// Note: E2B resource limits are template-based; these are hints for templates.
type ResourceConfig struct {
	// VCPU is the number of vCPUs (template-dependent).
	VCPU int `json:"vcpu,omitempty"`

	// MemoryMB is the memory limit in megabytes (template-dependent).
	MemoryMB int `json:"memory_mb,omitempty"`

	// DiskMB is the disk quota in megabytes (template-dependent).
	DiskMB int `json:"disk_mb,omitempty"`
}

// SuspendConfig holds suspend behavior settings for E2B provider.
type SuspendConfig struct {
	// Strategy is the suspend strategy to use.
	// E2B supports: pause (native), terminate
	Strategy provider.SuspendStrategy `json:"strategy"`

	// MinDuration prevents rapid suspend/resume cycles.
	MinDuration Duration `json:"min_duration"`

	// MaxDuration is max time suspended before auto-terminate.
	// E2B beta pause supports up to 30 days.
	MaxDuration Duration `json:"max_duration"`

	// Fallback is the strategy to use if primary fails.
	Fallback provider.SuspendStrategy `json:"fallback"`
}

// Duration is a wrapper around time.Duration for JSON unmarshaling.
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// Try as number (seconds)
		var n int64
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("invalid duration: %s", string(b))
		}
		*d = Duration(time.Duration(n) * time.Second)
		return nil
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// ParseConfig parses raw JSON into Config with defaults applied.
func ParseConfig(data json.RawMessage) (*Config, error) {
	var cfg Config
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing e2b config: %w", err)
		}
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParseSuspendConfig parses suspend configuration with defaults applied.
func ParseSuspendConfig(data json.RawMessage) (*SuspendConfig, error) {
	cfg := &SuspendConfig{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing suspend config: %w", err)
		}
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.APIURL == "" {
		c.APIURL = DefaultAPIURL
	}
	if c.Template == "" {
		c.Template = DefaultTemplate
	}
	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if c.LabelPrefix == "" {
		c.LabelPrefix = DefaultLabelPrefix
	}
}

func (c *Config) validate() error {
	if c.APIURL == "" {
		return &provider.ErrInvalidConfig{Field: "api_url", Reason: "required"}
	}

	// Validate timeout is within E2B limits
	if c.TimeoutSeconds < 0 {
		return &provider.ErrInvalidConfig{
			Field:  "timeout_seconds",
			Reason: "must be non-negative",
		}
	}
	if c.TimeoutSeconds > MaxTimeoutPro {
		return &provider.ErrInvalidConfig{
			Field:  "timeout_seconds",
			Reason: fmt.Sprintf("exceeds max limit of %d seconds", MaxTimeoutPro),
		}
	}

	return nil
}

func (c *SuspendConfig) applyDefaults() {
	if c.Strategy == "" {
		// E2B supports native pause (beta)
		c.Strategy = provider.SuspendStrategyPause
	}
	if c.MinDuration == 0 {
		c.MinDuration = Duration(60 * time.Second)
	}
	if c.MaxDuration == 0 {
		// E2B beta pause supports up to 30 days
		c.MaxDuration = Duration(30 * 24 * time.Hour)
	}
	if c.Fallback == "" {
		c.Fallback = provider.SuspendStrategyTerminate
	}
}

// ToProviderSuspendConfig converts to provider.SuspendConfig.
func (c *SuspendConfig) ToProviderSuspendConfig() provider.SuspendConfig {
	return provider.SuspendConfig{
		Strategy:    c.Strategy,
		MinDuration: c.MinDuration.Duration(),
		MaxDuration: c.MaxDuration.Duration(),
		Fallback:    c.Fallback,
	}
}

package docker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
)

const (
	// DefaultHost is the default Docker socket path.
	DefaultHost = "unix:///var/run/docker.sock"

	// DefaultImage is the default container image.
	DefaultImage = "marionette/agent:latest"

	// DefaultLabelPrefix is the default prefix for Docker labels.
	DefaultLabelPrefix = "marionette.dev"

	// DefaultMemory is the default memory limit.
	DefaultMemory = "2g"

	// DefaultCPUs is the default CPU limit.
	DefaultCPUs = "2"
)

// Config holds Docker provider settings parsed from provider_configs.config JSON.
type Config struct {
	// Host is the Docker daemon endpoint.
	// Examples: "unix:///var/run/docker.sock", "tcp://localhost:2376"
	Host string `json:"host"`

	// Image is the container image to use for runners.
	Image string `json:"image"`

	// Network is the Docker network to attach containers to.
	Network string `json:"network"`

	// Resources holds default resource limits.
	Resources ResourceConfig `json:"resources"`

	// LabelPrefix is the prefix for Docker labels (default: marionette.dev).
	LabelPrefix string `json:"label_prefix"`
}

// ResourceConfig holds default resource limits.
type ResourceConfig struct {
	// Memory is the memory limit (e.g., "2g", "2048m", "2147483648").
	Memory string `json:"memory"`

	// CPUs is the CPU limit (e.g., "2", "1.5").
	CPUs string `json:"cpus"`
}

// SuspendConfig holds suspend behavior settings.
type SuspendConfig struct {
	// Strategy is the suspend strategy to use.
	Strategy provider.SuspendStrategy `json:"strategy"`

	// MinDuration prevents rapid suspend/resume cycles.
	MinDuration Duration `json:"min_duration"`

	// MaxDuration auto-terminates after this time suspended.
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
			return nil, fmt.Errorf("parsing docker config: %w", err)
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
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Image == "" {
		c.Image = DefaultImage
	}
	if c.LabelPrefix == "" {
		c.LabelPrefix = DefaultLabelPrefix
	}
	if c.Resources.Memory == "" {
		c.Resources.Memory = DefaultMemory
	}
	if c.Resources.CPUs == "" {
		c.Resources.CPUs = DefaultCPUs
	}
}

func (c *Config) validate() error {
	if c.Image == "" {
		return &provider.ErrInvalidConfig{Field: "image", Reason: "required"}
	}

	// Validate host format
	if !strings.HasPrefix(c.Host, "unix://") && !strings.HasPrefix(c.Host, "tcp://") {
		return &provider.ErrInvalidConfig{
			Field:  "host",
			Reason: "must start with unix:// or tcp://",
		}
	}

	// Validate memory format
	if _, err := ParseMemory(c.Resources.Memory); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.memory", Reason: err.Error()}
	}

	// Validate CPU format
	if _, err := ParseCPUs(c.Resources.CPUs); err != nil {
		return &provider.ErrInvalidConfig{Field: "resources.cpus", Reason: err.Error()}
	}

	return nil
}

func (c *SuspendConfig) applyDefaults() {
	if c.Strategy == "" {
		c.Strategy = provider.SuspendStrategyPause
	}
	if c.MinDuration == 0 {
		c.MinDuration = Duration(60 * time.Second)
	}
	if c.MaxDuration == 0 {
		c.MaxDuration = Duration(24 * time.Hour)
	}
	if c.Fallback == "" {
		c.Fallback = provider.SuspendStrategyTerminatePreserveStorage
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

// memoryPattern matches memory strings like "2g", "2048m", "2147483648".
var memoryPattern = regexp.MustCompile(`^(\d+)([gGmMkK])?[bB]?$`)

// ParseMemory converts a memory string to bytes.
// Supports formats: "2g", "2G", "2gb", "2048m", "2048M", "2147483648"
func ParseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory value")
	}

	matches := memoryPattern.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid memory format: %s", s)
	}

	value, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value: %s", s)
	}

	unit := strings.ToLower(matches[2])
	switch unit {
	case "g":
		return value * 1024 * 1024 * 1024, nil
	case "m":
		return value * 1024 * 1024, nil
	case "k":
		return value * 1024, nil
	case "":
		return value, nil
	default:
		return 0, fmt.Errorf("unknown memory unit: %s", unit)
	}
}

// ParseCPUs converts a CPU string to a float64.
// Supports formats: "2", "1.5", "0.5"
func ParseCPUs(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty CPU value")
	}

	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU value: %s", s)
	}

	if value <= 0 {
		return 0, fmt.Errorf("CPU value must be positive: %s", s)
	}

	return value, nil
}

// MemoryMB returns the memory limit in megabytes.
func (c *Config) MemoryMB() (int, error) {
	bytes, err := ParseMemory(c.Resources.Memory)
	if err != nil {
		return 0, err
	}
	return int(bytes / (1024 * 1024)), nil
}

// CPUs returns the CPU limit as a float.
func (c *Config) CPUs() (float64, error) {
	return ParseCPUs(c.Resources.CPUs)
}

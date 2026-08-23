// Package pool implements a pool-based provider for Marionette.
// Pool providers manage runners that join a pool via token authentication,
// rather than being spawned by the server like managed providers.
package pool

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
)

// Config holds the pool provider configuration.
type Config struct {
	// PoolName is the unique identifier for this pool.
	PoolName string `json:"pool_name"`

	// RequiredLabels are labels that runners must have to join this pool.
	RequiredLabels map[string]string `json:"required_labels,omitempty"`

	// RequiredCapabilities are capabilities that runners must have.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`

	// MinRunners is the minimum number of runners in the pool.
	// The watchdog will alert if runners drop below this threshold.
	MinRunners int `json:"min_runners,omitempty"`

	// MaxRunners is the maximum number of runners allowed in the pool.
	MaxRunners int `json:"max_runners,omitempty"`

	// IdleTimeout is how long a runner can be idle before being considered for cleanup.
	// Only applies if the pool supports scaling down.
	IdleTimeout time.Duration `json:"idle_timeout,omitempty"`

	// HealthCheckInterval is how often to check runner health.
	HealthCheckInterval time.Duration `json:"health_check_interval,omitempty"`

	// StaleThreshold is how long before a runner is considered stale (no heartbeat).
	StaleThreshold time.Duration `json:"stale_threshold,omitempty"`

	// MaxTasksPerRunner is the maximum number of tasks a runner can execute
	// before being recycled (tainted). 0 means unlimited.
	MaxTasksPerRunner int `json:"max_tasks_per_runner,omitempty"`

	// SelectionStrategy determines how to select runners from the pool.
	// Options: "lru" (least recently used), "random", "round_robin"
	SelectionStrategy string `json:"selection_strategy,omitempty"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		MinRunners:          0,
		MaxRunners:          100,
		IdleTimeout:         30 * time.Minute,
		HealthCheckInterval: 30 * time.Second,
		StaleThreshold:      90 * time.Second,
		MaxTasksPerRunner:   0, // unlimited
		SelectionStrategy:   "lru",
	}
}

// ParseConfig parses pool configuration from JSON.
func ParseConfig(data json.RawMessage) (*Config, error) {
	cfg := DefaultConfig()

	if len(data) == 0 || string(data) == "null" || string(data) == "{}" {
		return cfg, nil
	}

	// Parse into intermediate struct for duration handling
	var raw struct {
		PoolName             string            `json:"pool_name"`
		RequiredLabels       map[string]string `json:"required_labels,omitempty"`
		RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
		MinRunners           int               `json:"min_runners,omitempty"`
		MaxRunners           int               `json:"max_runners,omitempty"`
		IdleTimeout          string            `json:"idle_timeout,omitempty"`
		HealthCheckInterval  string            `json:"health_check_interval,omitempty"`
		StaleThreshold       string            `json:"stale_threshold,omitempty"`
		MaxTasksPerRunner    int               `json:"max_tasks_per_runner,omitempty"`
		SelectionStrategy    string            `json:"selection_strategy,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing pool config: %w", err)
	}

	cfg.PoolName = raw.PoolName
	cfg.RequiredLabels = raw.RequiredLabels
	cfg.RequiredCapabilities = raw.RequiredCapabilities

	if raw.MinRunners > 0 {
		cfg.MinRunners = raw.MinRunners
	}
	if raw.MaxRunners > 0 {
		cfg.MaxRunners = raw.MaxRunners
	}
	if raw.MaxTasksPerRunner > 0 {
		cfg.MaxTasksPerRunner = raw.MaxTasksPerRunner
	}
	if raw.SelectionStrategy != "" {
		cfg.SelectionStrategy = raw.SelectionStrategy
	}

	// Parse durations
	if raw.IdleTimeout != "" {
		d, err := time.ParseDuration(raw.IdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("parsing idle_timeout: %w", err)
		}
		cfg.IdleTimeout = d
	}
	if raw.HealthCheckInterval != "" {
		d, err := time.ParseDuration(raw.HealthCheckInterval)
		if err != nil {
			return nil, fmt.Errorf("parsing health_check_interval: %w", err)
		}
		cfg.HealthCheckInterval = d
	}
	if raw.StaleThreshold != "" {
		d, err := time.ParseDuration(raw.StaleThreshold)
		if err != nil {
			return nil, fmt.Errorf("parsing stale_threshold: %w", err)
		}
		cfg.StaleThreshold = d
	}

	return cfg, nil
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.PoolName == "" {
		return fmt.Errorf("pool_name is required")
	}

	if c.MaxRunners < c.MinRunners {
		return fmt.Errorf("max_runners (%d) must be >= min_runners (%d)", c.MaxRunners, c.MinRunners)
	}

	switch c.SelectionStrategy {
	case "lru", "random", "round_robin", "":
		// valid
	default:
		return fmt.Errorf("invalid selection_strategy: %s (must be lru, random, or round_robin)", c.SelectionStrategy)
	}

	return nil
}

// SuspendConfig holds suspend configuration for pool providers.
type SuspendConfig struct {
	// Strategy is the suspend strategy for pool providers.
	// For pools, this is typically "release_to_pool".
	Strategy provider.SuspendStrategy `json:"strategy"`

	// MinDuration prevents rapid suspend/resume cycles.
	MinDuration time.Duration `json:"min_duration,omitempty"`

	// MaxDuration auto-terminates after this time suspended.
	MaxDuration time.Duration `json:"max_duration,omitempty"`

	// SyncWorkspace forces workspace sync before suspend.
	SyncWorkspace bool `json:"sync_workspace,omitempty"`
}

// DefaultSuspendConfig returns default suspend configuration for pools.
func DefaultSuspendConfig() *SuspendConfig {
	return &SuspendConfig{
		Strategy:      provider.SuspendStrategyReleaseToPool,
		MinDuration:   60 * time.Second,
		MaxDuration:   24 * time.Hour,
		SyncWorkspace: true,
	}
}

// ParseSuspendConfig parses suspend configuration from JSON.
func ParseSuspendConfig(data json.RawMessage) (*SuspendConfig, error) {
	cfg := DefaultSuspendConfig()

	if len(data) == 0 || string(data) == "null" || string(data) == "{}" {
		return cfg, nil
	}

	var raw struct {
		Strategy      string `json:"strategy"`
		MinDuration   string `json:"min_duration,omitempty"`
		MaxDuration   string `json:"max_duration,omitempty"`
		SyncWorkspace bool   `json:"sync_workspace,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing suspend config: %w", err)
	}

	if raw.Strategy != "" {
		cfg.Strategy = provider.SuspendStrategy(raw.Strategy)
	}
	cfg.SyncWorkspace = raw.SyncWorkspace

	if raw.MinDuration != "" {
		d, err := time.ParseDuration(raw.MinDuration)
		if err != nil {
			return nil, fmt.Errorf("parsing min_duration: %w", err)
		}
		cfg.MinDuration = d
	}
	if raw.MaxDuration != "" {
		d, err := time.ParseDuration(raw.MaxDuration)
		if err != nil {
			return nil, fmt.Errorf("parsing max_duration: %w", err)
		}
		cfg.MaxDuration = d
	}

	return cfg, nil
}

// ToProviderSuspendConfig converts to the provider.SuspendConfig type.
func (c *SuspendConfig) ToProviderSuspendConfig() provider.SuspendConfig {
	return provider.SuspendConfig{
		Strategy:      c.Strategy,
		MinDuration:   c.MinDuration,
		MaxDuration:   c.MaxDuration,
		SyncWorkspace: c.SyncWorkspace,
	}
}

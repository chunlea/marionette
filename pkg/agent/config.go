package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/chunlea/marionette/pkg/storage/cas"
)

const envPrefix = "MARIONETTE"

// Config is the root configuration for the agent.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Runner    RunnerConfig    `mapstructure:"runner"`
	Workspace WorkspaceConfig `mapstructure:"workspace"`
	Heartbeat HeartbeatConfig `mapstructure:"heartbeat"`
	Sandbox   SandboxConfig   `mapstructure:"sandbox"`
	Storage   StorageConfig   `mapstructure:"storage"`
	TLS       TLSConfig       `mapstructure:"tls"`
	Logging   LoggingConfig   `mapstructure:"logging"`
}

// WorkspaceConfig contains workspace settings.
type WorkspaceConfig struct {
	// BasePath is the base directory for workspaces.
	// Default: /workspace
	BasePath string `mapstructure:"base_path"`
}

// Storage backends the runner can sync a workspace to.
const (
	// StorageBackendNone disables workspace sync. Default.
	StorageBackendNone = "none"

	// StorageBackendLocal syncs to a directory the runner can write, which is
	// how a shared volume or a mounted object store is used.
	StorageBackendLocal = "local"
)

// StorageEncryptionNone stores chunks without encryption.
const StorageEncryptionNone = "none"

// StorageConfig controls where the runner syncs workspaces for suspend and
// resume. With no backend configured the runner reports workspace sync as not
// done rather than claiming a snapshot it never wrote.
type StorageConfig struct {
	// Backend is "none" (default) or "local".
	Backend string `mapstructure:"backend"`

	// LocalPath is the directory the local backend writes chunks and
	// manifests into. Required when Backend is "local".
	LocalPath string `mapstructure:"local_path"`

	// Encryption is "none". It has no default on purpose: storing workspace
	// contents unencrypted is an operator decision, not a fallback.
	Encryption string `mapstructure:"encryption"`

	// CAS tunes how a workspace is broken up for storage.
	CAS CASConfig `mapstructure:"cas"`
}

// CASConfig controls the content-addressable storage layout.
//
// The defaults suit a coding agent's workspace and an operator should not have
// to think about them. They are configurable because the right answer depends
// on the workspaces a deployment actually holds, and because a test needs to
// reach the large-workspace path without writing a hundred megabytes.
type CASConfig struct {
	// CDCThreshold is the workspace size, in bytes, at which storage switches
	// from a single tar.zst chunk to content-defined chunking. Zero uses the
	// default of 100 MB.
	CDCThreshold int64 `mapstructure:"cdc_threshold"`

	// CDCMode overrides the threshold: "auto" (default), "always" or "never".
	CDCMode string `mapstructure:"cdc_mode"`

	// MaxConcurrency is how many chunks may be in flight at once. It is the
	// main term in a sync's memory: each one holds up to one maximum-sized
	// chunk. Zero uses the default of 10.
	MaxConcurrency int `mapstructure:"max_concurrency"`
}

// ServerConfig contains gRPC server connection settings.
type ServerConfig struct {
	// Address is the gRPC server address (host:port).
	Address string `mapstructure:"address"`
}

// RunnerConfig contains runner identity and pool settings.
type RunnerConfig struct {
	// Token is the runner authentication token (required).
	// Should be set via MARIONETTE_RUNNER_TOKEN environment variable.
	Token string `mapstructure:"token"`

	// Name is the runner name. If empty, hostname is used.
	Name string `mapstructure:"name"`

	// PoolName is the pool this runner belongs to.
	PoolName string `mapstructure:"pool_name"`

	// Labels are custom key-value labels for runner matching.
	Labels map[string]string `mapstructure:"labels"`

	// Annotations are metadata annotations.
	Annotations map[string]string `mapstructure:"annotations"`
}

// HeartbeatConfig contains heartbeat timing settings.
type HeartbeatConfig struct {
	// Interval is the time between heartbeats. Default: 30s.
	Interval time.Duration `mapstructure:"interval"`

	// Timeout is the heartbeat RPC timeout. Default: 10s.
	Timeout time.Duration `mapstructure:"timeout"`
}

// SandboxConfig contains sandbox mode settings.
type SandboxConfig struct {
	// Mode is the sandbox mode: "runner-is-sandbox" or "runner-creates-sandbox".
	Mode string `mapstructure:"mode"`

	// Types is the list of available sandbox types.
	// If empty, capabilities are auto-detected.
	Types []string `mapstructure:"types"`
}

// TLSConfig contains TLS settings for gRPC connection.
type TLSConfig struct {
	// Enabled enables TLS for gRPC connection.
	Enabled bool `mapstructure:"enabled"`

	// CertFile is the path to the client certificate.
	CertFile string `mapstructure:"cert_file"`

	// KeyFile is the path to the client private key.
	KeyFile string `mapstructure:"key_file"`

	// CAFile is the path to the CA certificate for server verification.
	CAFile string `mapstructure:"ca_file"`

	// SkipVerify skips server certificate verification (insecure, dev only).
	SkipVerify bool `mapstructure:"skip_verify"`
}

// LoggingConfig contains logging settings.
type LoggingConfig struct {
	// Level is the log level: debug, info, warn, error.
	Level string `mapstructure:"level"`

	// Format is the log format: json or console.
	Format string `mapstructure:"format"`
}

// BindFlags defines command-line flags for agent configuration.
// Note: Flags are bound to viper in LoadWithFlags, not here.
func BindFlags(flags *pflag.FlagSet) {
	flags.String("server", "localhost:9090", "gRPC server address")
	flags.String("token", "", "Runner authentication token (or set MARIONETTE_RUNNER_TOKEN)")
	flags.String("name", "", "Runner name (defaults to hostname)")
	flags.String("pool", "", "Pool name for pool runners")
	flags.String("workspace", "/workspace", "Base path for workspaces")
	flags.String("sandbox-mode", "runner-is-sandbox", "Sandbox mode: runner-is-sandbox, runner-creates-sandbox, or none")
	flags.String("storage-backend", "none", "Workspace sync backend: none or local")
	flags.String("storage-local-path", "", "Directory the local storage backend writes to")
	flags.String("storage-encryption", "", "Workspace chunk encryption: none (no default; must be set when a backend is configured)")
	flags.String("log-level", "info", "Log level: debug, info, warn, error")
	flags.String("log-format", "json", "Log format: json or console")
	flags.Bool("tls", false, "Enable TLS for gRPC connection")
	flags.Bool("tls-skip-verify", false, "Skip TLS certificate verification (insecure)")
	flags.String("tls-cert", "", "Path to TLS client certificate")
	flags.String("tls-key", "", "Path to TLS client key")
	flags.String("tls-ca", "", "Path to TLS CA certificate")
}

// bindPFlags binds parsed pflags to a viper instance.
func bindPFlags(v *viper.Viper, flags *pflag.FlagSet) {
	_ = v.BindPFlag("server.address", flags.Lookup("server"))
	_ = v.BindPFlag("runner.token", flags.Lookup("token"))
	_ = v.BindPFlag("runner.name", flags.Lookup("name"))
	_ = v.BindPFlag("runner.pool_name", flags.Lookup("pool"))
	_ = v.BindPFlag("workspace.base_path", flags.Lookup("workspace"))
	_ = v.BindPFlag("sandbox.mode", flags.Lookup("sandbox-mode"))
	_ = v.BindPFlag("storage.backend", flags.Lookup("storage-backend"))
	_ = v.BindPFlag("storage.local_path", flags.Lookup("storage-local-path"))
	_ = v.BindPFlag("storage.encryption", flags.Lookup("storage-encryption"))
	_ = v.BindPFlag("logging.level", flags.Lookup("log-level"))
	_ = v.BindPFlag("logging.format", flags.Lookup("log-format"))
	_ = v.BindPFlag("tls.enabled", flags.Lookup("tls"))
	_ = v.BindPFlag("tls.skip_verify", flags.Lookup("tls-skip-verify"))
	_ = v.BindPFlag("tls.cert_file", flags.Lookup("tls-cert"))
	_ = v.BindPFlag("tls.key_file", flags.Lookup("tls-key"))
	_ = v.BindPFlag("tls.ca_file", flags.Lookup("tls-ca"))
}

// LoadWithFlags loads configuration from file, environment variables, and command-line flags.
// This is the primary loading function when flags are available.
func LoadWithFlags(configPath string, flags *pflag.FlagSet) (*Config, error) {
	v := viper.New()
	setDefaults(v)

	// Bind command-line flags first (highest priority after env vars)
	if flags != nil {
		bindPFlags(v, flags)
	}

	// Environment variable binding
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Explicitly bind environment variables for nested fields
	// This is required for Unmarshal to pick up env vars properly
	bindEnvVars(v)

	// Load config file if specified
	if configPath != "" {
		v.SetConfigFile(configPath)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Apply hostname default for runner name
	if cfg.Runner.Name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		cfg.Runner.Name = hostname
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// Load loads configuration from file and environment variables only (no flags).
// For loading with command-line flags, use LoadWithFlags instead.
func Load(configPath string) (*Config, error) {
	return LoadWithFlags(configPath, nil)
}

// LoadWithDefaults loads configuration with defaults and environment variables only.
func LoadWithDefaults() (*Config, error) {
	return Load("")
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.address", "localhost:9090")

	// Workspace defaults
	v.SetDefault("workspace.base_path", "/workspace")

	// Heartbeat defaults
	v.SetDefault("heartbeat.interval", "30s")
	v.SetDefault("heartbeat.timeout", "10s")

	// Sandbox defaults
	v.SetDefault("sandbox.mode", "runner-is-sandbox")

	// Storage: sync is off unless an operator turns it on. Encryption has no
	// default so that enabling a backend forces an explicit choice.
	v.SetDefault("storage.backend", StorageBackendNone)
	v.SetDefault("storage.cas.cdc_mode", cas.CDCModeAuto)

	// TLS defaults
	v.SetDefault("tls.enabled", false)
	v.SetDefault("tls.skip_verify", false)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
}

// bindEnvVars explicitly binds environment variables to config keys.
// This is required because viper's AutomaticEnv doesn't work well with
// Unmarshal for nested struct fields.
func bindEnvVars(v *viper.Viper) {
	// Server
	_ = v.BindEnv("server.address")

	// Runner
	_ = v.BindEnv("runner.token")
	_ = v.BindEnv("runner.name")
	_ = v.BindEnv("runner.pool_name")

	// Workspace
	_ = v.BindEnv("workspace.base_path")

	// Heartbeat
	_ = v.BindEnv("heartbeat.interval")
	_ = v.BindEnv("heartbeat.timeout")

	// Sandbox
	_ = v.BindEnv("sandbox.mode")

	// TLS
	_ = v.BindEnv("tls.enabled")
	_ = v.BindEnv("tls.skip_verify")
	_ = v.BindEnv("tls.cert_file")
	_ = v.BindEnv("tls.key_file")
	_ = v.BindEnv("tls.ca_file")

	// Logging
	_ = v.BindEnv("logging.level")
	_ = v.BindEnv("logging.format")
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.Server.Address == "" {
		return fmt.Errorf("%w: server.address is required", ErrInvalidConfig)
	}

	if c.Runner.Token == "" {
		return fmt.Errorf("%w: runner.token is required (set MARIONETTE_RUNNER_TOKEN)", ErrInvalidConfig)
	}

	validModes := map[string]bool{
		"runner-is-sandbox":      true,
		"runner-creates-sandbox": true,
		"none":                   true,
	}
	if !validModes[c.Sandbox.Mode] {
		return fmt.Errorf("%w: sandbox.mode must be 'runner-is-sandbox', 'runner-creates-sandbox', or 'none', got %q",
			ErrInvalidConfig, c.Sandbox.Mode)
	}

	// Storage validation. A misconfigured backend must fail at startup rather
	// than at the first suspend, where the only symptom would be a workspace
	// that quietly never got saved.
	switch c.Storage.Backend {
	case "", StorageBackendNone:
		// Sync disabled; nothing else to check.
	case StorageBackendLocal:
		if c.Storage.LocalPath == "" {
			return fmt.Errorf("%w: storage.local_path is required when storage.backend is %q",
				ErrInvalidConfig, StorageBackendLocal)
		}
		if c.Storage.Encryption == "" {
			return fmt.Errorf("%w: storage.encryption must be set explicitly when storage.backend is configured",
				ErrInvalidConfig)
		}
		if c.Storage.Encryption != StorageEncryptionNone {
			return fmt.Errorf("%w: storage.encryption must be %q, got %q",
				ErrInvalidConfig, StorageEncryptionNone, c.Storage.Encryption)
		}
		if err := c.Storage.CAS.validate(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: storage.backend must be %q or %q, got %q",
			ErrInvalidConfig, StorageBackendNone, StorageBackendLocal, c.Storage.Backend)
	}

	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("%w: logging.level must be debug, info, warn, or error, got %q",
			ErrInvalidConfig, c.Logging.Level)
	}

	validLogFormats := map[string]bool{
		"json":    true,
		"console": true,
	}
	if !validLogFormats[c.Logging.Format] {
		return fmt.Errorf("%w: logging.format must be json or console, got %q",
			ErrInvalidConfig, c.Logging.Format)
	}

	// TLS validation
	if c.TLS.Enabled {
		if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
			return fmt.Errorf("%w: tls.key_file is required when tls.cert_file is set", ErrInvalidConfig)
		}
		if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
			return fmt.Errorf("%w: tls.cert_file is required when tls.key_file is set", ErrInvalidConfig)
		}
	}

	return nil
}

// validate checks the CAS tuning knobs.
func (c CASConfig) validate() error {
	switch c.CDCMode {
	case "", cas.CDCModeAuto, cas.CDCModeAlways, cas.CDCModeNever:
	default:
		return fmt.Errorf("%w: storage.cas.cdc_mode must be %q, %q or %q, got %q",
			ErrInvalidConfig, cas.CDCModeAuto, cas.CDCModeAlways, cas.CDCModeNever, c.CDCMode)
	}
	if c.CDCThreshold < 0 {
		return fmt.Errorf("%w: storage.cas.cdc_threshold must not be negative, got %d",
			ErrInvalidConfig, c.CDCThreshold)
	}
	if c.MaxConcurrency < 0 {
		return fmt.Errorf("%w: storage.cas.max_concurrency must not be negative, got %d",
			ErrInvalidConfig, c.MaxConcurrency)
	}
	return nil
}

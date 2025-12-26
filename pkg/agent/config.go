package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const envPrefix = "MARIONETTE"

// Config is the root configuration for the agent.
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	Runner    RunnerConfig    `mapstructure:"runner"`
	Heartbeat HeartbeatConfig `mapstructure:"heartbeat"`
	Sandbox   SandboxConfig   `mapstructure:"sandbox"`
	TLS       TLSConfig       `mapstructure:"tls"`
	Logging   LoggingConfig   `mapstructure:"logging"`
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

// BindFlags binds command-line flags to viper for configuration.
func BindFlags(flags *pflag.FlagSet) {
	flags.String("server", "localhost:9090", "gRPC server address")
	flags.String("token", "", "Runner authentication token (or set MARIONETTE_RUNNER_TOKEN)")
	flags.String("name", "", "Runner name (defaults to hostname)")
	flags.String("pool", "", "Pool name for pool runners")
	flags.String("sandbox-mode", "runner-is-sandbox", "Sandbox mode: runner-is-sandbox or runner-creates-sandbox")
	flags.String("log-level", "info", "Log level: debug, info, warn, error")
	flags.String("log-format", "json", "Log format: json or console")
	flags.Bool("tls", false, "Enable TLS for gRPC connection")
	flags.Bool("tls-skip-verify", false, "Skip TLS certificate verification (insecure)")
	flags.String("tls-cert", "", "Path to TLS client certificate")
	flags.String("tls-key", "", "Path to TLS client key")
	flags.String("tls-ca", "", "Path to TLS CA certificate")

	_ = viper.BindPFlag("server.address", flags.Lookup("server"))
	_ = viper.BindPFlag("runner.token", flags.Lookup("token"))
	_ = viper.BindPFlag("runner.name", flags.Lookup("name"))
	_ = viper.BindPFlag("runner.pool_name", flags.Lookup("pool"))
	_ = viper.BindPFlag("sandbox.mode", flags.Lookup("sandbox-mode"))
	_ = viper.BindPFlag("logging.level", flags.Lookup("log-level"))
	_ = viper.BindPFlag("logging.format", flags.Lookup("log-format"))
	_ = viper.BindPFlag("tls.enabled", flags.Lookup("tls"))
	_ = viper.BindPFlag("tls.skip_verify", flags.Lookup("tls-skip-verify"))
	_ = viper.BindPFlag("tls.cert_file", flags.Lookup("tls-cert"))
	_ = viper.BindPFlag("tls.key_file", flags.Lookup("tls-key"))
	_ = viper.BindPFlag("tls.ca_file", flags.Lookup("tls-ca"))
}

// Load loads configuration from file, environment variables, and flags.
func Load(configPath string) (*Config, error) {
	v := viper.New()
	setDefaults(v)

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

// LoadWithDefaults loads configuration with defaults and environment variables only.
func LoadWithDefaults() (*Config, error) {
	return Load("")
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.address", "localhost:9090")

	// Heartbeat defaults
	v.SetDefault("heartbeat.interval", "30s")
	v.SetDefault("heartbeat.timeout", "10s")

	// Sandbox defaults
	v.SetDefault("sandbox.mode", "runner-is-sandbox")

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
	}
	if !validModes[c.Sandbox.Mode] {
		return fmt.Errorf("%w: sandbox.mode must be 'runner-is-sandbox' or 'runner-creates-sandbox', got %q",
			ErrInvalidConfig, c.Sandbox.Mode)
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

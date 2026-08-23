package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const envPrefix = "MARIONETTE"

// Load reads configuration from a YAML file and overlays environment variables.
// Environment variables use the MARIONETTE_ prefix and use underscores as separators.
// For example, MARIONETTE_SERVER_API_PORT overrides server.api.port in the YAML.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Configure viper
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Enable environment variable overrides
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// LoadWithDefaults loads configuration using only defaults and environment variables,
// without requiring a config file. Useful for testing and simple deployments.
func LoadWithDefaults() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Enable environment variable overrides
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Unmarshal into struct
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets sensible default values for all configuration options.
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.api.host", "0.0.0.0")
	v.SetDefault("server.api.port", 8080)
	v.SetDefault("server.admin.host", "127.0.0.1")
	v.SetDefault("server.admin.port", 8081)
	v.SetDefault("server.grpc.host", "0.0.0.0")
	v.SetDefault("server.grpc.port", 9090)

	// Database defaults
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")

	// Provider defaults
	v.SetDefault("providers.default", "docker")
	v.SetDefault("providers.docker.image", "marionette/agent:latest")
	v.SetDefault("providers.docker.network", "marionette-network")

	// Storage defaults
	v.SetDefault("storage.provider", "local")
	v.SetDefault("storage.local.path", "./data/storage")

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")

	// TLS defaults
	v.SetDefault("tls.enabled", false)
	v.SetDefault("tls.verify_client", false)

	// Development defaults
	v.SetDefault("dev.hot_reload", false)
	v.SetDefault("dev.skip_tls", false)

	// Observability defaults
	v.SetDefault("observability.metrics.enabled", false)
	v.SetDefault("observability.metrics.host", "")
	v.SetDefault("observability.metrics.port", 9091)
	v.SetDefault("observability.metrics.path", "/metrics")
	v.SetDefault("observability.metrics.namespace", "marionette")
	v.SetDefault("observability.health.enabled", true)

	// Tracing defaults
	v.SetDefault("observability.tracing.enabled", false)
	v.SetDefault("observability.tracing.exporter", "otlp")
	v.SetDefault("observability.tracing.endpoint", "localhost:4317")
	v.SetDefault("observability.tracing.service_name", "marionette-server")
	v.SetDefault("observability.tracing.sample_rate", 0.1)
	v.SetDefault("observability.tracing.insecure", true)

	// Streaming (frozen subsystem: off unless explicitly enabled)
	v.SetDefault("streaming.enabled", false)

	// Tunnels (live path: on unless explicitly disabled)
	v.SetDefault("tunnels.enabled", true)

	// Single-tenant unless a deployment says otherwise.
	v.SetDefault("multi_tenant", false)

	// Chunk GC deletes blobs; off until workspace sync references them.
	v.SetDefault("storage.gc.enabled", false)
}

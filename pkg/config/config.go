// Package config provides configuration loading and validation for the Marionette server.
// It uses Viper to load configuration from YAML files with environment variable overlays.
// Sensitive values (database URL, encryption keys) must come from environment variables only.
package config

// Config is the root configuration structure for the Marionette server.
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Database      DatabaseConfig      `mapstructure:"database"`
	Providers     ProvidersConfig     `mapstructure:"providers"`
	Storage       StorageConfig       `mapstructure:"storage"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	TLS           TLSConfig           `mapstructure:"tls"`
	Development   DevelopmentConfig   `mapstructure:"dev"`
	Observability ObservabilityConfig `mapstructure:"observability"`
}

// ServerConfig holds configuration for all server endpoints.
type ServerConfig struct {
	API   EndpointConfig `mapstructure:"api"`
	Admin EndpointConfig `mapstructure:"admin"`
	GRPC  EndpointConfig `mapstructure:"grpc"`
}

// EndpointConfig holds configuration for a single server endpoint.
type EndpointConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// DatabaseConfig holds database connection settings.
// URL is loaded from MARIONETTE_DATABASE_URL environment variable only.
type DatabaseConfig struct {
	URL             string `mapstructure:"url"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime"`
}

// ProvidersConfig holds provider configuration.
type ProvidersConfig struct {
	Default string                `mapstructure:"default"`
	Docker  *DockerProviderConfig `mapstructure:"docker"`
}

// DockerProviderConfig holds Docker-specific provider settings.
type DockerProviderConfig struct {
	Image   string `mapstructure:"image"`
	Network string `mapstructure:"network"`
}

// StorageConfig holds storage backend configuration.
type StorageConfig struct {
	Provider string              `mapstructure:"provider"`
	Local    *LocalStorageConfig `mapstructure:"local"`
	S3       *S3StorageConfig    `mapstructure:"s3"`
}

// LocalStorageConfig holds local filesystem storage settings.
type LocalStorageConfig struct {
	Path string `mapstructure:"path"`
}

// S3StorageConfig holds S3 storage settings.
type S3StorageConfig struct {
	Bucket string `mapstructure:"bucket"`
	Region string `mapstructure:"region"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// TLSConfig holds TLS/mTLS configuration.
type TLSConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	CertFile     string `mapstructure:"cert_file"`
	KeyFile      string `mapstructure:"key_file"`
	CAFile       string `mapstructure:"ca_file"`
	VerifyClient bool   `mapstructure:"verify_client"`
}

// DevelopmentConfig holds development-only settings.
type DevelopmentConfig struct {
	HotReload bool `mapstructure:"hot_reload"`
	SkipTLS   bool `mapstructure:"skip_tls"`
}

// ObservabilityConfig holds observability settings.
type ObservabilityConfig struct {
	Metrics MetricsConfig `mapstructure:"metrics"`
	Health  HealthConfig  `mapstructure:"health"`
}

// MetricsConfig holds metrics/prometheus configuration.
type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// HealthConfig holds health check configuration.
type HealthConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

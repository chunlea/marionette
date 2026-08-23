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
	Streaming     StreamingConfig     `mapstructure:"streaming"`

	// MultiTenant turns tenant isolation from a column into an enforced
	// boundary.
	//
	// Off (the default) is a single-tenant deployment: nothing writes a
	// tenant_id, every row has a NULL one, and everything behaves as it always
	// has. On, every API key must be scoped to a tenant, the store binds that
	// tenant for each statement, and the row level security policies added in
	// migration 008 hide everything else - including from a query that forgot
	// its WHERE clause.
	MultiTenant bool          `mapstructure:"multi_tenant"`
	Tunnels     TunnelsConfig `mapstructure:"tunnels"`
}

// StreamingConfig gates the desktop/browser streaming subsystem.
//
// Streaming is frozen: the SFU has no media source, no renegotiation and
// never reads RTCP, so it cannot deliver a frame. It stays compiled but must
// not register itself as a provider unless someone explicitly opts in.
type StreamingConfig struct {
	// Enabled turns the streaming subsystem on. Default false.
	Enabled bool `mapstructure:"enabled"`
}

// TunnelsConfig gates the tunnel subsystem (HTTP proxy and TCP relay).
type TunnelsConfig struct {
	// Enabled turns tunnels on. Default true: this is a live product path.
	Enabled bool `mapstructure:"enabled"`
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
	Default    string                    `mapstructure:"default"`
	Docker     *DockerProviderConfig     `mapstructure:"docker"`
	Kubernetes *KubernetesProviderConfig `mapstructure:"kubernetes"`
}

// DockerProviderConfig holds Docker-specific provider settings.
type DockerProviderConfig struct {
	// Host is the Docker daemon endpoint.
	// Examples: "unix:///var/run/docker.sock", "tcp://localhost:2376"
	Host string `mapstructure:"host"`

	// Image is the container image to use for runners.
	Image string `mapstructure:"image"`

	// Network is the Docker network to attach containers to.
	// If empty, containers use the default bridge network.
	Network string `mapstructure:"network"`

	// Resources holds default resource limits for containers.
	Resources DockerResourcesConfig `mapstructure:"resources"`
}

// DockerResourcesConfig holds Docker container resource limits.
type DockerResourcesConfig struct {
	// Memory is the memory limit (e.g., "2g", "2048m").
	Memory string `mapstructure:"memory"`

	// CPUs is the CPU limit (e.g., "2", "1.5").
	CPUs string `mapstructure:"cpus"`
}

// KubernetesProviderConfig holds Kubernetes-specific provider settings.
type KubernetesProviderConfig struct {
	// Kubeconfig is the path to the kubeconfig file.
	// If empty, uses in-cluster config or default kubeconfig location.
	Kubeconfig string `mapstructure:"kubeconfig"`

	// Context is the kubeconfig context to use.
	Context string `mapstructure:"context"`

	// Namespace is the Kubernetes namespace for runner pods.
	Namespace string `mapstructure:"namespace"`

	// Image is the container image to use for runners.
	Image string `mapstructure:"image"`

	// ServiceAccount is the service account for runner pods.
	ServiceAccount string `mapstructure:"service_account"`

	// Resources holds default resource limits for pods.
	Resources KubernetesResourcesConfig `mapstructure:"resources"`

	// Storage holds PVC configuration for workspaces.
	Storage KubernetesStorageConfig `mapstructure:"storage"`

	// NodeSelector for pod scheduling.
	NodeSelector map[string]string `mapstructure:"node_selector"`

	// Tolerations for pod scheduling.
	Tolerations []KubernetesTolerationConfig `mapstructure:"tolerations"`
}

// KubernetesResourcesConfig holds Kubernetes pod resource limits.
type KubernetesResourcesConfig struct {
	// Memory is the memory limit (e.g., "2Gi", "2048Mi").
	Memory string `mapstructure:"memory"`

	// MemoryRequest is the memory request (defaults to Memory if not set).
	MemoryRequest string `mapstructure:"memory_request"`

	// CPUs is the CPU limit (e.g., "2", "500m").
	CPUs string `mapstructure:"cpus"`

	// CPURequest is the CPU request (defaults to CPUs if not set).
	CPURequest string `mapstructure:"cpu_request"`
}

// KubernetesStorageConfig holds PVC configuration.
type KubernetesStorageConfig struct {
	// Size is the PVC size (e.g., "10Gi").
	Size string `mapstructure:"size"`

	// StorageClass is the storage class name.
	StorageClass string `mapstructure:"storage_class"`

	// AccessMode is the PVC access mode (e.g., "ReadWriteOnce").
	AccessMode string `mapstructure:"access_mode"`
}

// KubernetesTolerationConfig holds a single toleration.
type KubernetesTolerationConfig struct {
	Key      string `mapstructure:"key"`
	Operator string `mapstructure:"operator"`
	Value    string `mapstructure:"value"`
	Effect   string `mapstructure:"effect"`
}

// StorageConfig holds storage backend configuration.
type StorageConfig struct {
	Provider  string                 `mapstructure:"provider"`
	Local     *LocalStorageConfig    `mapstructure:"local"`
	S3        *S3StorageConfig       `mapstructure:"s3"`
	Workspace WorkspaceStorageConfig `mapstructure:"workspace"`
}

// WorkspaceStorageConfig holds workspace storage settings.
type WorkspaceStorageConfig struct {
	// BaseDir is the base directory for workspace storage on the host.
	// Each workspace will be created as a subdirectory under this path.
	// Defaults to /var/marionette/workspaces
	BaseDir string `mapstructure:"base_dir"`

	// DefaultQuotaMB is the default disk quota for workspaces in megabytes.
	// Set to 0 for unlimited.
	DefaultQuotaMB int `mapstructure:"default_quota_mb"`

	// CleanupOnTerminate controls whether workspaces are deleted when sessions terminate.
	// Defaults to false (workspaces are retained).
	CleanupOnTerminate bool `mapstructure:"cleanup_on_terminate"`
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
	Tracing TracingConfig `mapstructure:"tracing"`
}

// MetricsConfig holds metrics/prometheus configuration.
type MetricsConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Path      string `mapstructure:"path"`
	Namespace string `mapstructure:"namespace"`
}

// HealthConfig holds health check configuration.
type HealthConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// TracingConfig holds OpenTelemetry tracing configuration.
type TracingConfig struct {
	// Enabled controls whether tracing is enabled.
	Enabled bool `mapstructure:"enabled"`

	// Exporter specifies the exporter type: "otlp", "stdout", or "noop".
	Exporter string `mapstructure:"exporter"`

	// Endpoint is the collector endpoint (for OTLP exporter).
	// Example: "localhost:4317" for gRPC.
	Endpoint string `mapstructure:"endpoint"`

	// ServiceName is the name of the service for tracing.
	ServiceName string `mapstructure:"service_name"`

	// SampleRate is the sampling rate (0.0 to 1.0).
	// 1.0 means all traces are sampled, 0.1 means 10% of traces.
	SampleRate float64 `mapstructure:"sample_rate"`

	// Insecure disables TLS for the OTLP exporter.
	Insecure bool `mapstructure:"insecure"`
}

package config

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/chunlea/marionette/pkg/network"
)

// Valid log levels
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// Valid log formats
var validLogFormats = map[string]bool{
	"json":    true,
	"console": true,
}

// Valid storage providers
var validStorageProviders = map[string]bool{
	"local": true,
	"s3":    true,
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	var errs []error

	// Validate server config
	if err := c.Server.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("server: %w", err))
	}

	// Validate logging config
	if err := c.Logging.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("logging: %w", err))
	}

	// Validate storage config
	if err := c.Storage.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("storage: %w", err))
	}

	// Validate TLS config
	if err := c.TLS.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("tls: %w", err))
	}

	// Validate provider network isolation. A malformed proxy or server address
	// must fail at startup, not when the first restricted session spawns and
	// finds it has nowhere to send anything.
	if c.Providers.Docker != nil {
		if err := c.Providers.Docker.Isolation.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("providers.docker.isolation: %w", err))
		}
	}
	if c.Providers.Kubernetes != nil {
		if err := c.Providers.Kubernetes.Isolation.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("providers.kubernetes.isolation: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Validate checks the server configuration.
func (c *ServerConfig) Validate() error {
	var errs []error

	if err := c.API.Validate("api"); err != nil {
		errs = append(errs, err)
	}
	if err := c.Admin.Validate("admin"); err != nil {
		errs = append(errs, err)
	}
	if err := c.GRPC.Validate("grpc"); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Validate checks the endpoint configuration.
func (c *EndpointConfig) Validate(name string) error {
	if err := validatePort(c.Port, name); err != nil {
		return err
	}
	return nil
}

// Validate checks the logging configuration.
func (c *LoggingConfig) Validate() error {
	var errs []error

	if !validLogLevels[c.Level] {
		errs = append(errs, fmt.Errorf("invalid log level %q, must be one of: debug, info, warn, error", c.Level))
	}

	if !validLogFormats[c.Format] {
		errs = append(errs, fmt.Errorf("invalid log format %q, must be one of: json, console", c.Format))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Validate checks the storage configuration.
func (c *StorageConfig) Validate() error {
	if !validStorageProviders[c.Provider] {
		return fmt.Errorf("invalid storage provider %q, must be one of: local, s3", c.Provider)
	}

	switch c.Provider {
	case "local":
		if c.Local == nil || c.Local.Path == "" {
			return errors.New("local storage requires storage.local.path to be set")
		}
	case "s3":
		if c.S3 == nil {
			return errors.New("s3 storage requires storage.s3 configuration")
		}
		if c.S3.Bucket == "" {
			return errors.New("s3 storage requires storage.s3.bucket to be set")
		}
		if c.S3.Region == "" {
			return errors.New("s3 storage requires storage.s3.region to be set")
		}
	}

	return c.LogArchive.Validate()
}

// Validate checks the TLS configuration.
func (c *TLSConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	var errs []error

	if c.CertFile == "" {
		errs = append(errs, errors.New("tls.cert_file is required when TLS is enabled"))
	}
	if c.KeyFile == "" {
		errs = append(errs, errors.New("tls.key_file is required when TLS is enabled"))
	}
	if c.VerifyClient && c.CAFile == "" {
		errs = append(errs, errors.New("tls.ca_file is required when verify_client is enabled"))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// validatePort checks that a port number is valid.
// Validate checks the network isolation settings.
func (c *NetworkIsolationConfig) Validate() error {
	if c == nil {
		return nil
	}

	if c.ServerURL != "" {
		if _, err := network.ParseEndpoint(c.ServerURL, network.DefaultControlPlanePort); err != nil {
			return fmt.Errorf("server_url: %w", err)
		}
	}

	if c.ProxyURL != "" {
		if _, err := network.ParseProxyConfig(c.ProxyURL, c.ProxyNoProxy, c.ProxyCACert); err != nil {
			return fmt.Errorf("proxy_url: %w", err)
		}
	}

	for _, addr := range c.DNSServers {
		if net.ParseIP(addr) == nil {
			return fmt.Errorf("dns_servers: %q is not an IP address", addr)
		}
	}

	if c.RefreshInterval != "" {
		d, err := time.ParseDuration(c.RefreshInterval)
		if err != nil {
			return fmt.Errorf("refresh_interval: %w", err)
		}
		if d <= 0 {
			return fmt.Errorf("refresh_interval must be positive, got %q", c.RefreshInterval)
		}
	}

	return nil
}

func validatePort(port int, name string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535, got %d", name, port)
	}
	return nil
}

// Validate checks the log archiving configuration.
//
// The one rule worth enforcing is the ordering of the two retentions. Partition
// retention ages logs out of the database; archive retention deletes the copy
// that made that safe. Configured the wrong way round they do not conflict -
// they simply delete the logs, first from one place and then from the other.
func (c *StorageLogArchiveConfig) Validate() error {
	if !c.Enabled {
		// Retention is gated on archiving at the job level too, so a stale
		// retention_days in a config file with archiving off is harmless.
		return nil
	}

	if c.Interval < 0 {
		return errors.New("storage.log_archive.interval must not be negative")
	}
	if c.RetentionDays < 0 {
		return errors.New("storage.log_archive.retention_days must not be negative")
	}
	if c.Retention < 0 {
		return errors.New("storage.log_archive.retention must not be negative")
	}

	if c.RetentionDays > 0 && c.Retention > 0 {
		partitionRetention := time.Duration(c.RetentionDays) * 24 * time.Hour
		if c.Retention <= partitionRetention {
			return fmt.Errorf(
				"storage.log_archive.retention (%s) must outlast retention_days (%d days): "+
					"the archive is the copy that makes dropping partitions safe",
				c.Retention, c.RetentionDays)
		}
	}

	return nil
}

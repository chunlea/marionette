package config

import (
	"errors"
	"fmt"
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

	return nil
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
func validatePort(port int, name string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535, got %d", name, port)
	}
	return nil
}

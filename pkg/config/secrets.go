package config

import (
	"errors"
	"fmt"
	"os"
)

// Environment variable names for sensitive values.
// These values are never stored in config files.
//
//nolint:gosec // These are environment variable names, not credentials
const (
	EnvDatabaseURL   = "MARIONETTE_DATABASE_URL"
	EnvMasterKey     = "MARIONETTE_MASTER_KEY"
	EnvEncryptionKey = "MARIONETTE_ENCRYPTION_KEY"
	EnvUIUsername    = "MARIONETTE_UI_USERNAME"
	EnvUIPassword    = "MARIONETTE_UI_PASSWORD"
)

// Secrets holds sensitive configuration values that are loaded exclusively
// from environment variables. These values are never logged or serialized.
type Secrets struct {
	// DatabaseURL is the PostgreSQL connection string.
	// Required for all deployments.
	DatabaseURL string

	// MasterKey is used for admin operations and API key generation.
	// Required for production deployments.
	MasterKey string

	// EncryptionKey is used for encrypting agent credentials at rest.
	// Required for production deployments.
	EncryptionKey string

	// UIUsername is the Basic Auth username for the admin WebUI.
	// Optional, but required if UIPassword is set.
	UIUsername string

	// UIPassword is the Basic Auth password for the admin WebUI.
	// Optional, but required if UIUsername is set.
	UIPassword string
}

// LoadSecrets loads sensitive configuration from environment variables.
// It returns an error if required environment variables are missing.
func LoadSecrets() (*Secrets, error) {
	s := &Secrets{
		DatabaseURL:   os.Getenv(EnvDatabaseURL),
		MasterKey:     os.Getenv(EnvMasterKey),
		EncryptionKey: os.Getenv(EnvEncryptionKey),
		UIUsername:    os.Getenv(EnvUIUsername),
		UIPassword:    os.Getenv(EnvUIPassword),
	}

	if err := s.Validate(); err != nil {
		return nil, err
	}

	return s, nil
}

// LoadSecretsOptional loads secrets without requiring any to be set.
// Useful for local development and testing.
func LoadSecretsOptional() *Secrets {
	return &Secrets{
		DatabaseURL:   os.Getenv(EnvDatabaseURL),
		MasterKey:     os.Getenv(EnvMasterKey),
		EncryptionKey: os.Getenv(EnvEncryptionKey),
		UIUsername:    os.Getenv(EnvUIUsername),
		UIPassword:    os.Getenv(EnvUIPassword),
	}
}

// Validate checks that secrets are properly configured.
func (s *Secrets) Validate() error {
	var errs []error

	// Database URL is required
	if s.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvDatabaseURL))
	}

	// If either UI credential is set, both must be set
	if (s.UIUsername != "") != (s.UIPassword != "") {
		errs = append(errs, fmt.Errorf("both %s and %s must be set together", EnvUIUsername, EnvUIPassword))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// ValidateForProduction performs stricter validation for production deployments.
func (s *Secrets) ValidateForProduction() error {
	var errs []error

	// First run basic validation
	if err := s.Validate(); err != nil {
		errs = append(errs, err)
	}

	// Master key is required in production
	if s.MasterKey == "" {
		errs = append(errs, fmt.Errorf("%s is required for production", EnvMasterKey))
	}

	// Encryption key is required in production
	if s.EncryptionKey == "" {
		errs = append(errs, fmt.Errorf("%s is required for production", EnvEncryptionKey))
	}

	// UI credentials are required in production
	if s.UIUsername == "" || s.UIPassword == "" {
		errs = append(errs, fmt.Errorf("%s and %s are required for production", EnvUIUsername, EnvUIPassword))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// HasUICredentials returns true if both UI username and password are configured.
func (s *Secrets) HasUICredentials() bool {
	return s.UIUsername != "" && s.UIPassword != ""
}

// String returns a redacted string representation safe for logging.
func (s *Secrets) String() string {
	return fmt.Sprintf("Secrets{DatabaseURL: %s, MasterKey: %s, EncryptionKey: %s, UIUsername: %s}",
		redact(s.DatabaseURL),
		redact(s.MasterKey),
		redact(s.EncryptionKey),
		redact(s.UIUsername),
	)
}

// redact returns a redacted version of the string for logging.
func redact(s string) string {
	if s == "" {
		return "[not set]"
	}
	return "[redacted]"
}

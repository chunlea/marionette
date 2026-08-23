package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				Storage: StorageConfig{
					Provider: "local",
					Local:    &LocalStorageConfig{Path: "./data"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid api port",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 0},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				Storage: StorageConfig{
					Provider: "local",
					Local:    &LocalStorageConfig{Path: "./data"},
				},
			},
			wantErr:     true,
			errContains: "api port must be between",
		},
		{
			name: "invalid log level",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "trace", Format: "json"},
				Storage: StorageConfig{
					Provider: "local",
					Local:    &LocalStorageConfig{Path: "./data"},
				},
			},
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name: "invalid log format",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "info", Format: "xml"},
				Storage: StorageConfig{
					Provider: "local",
					Local:    &LocalStorageConfig{Path: "./data"},
				},
			},
			wantErr:     true,
			errContains: "invalid log format",
		},
		{
			name: "invalid storage provider",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				Storage: StorageConfig{Provider: "gcs"},
			},
			wantErr:     true,
			errContains: "invalid storage provider",
		},
		{
			name: "missing local storage path",
			config: &Config{
				Server: ServerConfig{
					API:   EndpointConfig{Host: "0.0.0.0", Port: 8080},
					Admin: EndpointConfig{Host: "127.0.0.1", Port: 8081},
					GRPC:  EndpointConfig{Host: "0.0.0.0", Port: 9090},
				},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				Storage: StorageConfig{Provider: "local"},
			},
			wantErr:     true,
			errContains: "local storage requires",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got: %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestTLSConfigValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *TLSConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "disabled",
			config:  &TLSConfig{Enabled: false},
			wantErr: false,
		},
		{
			name: "enabled with all files",
			config: &TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
				KeyFile:  "/path/to/key.pem",
			},
			wantErr: false,
		},
		{
			name: "enabled with mTLS",
			config: &TLSConfig{
				Enabled:      true,
				CertFile:     "/path/to/cert.pem",
				KeyFile:      "/path/to/key.pem",
				CAFile:       "/path/to/ca.pem",
				VerifyClient: true,
			},
			wantErr: false,
		},
		{
			name: "missing cert file",
			config: &TLSConfig{
				Enabled: true,
				KeyFile: "/path/to/key.pem",
			},
			wantErr:     true,
			errContains: "cert_file is required",
		},
		{
			name: "missing key file",
			config: &TLSConfig{
				Enabled:  true,
				CertFile: "/path/to/cert.pem",
			},
			wantErr:     true,
			errContains: "key_file is required",
		},
		{
			name: "mTLS without CA file",
			config: &TLSConfig{
				Enabled:      true,
				CertFile:     "/path/to/cert.pem",
				KeyFile:      "/path/to/key.pem",
				VerifyClient: true,
			},
			wantErr:     true,
			errContains: "ca_file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got: %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestS3StorageValidate(t *testing.T) {
	tests := []struct {
		name        string
		config      *StorageConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid s3 config",
			config: &StorageConfig{
				Provider: "s3",
				S3:       &S3StorageConfig{Bucket: "my-bucket", Region: "us-west-2"},
			},
			wantErr: false,
		},
		{
			name: "missing s3 config",
			config: &StorageConfig{
				Provider: "s3",
			},
			wantErr:     true,
			errContains: "s3 storage requires",
		},
		{
			name: "missing bucket",
			config: &StorageConfig{
				Provider: "s3",
				S3:       &S3StorageConfig{Region: "us-west-2"},
			},
			wantErr:     true,
			errContains: "bucket to be set",
		},
		{
			name: "missing region",
			config: &StorageConfig{
				Provider: "s3",
				S3:       &S3StorageConfig{Bucket: "my-bucket"},
			},
			wantErr:     true,
			errContains: "region to be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got: %v", tt.errContains, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    int
		wantErr bool
	}{
		{name: "port 1 valid", port: 1, wantErr: false},
		{name: "port 80 valid", port: 80, wantErr: false},
		{name: "port 8080 valid", port: 8080, wantErr: false},
		{name: "port 65535 valid", port: 65535, wantErr: false},
		{name: "port 0 invalid", port: 0, wantErr: true},
		{name: "port -1 invalid", port: -1, wantErr: true},
		{name: "port 65536 invalid", port: 65536, wantErr: true},
		{name: "port 100000 invalid", port: 100000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePort(tt.port, "test")
			if tt.wantErr && err == nil {
				t.Errorf("validatePort(%d) expected error, got nil", tt.port)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validatePort(%d) unexpected error: %v", tt.port, err)
			}
		})
	}
}

// The two retentions delete the same logs from different places. Configured the
// wrong way round they do not conflict, they just delete them.
func TestStorageLogArchiveRetentionOrdering(t *testing.T) {
	base := StorageLogArchiveConfig{
		Enabled:       true,
		Interval:      time.Hour,
		RetentionDays: 30,
	}

	t.Run("archive must outlast the partitions", func(t *testing.T) {
		c := base
		c.Retention = 10 * 24 * time.Hour
		err := c.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must outlast")
	})

	t.Run("longer archive retention is fine", func(t *testing.T) {
		c := base
		c.Retention = 90 * 24 * time.Hour
		assert.NoError(t, c.Validate())
	})

	t.Run("keeping archives forever is fine", func(t *testing.T) {
		c := base
		c.Retention = 0
		assert.NoError(t, c.Validate())
	})

	t.Run("nothing is checked while archiving is off", func(t *testing.T) {
		c := base
		c.Enabled = false
		c.Retention = time.Hour
		assert.NoError(t, c.Validate())
	})

	t.Run("negative values are rejected", func(t *testing.T) {
		c := base
		c.RetentionDays = -1
		assert.Error(t, c.Validate())

		c = base
		c.Retention = -time.Hour
		assert.Error(t, c.Validate())

		c = base
		c.Interval = -time.Hour
		assert.Error(t, c.Validate())
	})
}

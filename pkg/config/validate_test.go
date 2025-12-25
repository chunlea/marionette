package config

import (
	"strings"
	"testing"
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
		port    int
		wantErr bool
	}{
		{port: 1, wantErr: false},
		{port: 80, wantErr: false},
		{port: 8080, wantErr: false},
		{port: 65535, wantErr: false},
		{port: 0, wantErr: true},
		{port: -1, wantErr: true},
		{port: 65536, wantErr: true},
		{port: 100000, wantErr: true},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
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

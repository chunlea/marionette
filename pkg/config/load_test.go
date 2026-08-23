package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	testdataDir := "testdata"

	tests := []struct {
		name        string
		configFile  string
		wantErr     bool
		errContains string
	}{
		{
			name:       "valid config",
			configFile: filepath.Join(testdataDir, "valid.yaml"),
			wantErr:    false,
		},
		{
			name:       "minimal config",
			configFile: filepath.Join(testdataDir, "minimal.yaml"),
			wantErr:    false,
		},
		{
			name:        "invalid port",
			configFile:  filepath.Join(testdataDir, "invalid_port.yaml"),
			wantErr:     true,
			errContains: "port must be between 1 and 65535",
		},
		{
			name:        "malformed yaml",
			configFile:  filepath.Join(testdataDir, "malformed.yaml"),
			wantErr:     true,
			errContains: "reading config file",
		},
		{
			name:        "missing file",
			configFile:  filepath.Join(testdataDir, "nonexistent.yaml"),
			wantErr:     true,
			errContains: "reading config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(tt.configFile)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Load() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}
			if cfg == nil {
				t.Error("Load() returned nil config")
			}
		})
	}
}

func TestLoadValidConfig(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Check server config
	if cfg.Server.API.Port != 8080 {
		t.Errorf("API port = %d, want 8080", cfg.Server.API.Port)
	}
	if cfg.Server.API.Host != "0.0.0.0" {
		t.Errorf("API host = %s, want 0.0.0.0", cfg.Server.API.Host)
	}
	if cfg.Server.Admin.Port != 8081 {
		t.Errorf("Admin port = %d, want 8081", cfg.Server.Admin.Port)
	}
	if cfg.Server.GRPC.Port != 9090 {
		t.Errorf("gRPC port = %d, want 9090", cfg.Server.GRPC.Port)
	}

	// Check logging config
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging level = %s, want debug", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "console" {
		t.Errorf("Logging format = %s, want console", cfg.Logging.Format)
	}

	// Check storage config
	if cfg.Storage.Provider != "local" {
		t.Errorf("Storage provider = %s, want local", cfg.Storage.Provider)
	}
	if cfg.Storage.Local.Path != "./data/storage" {
		t.Errorf("Storage path = %s, want ./data/storage", cfg.Storage.Local.Path)
	}

	// Check development config
	if !cfg.Development.HotReload {
		t.Error("Development.HotReload = false, want true")
	}
	if !cfg.Development.SkipTLS {
		t.Error("Development.SkipTLS = false, want true")
	}
}

func TestLoadWithEnvOverride(t *testing.T) {
	// Set environment variable
	t.Setenv("MARIONETTE_SERVER_API_PORT", "9999")

	cfg, err := Load(filepath.Join("testdata", "valid.yaml"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.API.Port != 9999 {
		t.Errorf("API port = %d, want 9999 (env override)", cfg.Server.API.Port)
	}
}

func TestLoadWithDefaults(t *testing.T) {
	cfg, err := LoadWithDefaults()
	if err != nil {
		t.Fatalf("LoadWithDefaults() error: %v", err)
	}

	// Check that defaults are set
	if cfg.Server.API.Port != 8080 {
		t.Errorf("API port = %d, want 8080 (default)", cfg.Server.API.Port)
	}
	if cfg.Server.API.Host != "0.0.0.0" {
		t.Errorf("API host = %s, want 0.0.0.0 (default)", cfg.Server.API.Host)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging level = %s, want info (default)", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging format = %s, want json (default)", cfg.Logging.Format)
	}
}

func TestLoad_StreamingAndTunnelDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "minimal.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Streaming is frozen: it must stay off unless explicitly enabled.
	if cfg.Streaming.Enabled {
		t.Error("streaming.enabled must default to false")
	}

	// Tunnels are a live product path.
	if !cfg.Tunnels.Enabled {
		t.Error("tunnels.enabled must default to true")
	}
}

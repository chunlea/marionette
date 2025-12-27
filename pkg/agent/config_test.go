package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: false,
		},
		{
			name: "valid config with runner-creates-sandbox",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-creates-sandbox"},
				Logging: LoggingConfig{Level: "debug", Format: "console"},
			},
			wantErr: false,
		},
		{
			name: "missing server address",
			cfg: Config{
				Server:  ServerConfig{Address: ""},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
			errMsg:  "server.address is required",
		},
		{
			name: "missing token",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: ""},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
			errMsg:  "runner.token is required",
		},
		{
			name: "invalid sandbox mode",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "invalid"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
			},
			wantErr: true,
			errMsg:  "sandbox.mode must be",
		},
		{
			name: "invalid log level",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "invalid", Format: "json"},
			},
			wantErr: true,
			errMsg:  "logging.level must be",
		},
		{
			name: "invalid log format",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "invalid"},
			},
			wantErr: true,
			errMsg:  "logging.format must be",
		},
		{
			name: "TLS cert without key",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				TLS:     TLSConfig{Enabled: true, CertFile: "/path/cert.pem"},
			},
			wantErr: true,
			errMsg:  "tls.key_file is required",
		},
		{
			name: "TLS key without cert",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				TLS:     TLSConfig{Enabled: true, KeyFile: "/path/key.pem"},
			},
			wantErr: true,
			errMsg:  "tls.cert_file is required",
		},
		{
			name: "TLS with both cert and key is valid",
			cfg: Config{
				Server:  ServerConfig{Address: "localhost:9090"},
				Runner:  RunnerConfig{Token: "rtok_test"},
				Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
				Logging: LoggingConfig{Level: "info", Format: "json"},
				TLS:     TLSConfig{Enabled: true, CertFile: "/path/cert.pem", KeyFile: "/path/key.pem"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.ErrorIs(t, err, ErrInvalidConfig)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoad_FromFile(t *testing.T) {
	// Create a temporary config file
	configContent := `
server:
  address: "test-server:9090"
runner:
  name: "test-runner"
  pool_name: "test-pool"
  labels:
    env: test
heartbeat:
  interval: "15s"
  timeout: "5s"
sandbox:
  mode: "runner-creates-sandbox"
  types:
    - docker
    - gvisor
logging:
  level: "debug"
  format: "console"
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agent.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	// Set required token via environment
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test_token")

	cfg, err := Load(configPath)
	require.NoError(t, err)

	assert.Equal(t, "test-server:9090", cfg.Server.Address)
	assert.Equal(t, "rtok_test_token", cfg.Runner.Token)
	assert.Equal(t, "test-runner", cfg.Runner.Name)
	assert.Equal(t, "test-pool", cfg.Runner.PoolName)
	assert.Equal(t, map[string]string{"env": "test"}, cfg.Runner.Labels)
	assert.Equal(t, 15*time.Second, cfg.Heartbeat.Interval)
	assert.Equal(t, 5*time.Second, cfg.Heartbeat.Timeout)
	assert.Equal(t, "runner-creates-sandbox", cfg.Sandbox.Mode)
	assert.Equal(t, []string{"docker", "gvisor"}, cfg.Sandbox.Types)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "console", cfg.Logging.Format)
}

func TestLoad_FromEnvironment(t *testing.T) {
	t.Setenv("MARIONETTE_SERVER_ADDRESS", "env-server:9090")
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_env_token")
	t.Setenv("MARIONETTE_RUNNER_NAME", "env-runner")
	t.Setenv("MARIONETTE_RUNNER_POOL_NAME", "env-pool")
	t.Setenv("MARIONETTE_SANDBOX_MODE", "runner-creates-sandbox")
	t.Setenv("MARIONETTE_LOGGING_LEVEL", "warn")

	cfg, err := LoadWithDefaults()
	require.NoError(t, err)

	assert.Equal(t, "env-server:9090", cfg.Server.Address)
	assert.Equal(t, "rtok_env_token", cfg.Runner.Token)
	assert.Equal(t, "env-runner", cfg.Runner.Name)
	assert.Equal(t, "env-pool", cfg.Runner.PoolName)
	assert.Equal(t, "runner-creates-sandbox", cfg.Sandbox.Mode)
	assert.Equal(t, "warn", cfg.Logging.Level)
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test")

	cfg, err := LoadWithDefaults()
	require.NoError(t, err)

	assert.Equal(t, "localhost:9090", cfg.Server.Address)
	assert.Equal(t, 30*time.Second, cfg.Heartbeat.Interval)
	assert.Equal(t, 10*time.Second, cfg.Heartbeat.Timeout)
	assert.Equal(t, "runner-is-sandbox", cfg.Sandbox.Mode)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
	assert.False(t, cfg.TLS.Enabled)
}

func TestLoad_HostnameDefault(t *testing.T) {
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test")

	cfg, err := LoadWithDefaults()
	require.NoError(t, err)

	// Runner name should default to hostname
	hostname, _ := os.Hostname()
	assert.Equal(t, hostname, cfg.Runner.Name)
}

func TestLoad_MissingToken(t *testing.T) {
	// Clear environment
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "")

	_, err := LoadWithDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.token is required")
}

func TestLoad_InvalidConfigFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestLoad_MalformedConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad.yaml")
	err := os.WriteFile(configPath, []byte("{{invalid yaml"), 0600)
	require.NoError(t, err)

	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test")

	_, err = Load(configPath)
	require.Error(t, err)
}

func TestLoad_EnvironmentOverridesFile(t *testing.T) {
	// Create config file with one value
	configContent := `
server:
  address: "file-server:9090"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "agent.yaml")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	require.NoError(t, err)

	// Set environment to override
	t.Setenv("MARIONETTE_SERVER_ADDRESS", "env-server:9090")
	t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test")

	cfg, err := Load(configPath)
	require.NoError(t, err)

	// Environment should override file
	assert.Equal(t, "env-server:9090", cfg.Server.Address)
}

func TestBindFlags(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	BindFlags(flags)

	// Verify all flags are registered
	assert.NotNil(t, flags.Lookup("server"))
	assert.NotNil(t, flags.Lookup("token"))
	assert.NotNil(t, flags.Lookup("name"))
	assert.NotNil(t, flags.Lookup("pool"))
	assert.NotNil(t, flags.Lookup("sandbox-mode"))
	assert.NotNil(t, flags.Lookup("log-level"))
	assert.NotNil(t, flags.Lookup("log-format"))
	assert.NotNil(t, flags.Lookup("tls"))
	assert.NotNil(t, flags.Lookup("tls-skip-verify"))
	assert.NotNil(t, flags.Lookup("tls-cert"))
	assert.NotNil(t, flags.Lookup("tls-key"))
	assert.NotNil(t, flags.Lookup("tls-ca"))

	// Check default values
	serverFlag := flags.Lookup("server")
	assert.Equal(t, "localhost:9090", serverFlag.DefValue)

	sandboxFlag := flags.Lookup("sandbox-mode")
	assert.Equal(t, "runner-is-sandbox", sandboxFlag.DefValue)

	logLevelFlag := flags.Lookup("log-level")
	assert.Equal(t, "info", logLevelFlag.DefValue)

	logFormatFlag := flags.Lookup("log-format")
	assert.Equal(t, "json", logFormatFlag.DefValue)
}

func TestLoadWithFlags(t *testing.T) {
	t.Run("flags override defaults", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		BindFlags(flags)

		// Parse command-line flags
		err := flags.Parse([]string{
			"--server", "flag-server:9090",
			"--token", "rtok_flag_token",
			"--log-level", "debug",
			"--log-format", "console",
			"--sandbox-mode", "runner-creates-sandbox",
		})
		require.NoError(t, err)

		cfg, err := LoadWithFlags("", flags)
		require.NoError(t, err)

		assert.Equal(t, "flag-server:9090", cfg.Server.Address)
		assert.Equal(t, "rtok_flag_token", cfg.Runner.Token)
		assert.Equal(t, "debug", cfg.Logging.Level)
		assert.Equal(t, "console", cfg.Logging.Format)
		assert.Equal(t, "runner-creates-sandbox", cfg.Sandbox.Mode)
	})

	t.Run("flags override environment", func(t *testing.T) {
		// Set environment variables
		t.Setenv("MARIONETTE_SERVER_ADDRESS", "env-server:9090")
		t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_env_token")
		t.Setenv("MARIONETTE_LOGGING_LEVEL", "warn")

		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		BindFlags(flags)

		// Parse command-line flags that should override env vars
		err := flags.Parse([]string{
			"--log-level", "debug",
		})
		require.NoError(t, err)

		cfg, err := LoadWithFlags("", flags)
		require.NoError(t, err)

		// Flag should override environment
		assert.Equal(t, "debug", cfg.Logging.Level)
		// Env vars should still apply where flags not set
		assert.Equal(t, "env-server:9090", cfg.Server.Address)
		assert.Equal(t, "rtok_env_token", cfg.Runner.Token)
	})

	t.Run("flags override config file", func(t *testing.T) {
		// Create config file
		configContent := `
server:
  address: "file-server:9090"
logging:
  level: "error"
  format: "json"
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "agent.yaml")
		err := os.WriteFile(configPath, []byte(configContent), 0600)
		require.NoError(t, err)

		t.Setenv("MARIONETTE_RUNNER_TOKEN", "rtok_test")

		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		BindFlags(flags)

		// Parse command-line flags
		err = flags.Parse([]string{
			"--log-level", "debug",
		})
		require.NoError(t, err)

		cfg, err := LoadWithFlags(configPath, flags)
		require.NoError(t, err)

		// Flag should override file config
		assert.Equal(t, "debug", cfg.Logging.Level)
		// File config should apply where flags not set
		assert.Equal(t, "file-server:9090", cfg.Server.Address)
		assert.Equal(t, "json", cfg.Logging.Format)
	})

	t.Run("all logging flags work", func(t *testing.T) {
		flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
		BindFlags(flags)

		err := flags.Parse([]string{
			"--token", "rtok_test",
			"--log-level", "warn",
			"--log-format", "console",
		})
		require.NoError(t, err)

		cfg, err := LoadWithFlags("", flags)
		require.NoError(t, err)

		assert.Equal(t, "warn", cfg.Logging.Level)
		assert.Equal(t, "console", cfg.Logging.Format)
	})
}

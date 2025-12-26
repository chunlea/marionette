package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigSetContext(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Set flags for set-context
	setContextFlags.server = "http://localhost:8080"
	setContextFlags.apiKey = "mk_test123"
	defer func() {
		setContextFlags.server = ""
		setContextFlags.apiKey = ""
	}()

	// Execute set-context command
	rootCmd.SetArgs([]string{"config", "set-context", "local"})
	err := rootCmd.Execute()
	require.NoError(t, err)

	// Verify config file was created
	data, err := os.ReadFile(configPath) //nolint:gosec // Test file path
	require.NoError(t, err)

	var cfg CLIConfig
	err = yaml.Unmarshal(data, &cfg)
	require.NoError(t, err)

	assert.Equal(t, "http://localhost:8080", cfg.Contexts["local"].Server)
	assert.Equal(t, "mk_test123", cfg.Contexts["local"].APIKey)
}

func TestConfigUseContext(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create initial config with a context
	initialCfg := CLIConfig{
		Contexts: map[string]Context{
			"production": {
				Server: "https://prod.example.com",
				APIKey: "mk_prod",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute use-context command
	rootCmd.SetArgs([]string{"config", "use-context", "production"})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify current-context was set
	data, err = os.ReadFile(configPath) //nolint:gosec // Test file path
	require.NoError(t, err)

	var cfg CLIConfig
	err = yaml.Unmarshal(data, &cfg)
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.CurrentContext)
}

func TestConfigUseContextNotFound(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create empty config
	err := os.WriteFile(configPath, []byte("{}"), 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute use-context command with non-existent context
	rootCmd.SetArgs([]string{"config", "use-context", "nonexistent"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestConfigDeleteContext(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create initial config with contexts
	initialCfg := CLIConfig{
		CurrentContext: "local",
		Contexts: map[string]Context{
			"local": {
				Server: "http://localhost:8080",
			},
			"production": {
				Server: "https://prod.example.com",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute delete-context command
	rootCmd.SetArgs([]string{"config", "delete-context", "local"})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify context was deleted and current-context was cleared
	data, err = os.ReadFile(configPath) //nolint:gosec // Test file path
	require.NoError(t, err)

	var cfg CLIConfig
	err = yaml.Unmarshal(data, &cfg)
	require.NoError(t, err)

	assert.Empty(t, cfg.CurrentContext)
	assert.NotContains(t, cfg.Contexts, "local")
	assert.Contains(t, cfg.Contexts, "production")
}

func TestConfigGetContexts(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with contexts
	initialCfg := CLIConfig{
		CurrentContext: "production",
		Contexts: map[string]Context{
			"local": {
				Server: "http://localhost:8080",
			},
			"production": {
				Server: "https://prod.example.com",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute get-contexts command
	rootCmd.SetArgs([]string{"config", "get-contexts"})
	err = rootCmd.Execute()
	require.NoError(t, err)
}

func TestConfigView(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config
	initialCfg := CLIConfig{
		CurrentContext: "local",
		Contexts: map[string]Context{
			"local": {
				Server: "http://localhost:8080",
				APIKey: "mk_test",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute view command
	rootCmd.SetArgs([]string{"config", "view"})
	err = rootCmd.Execute()
	require.NoError(t, err)
}

func TestGetEffectiveConfig(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with contexts
	initialCfg := CLIConfig{
		CurrentContext: "production",
		Contexts: map[string]Context{
			"local": {
				Server: "http://localhost:8080",
				APIKey: "mk_local",
			},
			"production": {
				Server: "https://prod.example.com",
				APIKey: "mk_prod",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Test using current context
	cfg, err := GetEffectiveConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://prod.example.com", cfg.Server)
	assert.Equal(t, "mk_prod", cfg.APIKey)

	// Test with context name override
	contextName = "local"
	defer func() { contextName = "" }()

	cfg, err = GetEffectiveConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", cfg.Server)
	assert.Equal(t, "mk_local", cfg.APIKey)

	// Test with command-line flag override
	serverURL = "http://override:9000"
	apiKey = "mk_override"
	defer func() {
		serverURL = ""
		apiKey = ""
	}()

	cfg, err = GetEffectiveConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://override:9000", cfg.Server)
	assert.Equal(t, "mk_override", cfg.APIKey)
}

func TestLoadCLIConfigMissingFile(t *testing.T) {
	// Set config path to non-existent file
	oldCfgFile := cfgFile
	cfgFile = "/nonexistent/path/config.yaml"
	defer func() { cfgFile = oldCfgFile }()

	cfg, err := loadCLIConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Empty(t, cfg.Contexts)
}

func TestGetConfigPath(t *testing.T) {
	t.Run("with explicit cfgFile set", func(t *testing.T) {
		oldCfgFile := cfgFile
		cfgFile = "/custom/path/config.yaml"
		defer func() { cfgFile = oldCfgFile }()

		path := getConfigPath()
		assert.Equal(t, "/custom/path/config.yaml", path)
	})

	t.Run("with XDG_CONFIG_HOME set", func(t *testing.T) {
		oldCfgFile := cfgFile
		cfgFile = ""
		defer func() { cfgFile = oldCfgFile }()

		oldXDG := os.Getenv("XDG_CONFIG_HOME")
		err := os.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
		require.NoError(t, err)
		defer func() {
			if oldXDG != "" {
				_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
			} else {
				_ = os.Unsetenv("XDG_CONFIG_HOME")
			}
		}()

		path := getConfigPath()
		assert.Equal(t, "/custom/xdg/marionette/config.yaml", path)
	})

	t.Run("with default config path", func(t *testing.T) {
		oldCfgFile := cfgFile
		cfgFile = ""
		defer func() { cfgFile = oldCfgFile }()

		oldXDG := os.Getenv("XDG_CONFIG_HOME")
		err := os.Unsetenv("XDG_CONFIG_HOME")
		require.NoError(t, err)
		defer func() {
			if oldXDG != "" {
				_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
			}
		}()

		path := getConfigPath()
		// Should use ~/.config/marionette/config.yaml
		homeDir, _ := os.UserHomeDir()
		expectedPath := filepath.Join(homeDir, ".config", "marionette", "config.yaml")
		assert.Equal(t, expectedPath, path)
	})
}

func TestLoadCLIConfigInvalidYAML(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML
	err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	_, err = loadCLIConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse config file")
}

func TestSaveCLIConfig(t *testing.T) {
	t.Run("save to new directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "subdir", "config.yaml")

		oldCfgFile := cfgFile
		cfgFile = configPath
		defer func() { cfgFile = oldCfgFile }()

		cfg := &CLIConfig{
			CurrentContext: "test",
			Contexts: map[string]Context{
				"test": {Server: "http://localhost:8080"},
			},
		}

		err := saveCLIConfig(cfg)
		require.NoError(t, err)

		// Verify file was created
		data, err := os.ReadFile(configPath) //nolint:gosec // Test file path
		require.NoError(t, err)

		var loaded CLIConfig
		err = yaml.Unmarshal(data, &loaded)
		require.NoError(t, err)
		assert.Equal(t, "test", loaded.CurrentContext)
	})

	t.Run("save with empty config path", func(t *testing.T) {
		oldCfgFile := cfgFile
		cfgFile = ""
		defer func() { cfgFile = oldCfgFile }()

		// Unset XDG_CONFIG_HOME and HOME to simulate failure
		oldXDG := os.Getenv("XDG_CONFIG_HOME")
		oldHome := os.Getenv("HOME")
		err := os.Unsetenv("XDG_CONFIG_HOME")
		require.NoError(t, err)
		err = os.Unsetenv("HOME")
		require.NoError(t, err)
		defer func() {
			if oldXDG != "" {
				_ = os.Setenv("XDG_CONFIG_HOME", oldXDG)
			}
			if oldHome != "" {
				_ = os.Setenv("HOME", oldHome)
			}
		}()

		cfg := &CLIConfig{}
		err = saveCLIConfig(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not determine config path")
	})
}

func TestConfigDeleteContextNotFound(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config without contexts
	err := os.WriteFile(configPath, []byte("{}"), 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute delete-context command with non-existent context
	rootCmd.SetArgs([]string{"config", "delete-context", "nonexistent"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestConfigGetContextsEmpty(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create empty config
	err := os.WriteFile(configPath, []byte("{}"), 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Execute get-contexts command
	rootCmd.SetArgs([]string{"config", "get-contexts"})
	err = rootCmd.Execute()
	require.NoError(t, err)
}

func TestGetEffectiveConfigWithEnvVars(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with contexts
	initialCfg := CLIConfig{
		CurrentContext: "production",
		Contexts: map[string]Context{
			"production": {
				Server: "https://prod.example.com",
				APIKey: "mk_prod",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Save and restore environment variables
	oldEnvURL := os.Getenv("MARIONETTE_API_URL")
	oldEnvKey := os.Getenv("MARIONETTE_API_KEY")
	defer func() {
		if oldEnvURL != "" {
			_ = os.Setenv("MARIONETTE_API_URL", oldEnvURL)
		} else {
			_ = os.Unsetenv("MARIONETTE_API_URL")
		}
		if oldEnvKey != "" {
			_ = os.Setenv("MARIONETTE_API_KEY", oldEnvKey)
		} else {
			_ = os.Unsetenv("MARIONETTE_API_KEY")
		}
	}()

	// Set environment variables
	err = os.Setenv("MARIONETTE_API_URL", "http://env-url:8080")
	require.NoError(t, err)
	err = os.Setenv("MARIONETTE_API_KEY", "mk_env")
	require.NoError(t, err)

	cfg, err := GetEffectiveConfig()
	require.NoError(t, err)
	assert.Equal(t, "http://env-url:8080", cfg.Server)
	assert.Equal(t, "mk_env", cfg.APIKey)
}

func TestGetEffectiveConfigWithEnvContext(t *testing.T) {
	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with contexts
	initialCfg := CLIConfig{
		CurrentContext: "production",
		Contexts: map[string]Context{
			"staging": {
				Server: "https://staging.example.com",
				APIKey: "mk_staging",
			},
			"production": {
				Server: "https://prod.example.com",
				APIKey: "mk_prod",
			},
		},
	}
	data, err := yaml.Marshal(initialCfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, data, 0o600)
	require.NoError(t, err)

	// Set config path
	oldCfgFile := cfgFile
	cfgFile = configPath
	defer func() { cfgFile = oldCfgFile }()

	// Reset context name flag
	oldContextName := contextName
	contextName = ""
	defer func() { contextName = oldContextName }()

	// Save and restore environment variables
	oldEnvContext := os.Getenv("MARIONETTE_CONTEXT")
	defer func() {
		if oldEnvContext != "" {
			_ = os.Setenv("MARIONETTE_CONTEXT", oldEnvContext)
		} else {
			_ = os.Unsetenv("MARIONETTE_CONTEXT")
		}
	}()

	// Set environment variable to select staging context
	err = os.Setenv("MARIONETTE_CONTEXT", "staging")
	require.NoError(t, err)

	cfg, err := GetEffectiveConfig()
	require.NoError(t, err)
	assert.Equal(t, "https://staging.example.com", cfg.Server)
	assert.Equal(t, "mk_staging", cfg.APIKey)
}

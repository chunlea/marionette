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

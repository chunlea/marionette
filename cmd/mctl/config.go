package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// CLIConfig represents the mctl configuration file.
type CLIConfig struct {
	CurrentContext string             `yaml:"current-context,omitempty"`
	Contexts       map[string]Context `yaml:"contexts,omitempty"`
}

// Context represents a named configuration context.
type Context struct {
	Server string `yaml:"server,omitempty"`
	APIKey string `yaml:"api-key,omitempty"`
}

// setContextFlags holds flags for the set-context command.
var setContextFlags struct {
	server string
	apiKey string
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
	Long:  `Manage CLI configuration including contexts for different Marionette servers.`,
}

func init() {
	configCmd.AddCommand(configSetContextCmd)
	configCmd.AddCommand(configUseContextCmd)
	configCmd.AddCommand(configViewCmd)
	configCmd.AddCommand(configDeleteContextCmd)
	configCmd.AddCommand(configGetContextsCmd)

	// Flags for set-context
	configSetContextCmd.Flags().StringVar(&setContextFlags.server, "server", "", "API server URL")
	configSetContextCmd.Flags().StringVar(&setContextFlags.apiKey, "api-key", "", "API key for authentication")
}

var configSetContextCmd = &cobra.Command{
	Use:   "set-context NAME",
	Short: "Set a context entry in the config file",
	Long: `Create or update a context entry with the specified server and API key.

Examples:
  # Create a new context
  mctl config set-context local --server http://localhost:8080 --api-key mk_xxx

  # Update an existing context
  mctl config set-context production --server https://marionette.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}

		if cfg.Contexts == nil {
			cfg.Contexts = make(map[string]Context)
		}

		ctx := cfg.Contexts[name]
		if setContextFlags.server != "" {
			ctx.Server = setContextFlags.server
		}
		if setContextFlags.apiKey != "" {
			ctx.APIKey = setContextFlags.apiKey
		}
		cfg.Contexts[name] = ctx

		if err := saveCLIConfig(cfg); err != nil {
			return err
		}

		printf("Context %q modified.\n", name)
		return nil
	},
}

var configUseContextCmd = &cobra.Command{
	Use:   "use-context NAME",
	Short: "Set the current-context in the config file",
	Long: `Set the current-context to the specified context name.

Example:
  mctl config use-context production`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}

		if cfg.Contexts == nil || cfg.Contexts[name].Server == "" {
			return fmt.Errorf("context %q not found", name)
		}

		cfg.CurrentContext = name

		if err := saveCLIConfig(cfg); err != nil {
			return err
		}

		printf("Switched to context %q.\n", name)
		return nil
	},
}

var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "Display merged config settings",
	Long:  `Display the current configuration including all contexts.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}

		data, err := yaml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		printf("%s", string(data))
		return nil
	},
}

var configDeleteContextCmd = &cobra.Command{
	Use:   "delete-context NAME",
	Short: "Delete a context from the config file",
	Long: `Delete the specified context from the config file.

Example:
  mctl config delete-context old-cluster`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]

		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}

		if cfg.Contexts == nil {
			return fmt.Errorf("context %q not found", name)
		}

		if _, exists := cfg.Contexts[name]; !exists {
			return fmt.Errorf("context %q not found", name)
		}

		delete(cfg.Contexts, name)

		// Clear current context if it was deleted
		if cfg.CurrentContext == name {
			cfg.CurrentContext = ""
		}

		if err := saveCLIConfig(cfg); err != nil {
			return err
		}

		printf("Deleted context %q.\n", name)
		return nil
	},
}

var configGetContextsCmd = &cobra.Command{
	Use:   "get-contexts",
	Short: "List all contexts",
	Long:  `Display a list of all available contexts.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		cfg, err := loadCLIConfig()
		if err != nil {
			return err
		}

		if len(cfg.Contexts) == 0 {
			printf("No contexts configured.\n")
			return nil
		}

		printf("%-10s %-20s %s\n", "CURRENT", "NAME", "SERVER")
		for name, ctx := range cfg.Contexts {
			current := ""
			if name == cfg.CurrentContext {
				current = "*"
			}
			printf("%-10s %-20s %s\n", current, name, ctx.Server)
		}

		return nil
	},
}

// getConfigPath returns the path to the config file.
func getConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}

	// Use XDG_CONFIG_HOME if set, otherwise ~/.config
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	return filepath.Join(configDir, "marionette", "config.yaml")
}

// loadCLIConfig loads the CLI configuration from the config file.
func loadCLIConfig() (*CLIConfig, error) {
	configPath := getConfigPath()
	if configPath == "" {
		return &CLIConfig{}, nil
	}

	data, err := os.ReadFile(configPath) //nolint:gosec // Config path is user-controlled
	if err != nil {
		if os.IsNotExist(err) {
			return &CLIConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg CLIConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// saveCLIConfig saves the CLI configuration to the config file.
func saveCLIConfig(cfg *CLIConfig) error {
	configPath := getConfigPath()
	if configPath == "" {
		return fmt.Errorf("could not determine config path")
	}

	// Create directory if it doesn't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetEffectiveConfig returns the effective configuration considering
// environment variables and command-line flags.
// Exported for use in other command files.
func GetEffectiveConfig() (*Context, error) {
	cfg, err := loadCLIConfig()
	if err != nil {
		return nil, err
	}

	// Start with the current context from config
	var ctx Context
	ctxName := contextName
	if ctxName == "" {
		ctxName = os.Getenv("MARIONETTE_CONTEXT")
	}
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}
	if ctxName != "" && cfg.Contexts != nil {
		ctx = cfg.Contexts[ctxName]
	}

	// Override with environment variables
	if envURL := os.Getenv("MARIONETTE_API_URL"); envURL != "" {
		ctx.Server = envURL
	}
	if envKey := os.Getenv("MARIONETTE_API_KEY"); envKey != "" {
		ctx.APIKey = envKey
	}

	// Override with command-line flags (highest priority)
	if serverURL != "" {
		ctx.Server = serverURL
	}
	if apiKey != "" {
		ctx.APIKey = apiKey
	}

	return &ctx, nil
}

// Package main provides the mctl CLI binary.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Global flags
var (
	cfgFile     string
	serverURL   string
	apiKey      string
	outputFmt   string
	contextName string
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "mctl",
	Short: "Marionette CLI",
	Long: `mctl is the command line interface for managing Marionette sessions, tasks, and runners.

Use mctl to create and manage AI coding agent sessions, execute tasks,
and monitor their progress.`,
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.config/marionette/config.yaml)")
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server", "s", "", "API server URL")
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "API key for authentication")
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "output format (table, json, yaml)")
	rootCmd.PersistentFlags().StringVar(&contextName, "context", "", "context to use from config file")

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(tasksCmd)
}

// getOutput returns the output writer for commands (allows testing).
func getOutput() io.Writer {
	return os.Stdout
}

// printf is a helper to print to stdout.
func printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(getOutput(), format, args...)
}

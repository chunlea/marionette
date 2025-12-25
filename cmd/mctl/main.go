// Package main provides the mctl CLI binary.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mctl",
	Short: "Marionette CLI",
	Long:  `mctl is the command line interface for managing Marionette sessions, tasks, and runners.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// TODO: Add config commands
	// TODO: Add session commands
	// TODO: Add task commands
	// TODO: Add runner commands
	// TODO: Add permission commands
	// TODO: Add admin commands

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("mctl version 0.0.1")
		},
	})
}

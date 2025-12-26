package main

import (
	"github.com/spf13/cobra"
)

// Version information (set via ldflags during build).
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Print the version, commit hash, and build date of mctl.`,
	Run: func(_ *cobra.Command, _ []string) {
		printf("mctl version %s\n", version)
		printf("  commit:     %s\n", commit)
		printf("  build date: %s\n", buildDate)
	},
}

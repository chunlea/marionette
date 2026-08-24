package main

import (
	"fmt"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// adminClient is the admin API client (separate from public API client).
var adminClient client.AdminClient

// adminClientWasSet tracks if adminClient was explicitly set (for testing).
var adminClientWasSet bool

// SetAdminClient sets the admin client (for testing).
func SetAdminClient(c client.AdminClient) {
	adminClient = c
	adminClientWasSet = true
}

// ResetAdminClient clears the admin client, so a test that installed one does
// not leave the credential check skipped for whatever runs next.
func ResetAdminClient() {
	adminClient = nil
	adminClientWasSet = false
}

// Admin flags
var (
	adminServer   string
	adminUsername string
	adminPassword string
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Admin operations",
	Long:  `Administrative operations for managing Marionette infrastructure.`,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// Skip if client was explicitly set (e.g., by tests)
		if adminClientWasSet {
			return nil
		}

		// Get admin server URL
		server := adminServer
		if server == "" {
			// Try to derive from context config
			ctx, err := GetEffectiveConfig()
			if err == nil && ctx.Server != "" {
				// Replace port 8080 with 8081 for admin API
				server = ctx.Server
				// Simple port replacement - assumes standard ports
				if len(server) > 5 && server[len(server)-5:] == ":8080" {
					server = server[:len(server)-5] + ":8081"
				}
			}
		}

		if server == "" {
			return fmt.Errorf("admin server URL is required (use --admin-server or configure context)")
		}

		username := adminUsername
		password := adminPassword

		if username == "" || password == "" {
			return fmt.Errorf("admin credentials required (use --admin-username and --admin-password)")
		}

		adminClient = client.NewHTTPAdminClient(server, username, password)
		return nil
	},
}

func init() {
	// Admin persistent flags
	adminCmd.PersistentFlags().StringVar(&adminServer, "admin-server", "", "Admin API server URL (default: derived from --server with port 8081)")
	adminCmd.PersistentFlags().StringVar(&adminUsername, "admin-username", "", "Admin username for basic auth")
	adminCmd.PersistentFlags().StringVar(&adminPassword, "admin-password", "", "Admin password for basic auth")

	// Add subcommands
	adminCmd.AddCommand(adminSessionsCmd)
}

// adminSessionsCmd is the parent command for admin session operations.
var adminSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Admin session operations",
	Long:  `Administrative operations for managing sessions.`,
}

func init() {
	adminSessionsCmd.AddCommand(adminSessionsActivateCmd)
	adminSessionsCmd.AddCommand(adminSessionsSuspendCmd)
}

var adminSessionsActivateCmd = &cobra.Command{
	Use:   "activate SESSION_ID RUNNER_ID",
	Short: "Activate a session by attaching a runner",
	Long: `Activate a pending session by attaching a runner to it.

This command is useful for testing when you need to manually attach
a runner to a session.

Examples:
  # Activate a session with a specific runner
  mctl admin sessions activate sess_xxx run_xxx --admin-username admin --admin-password secret`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]
		runnerID := args[1]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if err := adminClient.ActivateSession(ctx, sessionID, runnerID); err != nil {
			return fmt.Errorf("failed to activate session: %w", err)
		}

		printf("Session %s activated with runner %s.\n", sessionID, runnerID)
		return nil
	},
}

var adminSessionsSuspendCmd = &cobra.Command{
	Use:   "suspend SESSION_ID",
	Short: "Suspend a session",
	Long: `Suspend an active session.

Examples:
  # Suspend a session with default strategy
  mctl admin sessions suspend sess_xxx --admin-username admin --admin-password secret

  # Suspend with specific strategy
  mctl admin sessions suspend sess_xxx --strategy pause --admin-username admin --admin-password secret`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		strategy, _ := cmd.Flags().GetString("strategy")

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if err := adminClient.SuspendSession(ctx, sessionID, strategy); err != nil {
			return fmt.Errorf("failed to suspend session: %w", err)
		}

		printf("Session %s suspended.\n", sessionID)
		return nil
	},
}

func init() {
	adminSessionsSuspendCmd.Flags().String("strategy", "", "Suspend strategy (pause, snapshot, terminate_preserve_storage, release_to_pool, terminate)")
}

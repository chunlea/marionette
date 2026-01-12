package main

import (
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// adminRunnerTokensCmd is the parent command for admin runner token operations.
var adminRunnerTokensCmd = &cobra.Command{
	Use:   "runner-tokens",
	Short: "Manage runner tokens",
	Long:  `Administrative operations for managing runner tokens.`,
}

func init() {
	adminCmd.AddCommand(adminRunnerTokensCmd)
	adminRunnerTokensCmd.AddCommand(adminRunnerTokensCreateCmd)
	adminRunnerTokensCmd.AddCommand(adminRunnerTokensListCmd)
	adminRunnerTokensCmd.AddCommand(adminRunnerTokensGetCmd)
	adminRunnerTokensCmd.AddCommand(adminRunnerTokensRevokeCmd)
	adminRunnerTokensCmd.AddCommand(adminRunnerTokensRotateCmd)
}

// Create command flags
var (
	runnerTokenPoolName  string
	runnerTokenLabels    []string
	runnerTokenExpiresAt string
)

var adminRunnerTokensCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new runner token",
	Long: `Create a new runner token for a pool.

The token will be displayed once after creation. Make sure to save it securely.

Examples:
  # Create a token for the default pool
  mctl admin runner-tokens create --pool-name default --admin-username admin --admin-password secret

  # Create a token with labels and expiration
  mctl admin runner-tokens create --pool-name gpu-pool --label env=prod --label tier=premium --expires-at 2025-12-31T23:59:59Z`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if runnerTokenPoolName == "" {
			return fmt.Errorf("--pool-name is required")
		}

		opts := client.CreateRunnerTokenOptions{
			PoolName: runnerTokenPoolName,
			Labels:   parseLabels(runnerTokenLabels),
		}

		if runnerTokenExpiresAt != "" {
			t, err := time.Parse(time.RFC3339, runnerTokenExpiresAt)
			if err != nil {
				return fmt.Errorf("invalid expires-at format (use RFC3339): %w", err)
			}
			opts.ExpiresAt = &t
		}

		result, err := adminClient.CreateRunnerToken(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create runner token: %w", err)
		}

		// Print the result
		printf("Runner token created successfully!\n\n")
		printf("ID:           %s\n", result.ID)
		printf("Pool:         %s\n", result.PoolName)
		printf("Token Prefix: %s\n", result.TokenPrefix)
		printf("Status:       %s\n", result.Status)
		printf("Created:      %s\n", result.CreatedAt.Format(time.RFC3339))
		if result.ExpiresAt != nil {
			printf("Expires:      %s\n", result.ExpiresAt.Format(time.RFC3339))
		}
		printf("\n")
		printf("Raw Token (save this, it will not be shown again):\n")
		printf("  %s\n", result.RawToken)

		return nil
	},
}

func init() {
	adminRunnerTokensCreateCmd.Flags().StringVar(&runnerTokenPoolName, "pool-name", "", "Pool name for the token (required)")
	adminRunnerTokensCreateCmd.Flags().StringArrayVar(&runnerTokenLabels, "label", nil, "Labels in key=value format (can be repeated)")
	adminRunnerTokensCreateCmd.Flags().StringVar(&runnerTokenExpiresAt, "expires-at", "", "Expiration time in RFC3339 format")
	_ = adminRunnerTokensCreateCmd.MarkFlagRequired("pool-name")
}

// List command flags
var (
	listRunnerTokenPoolName       string
	listRunnerTokenStatus         []string
	listRunnerTokenIncludeRevoked bool
	listRunnerTokenLimit          int
)

var adminRunnerTokensListCmd = &cobra.Command{
	Use:   "list",
	Short: "List runner tokens",
	Long: `List runner tokens with optional filtering.

Examples:
  # List all tokens
  mctl admin runner-tokens list --admin-username admin --admin-password secret

  # List tokens for a specific pool
  mctl admin runner-tokens list --pool-name default

  # List tokens including revoked ones
  mctl admin runner-tokens list --include-revoked

  # List only active tokens
  mctl admin runner-tokens list --status active`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		opts := client.ListRunnerTokensOptions{
			PoolName:       listRunnerTokenPoolName,
			Status:         listRunnerTokenStatus,
			IncludeRevoked: listRunnerTokenIncludeRevoked,
			Limit:          listRunnerTokenLimit,
		}

		result, err := adminClient.ListRunnerTokens(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list runner tokens: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No runner tokens found.\n")
			return nil
		}

		// Print as table
		printf("%-24s %-15s %-15s %-10s %-20s %-20s\n", "ID", "POOL", "PREFIX", "STATUS", "LAST USED", "CREATED")
		printf("%-24s %-15s %-15s %-10s %-20s %-20s\n", "----", "----", "------", "------", "---------", "-------")

		for _, token := range result.Items {
			lastUsed := "-"
			if token.LastUsedAt != nil {
				lastUsed = token.LastUsedAt.Format("2006-01-02 15:04:05")
			}
			printf("%-24s %-15s %-15s %-10s %-20s %-20s\n",
				token.ID,
				truncate(token.PoolName, 15),
				token.TokenPrefix,
				token.Status,
				lastUsed,
				token.CreatedAt.Format("2006-01-02 15:04:05"),
			)
		}

		if result.NextCursor != "" {
			printf("\nMore results available. Use --cursor %s to fetch next page.\n", result.NextCursor)
		}

		return nil
	},
}

func init() {
	adminRunnerTokensListCmd.Flags().StringVar(&listRunnerTokenPoolName, "pool-name", "", "Filter by pool name")
	adminRunnerTokensListCmd.Flags().StringSliceVar(&listRunnerTokenStatus, "status", nil, "Filter by status (active, rotating, revoked, expired)")
	adminRunnerTokensListCmd.Flags().BoolVar(&listRunnerTokenIncludeRevoked, "include-revoked", false, "Include revoked tokens")
	adminRunnerTokensListCmd.Flags().IntVar(&listRunnerTokenLimit, "limit", 50, "Maximum number of tokens to return")
}

var adminRunnerTokensGetCmd = &cobra.Command{
	Use:   "get TOKEN_ID",
	Short: "Get a runner token by ID",
	Long: `Get detailed information about a runner token.

Examples:
  mctl admin runner-tokens get rtok_xxx --admin-username admin --admin-password secret`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		tokenID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		token, err := adminClient.GetRunnerToken(ctx, tokenID)
		if err != nil {
			return fmt.Errorf("failed to get runner token: %w", err)
		}

		printf("ID:           %s\n", token.ID)
		printf("Pool:         %s\n", token.PoolName)
		printf("Token Prefix: %s\n", token.TokenPrefix)
		printf("Status:       %s\n", token.Status)
		if token.RunnerID != nil {
			printf("Runner ID:    %s\n", *token.RunnerID)
		}
		printf("Created:      %s\n", token.CreatedAt.Format(time.RFC3339))
		if token.CreatedBy != nil {
			printf("Created By:   %s\n", *token.CreatedBy)
		}
		if token.LastUsedAt != nil {
			printf("Last Used:    %s\n", token.LastUsedAt.Format(time.RFC3339))
		}
		if token.ExpiresAt != nil {
			printf("Expires:      %s\n", token.ExpiresAt.Format(time.RFC3339))
		}
		if token.RevokedAt != nil {
			printf("Revoked:      %s\n", token.RevokedAt.Format(time.RFC3339))
			if token.RevokeReason != nil {
				printf("Revoke Reason: %s\n", *token.RevokeReason)
			}
		}
		if token.RotationDeadline != nil {
			printf("Rotation Deadline: %s\n", token.RotationDeadline.Format(time.RFC3339))
		}

		return nil
	},
}

// Revoke command flags
var revokeRunnerTokenReason string

var adminRunnerTokensRevokeCmd = &cobra.Command{
	Use:   "revoke TOKEN_ID",
	Short: "Revoke a runner token",
	Long: `Revoke a runner token. This action cannot be undone.

Examples:
  # Revoke a token
  mctl admin runner-tokens revoke rtok_xxx --admin-username admin --admin-password secret

  # Revoke with a reason
  mctl admin runner-tokens revoke rtok_xxx --reason "token compromised"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		tokenID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if err := adminClient.RevokeRunnerToken(ctx, tokenID, revokeRunnerTokenReason); err != nil {
			return fmt.Errorf("failed to revoke runner token: %w", err)
		}

		printf("Runner token %s revoked.\n", tokenID)
		return nil
	},
}

func init() {
	adminRunnerTokensRevokeCmd.Flags().StringVar(&revokeRunnerTokenReason, "reason", "", "Reason for revoking the token")
}

var adminRunnerTokensRotateCmd = &cobra.Command{
	Use:   "rotate TOKEN_ID",
	Short: "Rotate a runner token",
	Long: `Rotate a runner token. The old token remains valid during the rotation window.

The new token will be displayed once. Make sure to save it securely.

Examples:
  mctl admin runner-tokens rotate rtok_xxx --admin-username admin --admin-password secret`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		tokenID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		result, err := adminClient.RotateRunnerToken(ctx, tokenID)
		if err != nil {
			return fmt.Errorf("failed to rotate runner token: %w", err)
		}

		printf("Runner token rotated successfully!\n\n")
		printf("ID:           %s\n", result.ID)
		printf("Pool:         %s\n", result.PoolName)
		printf("Token Prefix: %s\n", result.TokenPrefix)
		printf("Status:       %s\n", result.Status)
		if result.RotationDeadline != nil {
			printf("Rotation Deadline: %s\n", result.RotationDeadline.Format(time.RFC3339))
			printf("(Old token valid until this time)\n")
		}
		printf("\n")
		printf("New Raw Token (save this, it will not be shown again):\n")
		printf("  %s\n", result.RawToken)

		return nil
	},
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

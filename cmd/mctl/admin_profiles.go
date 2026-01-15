package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// adminProfilesCmd is the parent command for admin profile operations.
var adminProfilesCmd = &cobra.Command{
	Use:   "profiles",
	Short: "Manage profiles",
	Long:  `Administrative operations for managing runner profiles.`,
}

func init() {
	adminCmd.AddCommand(adminProfilesCmd)
	adminProfilesCmd.AddCommand(adminProfilesCreateCmd)
	adminProfilesCmd.AddCommand(adminProfilesListCmd)
	adminProfilesCmd.AddCommand(adminProfilesGetCmd)
	adminProfilesCmd.AddCommand(adminProfilesUpdateCmd)
	adminProfilesCmd.AddCommand(adminProfilesDeleteCmd)
}

// Create command flags
var (
	profileName             string
	profileDescription      string
	profileProviderConfigID string
	profileResources        string
	profileNetwork          string
	profileInitScript       string
	profileCleanupScript    string
	profileTunnels          string
	profileSelector         string
	profileLabels           []string
	profileAnnotations      []string
)

var adminProfilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new profile",
	Long: `Create a new profile for configuring runner resources and behavior.

Examples:
  # Create a minimal profile
  mctl admin profiles create --name dev-small

  # Create a profile with resources
  mctl admin profiles create --name dev-medium \
    --description "Medium development environment" \
    --resources '{"cpu":"2","memory":"4Gi"}' \
    --label env=dev

  # Create a profile with network settings and tunnels
  mctl admin profiles create --name streaming \
    --network '{"policy":"allow_list","allowed_hosts":["api.openai.com"]}' \
    --tunnels '[{"type":"desktop","port":5900}]'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if profileName == "" {
			return fmt.Errorf("--name is required")
		}

		opts := client.CreateProfileOptions{
			Name:             profileName,
			Description:      profileDescription,
			ProviderConfigID: profileProviderConfigID,
			Labels:           parseLabels(profileLabels),
			Annotations:      parseLabels(profileAnnotations),
		}

		// Parse JSON fields if provided
		if profileResources != "" {
			var resources map[string]any
			if err := json.Unmarshal([]byte(profileResources), &resources); err != nil {
				return fmt.Errorf("invalid resources JSON: %w", err)
			}
			opts.Resources = resources
		}

		if profileNetwork != "" {
			var network map[string]any
			if err := json.Unmarshal([]byte(profileNetwork), &network); err != nil {
				return fmt.Errorf("invalid network JSON: %w", err)
			}
			opts.Network = network
		}

		if profileTunnels != "" {
			var tunnels []map[string]any
			if err := json.Unmarshal([]byte(profileTunnels), &tunnels); err != nil {
				return fmt.Errorf("invalid tunnels JSON: %w", err)
			}
			opts.Tunnels = tunnels
		}

		if profileSelector != "" {
			var selector map[string]any
			if err := json.Unmarshal([]byte(profileSelector), &selector); err != nil {
				return fmt.Errorf("invalid selector JSON: %w", err)
			}
			opts.Selector = selector
		}

		if profileInitScript != "" {
			opts.InitScript = profileInitScript
		}

		if profileCleanupScript != "" {
			opts.CleanupScript = profileCleanupScript
		}

		profile, err := adminClient.CreateProfile(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create profile: %w", err)
		}

		printf("Profile created successfully!\n\n")
		printf("ID:          %s\n", profile.ID)
		printf("Name:        %s\n", profile.Name)
		if profile.Description != nil {
			printf("Description: %s\n", *profile.Description)
		}
		if profile.ProviderConfigID != nil {
			printf("Provider:    %s\n", *profile.ProviderConfigID)
		}
		printf("Created:     %s\n", profile.CreatedAt.Format(time.RFC3339))

		return nil
	},
}

func init() {
	adminProfilesCreateCmd.Flags().StringVar(&profileName, "name", "", "Profile name (required)")
	adminProfilesCreateCmd.Flags().StringVar(&profileDescription, "description", "", "Profile description")
	adminProfilesCreateCmd.Flags().StringVar(&profileProviderConfigID, "provider-config-id", "", "Provider configuration ID")
	adminProfilesCreateCmd.Flags().StringVar(&profileResources, "resources", "", "Resources JSON (e.g., '{\"cpu\":\"2\",\"memory\":\"4Gi\"}')")
	adminProfilesCreateCmd.Flags().StringVar(&profileNetwork, "network", "", "Network configuration JSON")
	adminProfilesCreateCmd.Flags().StringVar(&profileInitScript, "init-script", "", "Initialization script")
	adminProfilesCreateCmd.Flags().StringVar(&profileCleanupScript, "cleanup-script", "", "Cleanup script")
	adminProfilesCreateCmd.Flags().StringVar(&profileTunnels, "tunnels", "", "Tunnels configuration JSON array")
	adminProfilesCreateCmd.Flags().StringVar(&profileSelector, "selector", "", "Selector JSON for runner matching")
	adminProfilesCreateCmd.Flags().StringArrayVar(&profileLabels, "label", nil, "Labels in key=value format (can be repeated)")
	adminProfilesCreateCmd.Flags().StringArrayVar(&profileAnnotations, "annotation", nil, "Annotations in key=value format (can be repeated)")
	_ = adminProfilesCreateCmd.MarkFlagRequired("name")
}

// List command flags
var (
	listProfileProviderConfigID string
	listProfileIncludeBuiltin   bool
	listProfileLimit            int
	listProfileLabels           []string
)

var adminProfilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List profiles",
	Long: `List profiles with optional filtering.

Examples:
  # List all user-created profiles
  mctl admin profiles list

  # Include built-in profiles
  mctl admin profiles list --include-builtin

  # Filter by provider config
  mctl admin profiles list --provider-config-id pcfg_xxx

  # Filter by labels
  mctl admin profiles list --label env=prod`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		opts := client.ListProfilesOptions{
			ProviderConfigID: listProfileProviderConfigID,
			IncludeBuiltin:   listProfileIncludeBuiltin,
			Limit:            listProfileLimit,
			Labels:           parseLabels(listProfileLabels),
		}

		result, err := adminClient.ListProfiles(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list profiles: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No profiles found.\n")
			return nil
		}

		// Print as table
		printf("%-24s %-20s %-30s %-10s %-20s\n", "ID", "NAME", "DESCRIPTION", "BUILTIN", "CREATED")
		printf("%-24s %-20s %-30s %-10s %-20s\n", "----", "----", "-----------", "-------", "-------")

		for _, profile := range result.Items {
			desc := "-"
			if profile.Description != nil && *profile.Description != "" {
				desc = truncate(*profile.Description, 30)
			}
			builtin := "no"
			if profile.IsBuiltin {
				builtin = "yes"
			}
			printf("%-24s %-20s %-30s %-10s %-20s\n",
				profile.ID,
				truncate(profile.Name, 20),
				desc,
				builtin,
				profile.CreatedAt.Format("2006-01-02 15:04:05"),
			)
		}

		if result.NextCursor != "" {
			printf("\nMore results available. Use --cursor %s to fetch next page.\n", result.NextCursor)
		}

		return nil
	},
}

func init() {
	adminProfilesListCmd.Flags().StringVar(&listProfileProviderConfigID, "provider-config-id", "", "Filter by provider config ID")
	adminProfilesListCmd.Flags().BoolVar(&listProfileIncludeBuiltin, "include-builtin", false, "Include built-in profiles")
	adminProfilesListCmd.Flags().IntVar(&listProfileLimit, "limit", 50, "Maximum number of profiles to return")
	adminProfilesListCmd.Flags().StringArrayVar(&listProfileLabels, "label", nil, "Filter by labels in key=value format")
}

var adminProfilesGetCmd = &cobra.Command{
	Use:   "get PROFILE_ID",
	Short: "Get a profile by ID",
	Long: `Get detailed information about a profile.

Examples:
  mctl admin profiles get prof_xxx`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		profileID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		profile, err := adminClient.GetProfile(ctx, profileID)
		if err != nil {
			return fmt.Errorf("failed to get profile: %w", err)
		}

		printf("ID:          %s\n", profile.ID)
		printf("Name:        %s\n", profile.Name)
		if profile.Description != nil && *profile.Description != "" {
			printf("Description: %s\n", *profile.Description)
		}
		if profile.ProviderConfigID != nil && *profile.ProviderConfigID != "" {
			printf("Provider:    %s\n", *profile.ProviderConfigID)
		}
		printf("Built-in:    %v\n", profile.IsBuiltin)
		printf("Created:     %s\n", profile.CreatedAt.Format(time.RFC3339))
		printf("Updated:     %s\n", profile.UpdatedAt.Format(time.RFC3339))

		// Print JSON fields if they have content
		if len(profile.Resources) > 2 { // More than just "{}"
			printf("\nResources:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Resources, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		if len(profile.Network) > 2 {
			printf("\nNetwork:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Network, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		if profile.InitScript != nil && *profile.InitScript != "" {
			printf("\nInit Script:\n  %s\n", *profile.InitScript)
		}

		if profile.CleanupScript != nil && *profile.CleanupScript != "" {
			printf("\nCleanup Script:\n  %s\n", *profile.CleanupScript)
		}

		if len(profile.Tunnels) > 2 { // More than just "[]"
			printf("\nTunnels:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Tunnels, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		if len(profile.Selector) > 2 {
			printf("\nSelector:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Selector, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		if len(profile.Labels) > 2 {
			printf("\nLabels:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Labels, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		if len(profile.Annotations) > 2 {
			printf("\nAnnotations:\n")
			prettyJSON, _ := json.MarshalIndent(profile.Annotations, "  ", "  ")
			printf("  %s\n", prettyJSON)
		}

		return nil
	},
}

// Update command flags
var (
	updateProfileName             string
	updateProfileDescription      string
	updateProfileProviderConfigID string
	updateProfileResources        string
	updateProfileNetwork          string
	updateProfileInitScript       string
	updateProfileCleanupScript    string
	updateProfileTunnels          string
	updateProfileSelector         string
	updateProfileLabels           []string
	updateProfileAnnotations      []string
)

var adminProfilesUpdateCmd = &cobra.Command{
	Use:   "update PROFILE_ID",
	Short: "Update a profile",
	Long: `Update an existing profile.

Examples:
  # Update profile name
  mctl admin profiles update prof_xxx --name new-name

  # Update resources
  mctl admin profiles update prof_xxx --resources '{"cpu":"4","memory":"8Gi"}'

  # Update network configuration
  mctl admin profiles update prof_xxx --network '{"policy":"proxy"}'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		profileID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		opts := client.UpdateProfileOptions{}
		hasUpdates := false

		if cmd.Flags().Changed("name") {
			opts.Name = &updateProfileName
			hasUpdates = true
		}
		if cmd.Flags().Changed("description") {
			opts.Description = &updateProfileDescription
			hasUpdates = true
		}
		if cmd.Flags().Changed("provider-config-id") {
			opts.ProviderConfigID = &updateProfileProviderConfigID
			hasUpdates = true
		}
		if cmd.Flags().Changed("init-script") {
			opts.InitScript = &updateProfileInitScript
			hasUpdates = true
		}
		if cmd.Flags().Changed("cleanup-script") {
			opts.CleanupScript = &updateProfileCleanupScript
			hasUpdates = true
		}

		if cmd.Flags().Changed("resources") {
			var resources map[string]any
			if err := json.Unmarshal([]byte(updateProfileResources), &resources); err != nil {
				return fmt.Errorf("invalid resources JSON: %w", err)
			}
			opts.Resources = &resources
			hasUpdates = true
		}

		if cmd.Flags().Changed("network") {
			var network map[string]any
			if err := json.Unmarshal([]byte(updateProfileNetwork), &network); err != nil {
				return fmt.Errorf("invalid network JSON: %w", err)
			}
			opts.Network = &network
			hasUpdates = true
		}

		if cmd.Flags().Changed("tunnels") {
			var tunnels []map[string]any
			if err := json.Unmarshal([]byte(updateProfileTunnels), &tunnels); err != nil {
				return fmt.Errorf("invalid tunnels JSON: %w", err)
			}
			opts.Tunnels = &tunnels
			hasUpdates = true
		}

		if cmd.Flags().Changed("selector") {
			var selector map[string]any
			if err := json.Unmarshal([]byte(updateProfileSelector), &selector); err != nil {
				return fmt.Errorf("invalid selector JSON: %w", err)
			}
			opts.Selector = &selector
			hasUpdates = true
		}

		if cmd.Flags().Changed("label") {
			labels := parseLabels(updateProfileLabels)
			opts.Labels = &labels
			hasUpdates = true
		}

		if cmd.Flags().Changed("annotation") {
			annotations := parseLabels(updateProfileAnnotations)
			opts.Annotations = &annotations
			hasUpdates = true
		}

		if !hasUpdates {
			return fmt.Errorf("no updates specified")
		}

		profile, err := adminClient.UpdateProfile(ctx, profileID, opts)
		if err != nil {
			return fmt.Errorf("failed to update profile: %w", err)
		}

		printf("Profile updated successfully!\n\n")
		printf("ID:          %s\n", profile.ID)
		printf("Name:        %s\n", profile.Name)
		if profile.Description != nil {
			printf("Description: %s\n", *profile.Description)
		}
		printf("Updated:     %s\n", profile.UpdatedAt.Format(time.RFC3339))

		return nil
	},
}

func init() {
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileName, "name", "", "New profile name")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileDescription, "description", "", "New profile description")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileProviderConfigID, "provider-config-id", "", "New provider configuration ID")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileResources, "resources", "", "New resources JSON")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileNetwork, "network", "", "New network configuration JSON")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileInitScript, "init-script", "", "New initialization script")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileCleanupScript, "cleanup-script", "", "New cleanup script")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileTunnels, "tunnels", "", "New tunnels configuration JSON array")
	adminProfilesUpdateCmd.Flags().StringVar(&updateProfileSelector, "selector", "", "New selector JSON")
	adminProfilesUpdateCmd.Flags().StringArrayVar(&updateProfileLabels, "label", nil, "New labels in key=value format")
	adminProfilesUpdateCmd.Flags().StringArrayVar(&updateProfileAnnotations, "annotation", nil, "New annotations in key=value format")
}

var adminProfilesDeleteCmd = &cobra.Command{
	Use:   "delete PROFILE_ID",
	Short: "Delete a profile",
	Long: `Delete a profile. Built-in profiles cannot be deleted.

Examples:
  mctl admin profiles delete prof_xxx`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		profileID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if err := adminClient.DeleteProfile(ctx, profileID); err != nil {
			return fmt.Errorf("failed to delete profile: %w", err)
		}

		printf("Profile %s deleted.\n", profileID)
		return nil
	},
}

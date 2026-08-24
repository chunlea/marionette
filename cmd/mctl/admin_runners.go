package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/spf13/cobra"
)

// adminRunnersCmd is the parent command for admin runner operations.
//
// The admin API has had spawn/list/get/destroy since R5 and CLAUDE.md has
// shown `mctl admin runners spawn` for longer than that, but the subcommand
// did not exist: the examples were fiction.
var adminRunnersCmd = &cobra.Command{
	Use:   "runners",
	Short: "Manage runners",
	Long: `Administrative operations for managing runners.

Runners spawned here are provisioned through a managed provider (Docker,
Kubernetes, E2B...). Pool runners join by themselves and are not spawned;
they show up in list once they connect.`,
}

func init() {
	adminCmd.AddCommand(adminRunnersCmd)
	adminRunnersCmd.AddCommand(adminRunnersSpawnCmd)
	adminRunnersCmd.AddCommand(adminRunnersListCmd)
	adminRunnersCmd.AddCommand(adminRunnersGetCmd)
	adminRunnersCmd.AddCommand(adminRunnersDestroyCmd)
}

// resolvePageLimit is the page size used while resolving a name to an id.
// The admin API caps a page at 100.
const resolvePageLimit = 100

// resolvePageBudget bounds how many pages a name lookup will walk, so a
// mistyped name cannot turn into an unbounded scan.
const resolvePageBudget = 20

// Spawn command flags
var (
	spawnRunnerName           string
	spawnRunnerProviderConfig string
	spawnRunnerProfile        string
	spawnRunnerLabels         []string
)

var adminRunnersSpawnCmd = &cobra.Command{
	Use:   "spawn",
	Short: "Spawn a runner through a managed provider",
	Long: `Spawn a runner through a managed provider configuration.

--provider-config and --profile each take either an ID or a name; a name is
resolved to its ID through the corresponding list endpoint.

Examples:
  # Spawn through a provider config, by name
  mctl admin runners spawn --provider-config docker-local

  # Name the runner and put it on a profile
  mctl admin runners spawn --provider-config pcfg_xxx --name runner-1 --profile dev-small

  # Spawn with labels
  mctl admin runners spawn --provider-config docker-local --label env=dev --label team=backend`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if spawnRunnerProviderConfig == "" {
			return fmt.Errorf("--provider-config is required")
		}

		providerConfigID, err := resolveProviderConfigID(ctx, spawnRunnerProviderConfig)
		if err != nil {
			return err
		}

		profileID, err := resolveProfileID(ctx, spawnRunnerProfile)
		if err != nil {
			return err
		}

		runner, err := adminClient.SpawnRunner(ctx, client.SpawnRunnerOptions{
			Name:             spawnRunnerName,
			ProviderConfigID: providerConfigID,
			ProfileID:        profileID,
			Labels:           parseLabels(spawnRunnerLabels),
		})
		if err != nil {
			return fmt.Errorf("failed to spawn runner: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintAdminRunner(runner)
	},
}

func init() {
	adminRunnersSpawnCmd.Flags().StringVar(&spawnRunnerName, "name", "", "Runner name (defaults to a generated one)")
	adminRunnersSpawnCmd.Flags().StringVar(&spawnRunnerProviderConfig, "provider-config", "", "Provider config ID or name (required)")
	adminRunnersSpawnCmd.Flags().StringVar(&spawnRunnerProfile, "profile", "", "Profile ID or name")
	adminRunnersSpawnCmd.Flags().StringArrayVar(&spawnRunnerLabels, "label", nil, "Labels in key=value format (can be repeated)")
	_ = adminRunnersSpawnCmd.MarkFlagRequired("provider-config")
}

// List command flags
var (
	listRunnerStatus   []string
	listRunnerPoolName string
	listRunnerLabels   []string
	listRunnerLimit    int
	listRunnerCursor   string
)

var adminRunnersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List runners",
	Long: `List runners with optional filtering.

This is the operator's view: unlike the public runner list it names the
provider config behind each runner, and marks the tainted ones.

Examples:
  # List all runners
  mctl admin runners list

  # Only the idle ones
  mctl admin runners list --status idle

  # Runners in a pool, as JSON
  mctl admin runners list --pool-name macos-pool -o json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		result, err := adminClient.ListRunners(ctx, client.ListRunnersOptions{
			Status:   listRunnerStatus,
			PoolName: listRunnerPoolName,
			Labels:   parseLabels(listRunnerLabels),
			Limit:    listRunnerLimit,
			Cursor:   listRunnerCursor,
		})
		if err != nil {
			return fmt.Errorf("failed to list runners: %w", err)
		}

		// The "nothing here" line and the pagination hint are prose, so they
		// belong to the table rendering only: printing them in JSON or YAML
		// mode would hand the caller a document that does not parse.
		if outputFmt != string(OutputTable) {
			printer := NewPrinter(outputFmt, getOutput())
			return printer.PrintAdminRunnerList(result.Items)
		}

		if len(result.Items) == 0 {
			printf("No runners found.\n")
			return nil
		}

		printer := NewPrinter(outputFmt, getOutput())
		if err := printer.PrintAdminRunnerList(result.Items); err != nil {
			return err
		}

		if result.NextCursor != "" {
			printf("\nMore results available. Use --cursor %s to fetch next page.\n", result.NextCursor)
		}

		return nil
	},
}

func init() {
	adminRunnersListCmd.Flags().StringSliceVar(&listRunnerStatus, "status", nil, "Filter by status (offline, idle, busy, paused)")
	adminRunnersListCmd.Flags().StringVar(&listRunnerPoolName, "pool-name", "", "Filter by pool name")
	adminRunnersListCmd.Flags().StringArrayVar(&listRunnerLabels, "label", nil, "Filter by labels in key=value format")
	adminRunnersListCmd.Flags().IntVar(&listRunnerLimit, "limit", 50, "Maximum number of runners to return")
	adminRunnersListCmd.Flags().StringVar(&listRunnerCursor, "cursor", "", "Pagination cursor from a previous list")
}

var adminRunnersGetCmd = &cobra.Command{
	Use:   "get RUNNER_ID",
	Short: "Get a runner by ID",
	Long: `Get detailed information about a runner.

Examples:
  mctl admin runners get run_xxx
  mctl admin runners get run_xxx -o yaml`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		runner, err := adminClient.GetRunner(ctx, args[0])
		if err != nil {
			return fmt.Errorf("failed to get runner: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintAdminRunner(runner)
	},
}

var adminRunnersDestroyCmd = &cobra.Command{
	Use:   "destroy RUNNER_ID",
	Short: "Destroy a runner",
	Long: `Destroy a runner, terminating it through its provider.

A busy runner is refused; suspend or cancel its session first.

Examples:
  mctl admin runners destroy run_xxx`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		runnerID := args[0]

		if adminClient == nil {
			return fmt.Errorf("admin client not configured")
		}

		if err := adminClient.DestroyRunner(ctx, runnerID); err != nil {
			return fmt.Errorf("failed to destroy runner: %w", err)
		}

		printf("Runner %s destroyed.\n", runnerID)
		return nil
	},
}

// resolveProviderConfigID accepts either a provider config ID or a name and
// returns the ID. Names are matched exactly, and an ambiguous one is an error
// rather than a silent pick.
func resolveProviderConfigID(ctx context.Context, ref string) (string, error) {
	if ref == "" || id.IsProviderConfig(ref) {
		return ref, nil
	}

	var matches []string
	cursor := ""
	for page := 0; page < resolvePageBudget; page++ {
		result, err := adminClient.ListProviderConfigs(ctx, client.ListProviderConfigsOptions{
			Limit:  resolvePageLimit,
			Cursor: cursor,
		})
		if err != nil {
			return "", fmt.Errorf("failed to resolve provider config %q: %w", ref, err)
		}

		for _, cfg := range result.Items {
			if cfg.Name == ref {
				matches = append(matches, cfg.ID)
			}
		}

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	return singleMatch("provider config", ref, matches)
}

// resolveProfileID accepts either a profile ID or a name and returns the ID.
// Built-in profiles are included: they are exactly the ones an operator is
// most likely to name.
func resolveProfileID(ctx context.Context, ref string) (string, error) {
	if ref == "" || id.IsProfile(ref) {
		return ref, nil
	}

	var matches []string
	cursor := ""
	for page := 0; page < resolvePageBudget; page++ {
		result, err := adminClient.ListProfiles(ctx, client.ListProfilesOptions{
			Limit:          resolvePageLimit,
			Cursor:         cursor,
			IncludeBuiltin: true,
		})
		if err != nil {
			return "", fmt.Errorf("failed to resolve profile %q: %w", ref, err)
		}

		for _, profile := range result.Items {
			if profile.Name == ref {
				matches = append(matches, profile.ID)
			}
		}

		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	return singleMatch("profile", ref, matches)
}

// singleMatch turns the result of a name lookup into one id, or says why it
// cannot. Passing the id instead of the name is always available as a way out,
// so both errors say so.
func singleMatch(kind, ref string, matches []string) (string, error) {
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no %s named %q; pass its ID instead, or check `mctl admin %ss list`",
			kind, ref, strings.ReplaceAll(kind, " ", "-"))
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("%d %ss are named %q (%s); pass the ID you mean",
			len(matches), kind, ref, strings.Join(matches, ", "))
	}
}

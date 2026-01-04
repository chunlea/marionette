package main

import (
	"fmt"
	"strings"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// sessionsCreateFlags holds flags for the sessions create command.
var sessionsCreateFlags struct {
	name               string
	agent              string
	agentConfig        string
	agentAPIKey        string
	lifecycleMode      string
	idleTimeoutSeconds int
	labels             []string
}

// sessionsListFlags holds flags for the sessions list command.
var sessionsListFlags struct {
	status []string
	agent  string
	labels []string
	limit  int
}

// apiClient holds the client instance (set during command execution).
var apiClient client.Client

// clientWasSet tracks whether SetClient was called (for testing).
var clientWasSet bool

// SetClient sets the API client for commands (used in testing).
func SetClient(c client.Client) {
	apiClient = c
	clientWasSet = true
}

// ResetClient resets the client state (used in testing).
func ResetClient() {
	apiClient = nil
	clientWasSet = false
}

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"session", "sess"},
	Short:   "Manage sessions",
	Long:    `Manage Marionette sessions - long-lived work contexts for AI coding agents.`,
}

func init() {
	sessionsCmd.AddCommand(sessionsCreateCmd)
	sessionsCmd.AddCommand(sessionsListCmd)
	sessionsCmd.AddCommand(sessionsGetCmd)
	sessionsCmd.AddCommand(sessionsSuspendCmd)
	sessionsCmd.AddCommand(sessionsResumeCmd)
	sessionsCmd.AddCommand(sessionsTerminateCmd)

	// Flags for sessions create
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateFlags.name, "name", "", "session name")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateFlags.agent, "agent", "claude", "agent type (claude, codex, etc.)")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateFlags.agentConfig, "agent-config", "", "agent config ID to use")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateFlags.agentAPIKey, "agent-api-key", "", "API key for BYOK mode")
	sessionsCreateCmd.Flags().StringVar(&sessionsCreateFlags.lifecycleMode, "lifecycle", "on_demand", "lifecycle mode (on_demand, always_on, scheduled)")
	sessionsCreateCmd.Flags().IntVar(&sessionsCreateFlags.idleTimeoutSeconds, "idle-timeout", 1800, "idle timeout in seconds (for on_demand)")
	sessionsCreateCmd.Flags().StringSliceVar(&sessionsCreateFlags.labels, "labels", nil, "labels in key=value format")

	// Flags for sessions list
	sessionsListCmd.Flags().StringSliceVar(&sessionsListFlags.status, "status", nil, "filter by status")
	sessionsListCmd.Flags().StringVar(&sessionsListFlags.agent, "agent", "", "filter by agent type")
	sessionsListCmd.Flags().StringSliceVar(&sessionsListFlags.labels, "labels", nil, "filter by labels in key=value format")
	sessionsListCmd.Flags().IntVar(&sessionsListFlags.limit, "limit", 50, "maximum number of results")
}

var sessionsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new session",
	Long: `Create a new session with the specified agent and configuration.

Examples:
  # Create a session with default agent
  mctl sessions create --name my-project

  # Create a session with BYOK API key
  mctl sessions create --agent claude --agent-api-key $ANTHROPIC_API_KEY

  # Create an always-on session
  mctl sessions create --name assistant --lifecycle always_on`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.CreateSessionOptions{
			Name:               sessionsCreateFlags.name,
			Agent:              sessionsCreateFlags.agent,
			AgentConfigID:      sessionsCreateFlags.agentConfig,
			APIKey:             sessionsCreateFlags.agentAPIKey,
			LifecycleMode:      sessionsCreateFlags.lifecycleMode,
			IdleTimeoutSeconds: sessionsCreateFlags.idleTimeoutSeconds,
			Labels:             parseLabels(sessionsCreateFlags.labels),
		}

		session, err := apiClient.CreateSession(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintSession(session)
	},
}

var sessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions",
	Long: `List sessions with optional filtering.

Examples:
  # List all sessions
  mctl sessions list

  # List active sessions only
  mctl sessions list --status active

  # List sessions with specific labels
  mctl sessions list --labels env=prod,team=backend`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.ListSessionsOptions{
			Limit:  sessionsListFlags.limit,
			Status: sessionsListFlags.status,
			Agent:  sessionsListFlags.agent,
			Labels: parseLabels(sessionsListFlags.labels),
		}

		result, err := apiClient.ListSessions(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No sessions found.\n")
			return nil
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintSessionList(result.Items)
	},
}

var sessionsGetCmd = &cobra.Command{
	Use:   "get SESSION_ID",
	Short: "Get session details",
	Long: `Get detailed information about a specific session.

Example:
  mctl sessions get sess_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		session, err := apiClient.GetSession(ctx, sessionID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("session %q not found", sessionID)
			}
			return fmt.Errorf("failed to get session: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintSession(session)
	},
}

var sessionsSuspendCmd = &cobra.Command{
	Use:   "suspend SESSION_ID",
	Short: "Suspend a session",
	Long: `Suspend an active session, releasing its runner while preserving state.

Example:
  mctl sessions suspend sess_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.SuspendSession(ctx, sessionID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("session %q not found", sessionID)
			}
			return fmt.Errorf("failed to suspend session: %w", err)
		}

		printf("Session %s suspended.\n", sessionID)
		return nil
	},
}

var sessionsResumeCmd = &cobra.Command{
	Use:   "resume SESSION_ID",
	Short: "Resume a suspended session",
	Long: `Resume a suspended session, attaching a new runner.

Example:
  mctl sessions resume sess_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.ResumeSession(ctx, sessionID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("session %q not found", sessionID)
			}
			return fmt.Errorf("failed to resume session: %w", err)
		}

		printf("Session %s resuming.\n", sessionID)
		return nil
	},
}

var sessionsTerminateCmd = &cobra.Command{
	Use:   "terminate SESSION_ID",
	Short: "Terminate a session",
	Long: `Terminate a session and clean up all associated resources.

Example:
  mctl sessions terminate sess_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.TerminateSession(ctx, sessionID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("session %q not found", sessionID)
			}
			return fmt.Errorf("failed to terminate session: %w", err)
		}

		printf("Session %s terminated.\n", sessionID)
		return nil
	},
}

// parseLabels parses label strings in key=value format.
func parseLabels(labels []string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	result := make(map[string]string)
	for _, label := range labels {
		parts := strings.SplitN(label, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

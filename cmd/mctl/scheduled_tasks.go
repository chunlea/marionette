package main

import (
	"fmt"
	"os"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// scheduledTasksCreateFlags holds flags for the scheduled-tasks create command.
var scheduledTasksCreateFlags struct {
	sessionID              string
	name                   string
	description            string
	cronExpression         string
	timezone               string
	promptTemplate         string
	promptFile             string
	timeoutSeconds         int
	maxRetries             int
	onFailure              string
	maxConsecutiveFailures int
	labels                 []string
}

// scheduledTasksListFlags holds flags for the scheduled-tasks list command.
var scheduledTasksListFlags struct {
	sessionID string
	status    []string
	limit     int
}

// scheduledTasksUpdateFlags holds flags for the scheduled-tasks update command.
var scheduledTasksUpdateFlags struct {
	name                   string
	description            string
	cronExpression         string
	timezone               string
	promptTemplate         string
	promptFile             string
	timeoutSeconds         int
	maxRetries             int
	onFailure              string
	maxConsecutiveFailures int
	labels                 []string
}

var scheduledTasksCmd = &cobra.Command{
	Use:     "scheduled-tasks",
	Aliases: []string{"scheduled-task", "stask", "stasks"},
	Short:   "Manage scheduled tasks",
	Long:    `Manage scheduled tasks - recurring tasks triggered by cron schedules.`,
}

func init() {
	scheduledTasksCmd.AddCommand(scheduledTasksCreateCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksListCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksGetCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksUpdateCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksDeleteCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksPauseCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksResumeCmd)
	scheduledTasksCmd.AddCommand(scheduledTasksTriggerCmd)

	// Flags for scheduled-tasks create
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.sessionID, "session", "", "session ID (required)")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.name, "name", "", "scheduled task name (required)")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.description, "description", "", "scheduled task description")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.cronExpression, "cron", "", "cron expression (required)")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.timezone, "timezone", "UTC", "timezone for cron expression")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.promptTemplate, "prompt", "", "prompt template")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.promptFile, "prompt-file", "", "read prompt template from file")
	scheduledTasksCreateCmd.Flags().IntVar(&scheduledTasksCreateFlags.timeoutSeconds, "timeout", 3600, "task timeout in seconds")
	scheduledTasksCreateCmd.Flags().IntVar(&scheduledTasksCreateFlags.maxRetries, "max-retries", 0, "maximum retry attempts")
	scheduledTasksCreateCmd.Flags().StringVar(&scheduledTasksCreateFlags.onFailure, "on-failure", "continue", "failure policy (continue, pause_on_failure, disable_on_failure)")
	scheduledTasksCreateCmd.Flags().IntVar(&scheduledTasksCreateFlags.maxConsecutiveFailures, "max-consecutive-failures", 3, "max consecutive failures before action")
	scheduledTasksCreateCmd.Flags().StringSliceVar(&scheduledTasksCreateFlags.labels, "labels", nil, "labels in key=value format")

	// Flags for scheduled-tasks list
	scheduledTasksListCmd.Flags().StringVar(&scheduledTasksListFlags.sessionID, "session", "", "filter by session ID")
	scheduledTasksListCmd.Flags().StringSliceVar(&scheduledTasksListFlags.status, "status", nil, "filter by status (active, paused, disabled)")
	scheduledTasksListCmd.Flags().IntVar(&scheduledTasksListFlags.limit, "limit", 50, "maximum number of results")

	// Flags for scheduled-tasks update
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.name, "name", "", "updated name")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.description, "description", "", "updated description")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.cronExpression, "cron", "", "updated cron expression")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.timezone, "timezone", "", "updated timezone")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.promptTemplate, "prompt", "", "updated prompt template")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.promptFile, "prompt-file", "", "read updated prompt template from file")
	scheduledTasksUpdateCmd.Flags().IntVar(&scheduledTasksUpdateFlags.timeoutSeconds, "timeout", 0, "updated timeout in seconds")
	scheduledTasksUpdateCmd.Flags().IntVar(&scheduledTasksUpdateFlags.maxRetries, "max-retries", -1, "updated max retries (-1 to keep current)")
	scheduledTasksUpdateCmd.Flags().StringVar(&scheduledTasksUpdateFlags.onFailure, "on-failure", "", "updated failure policy")
	scheduledTasksUpdateCmd.Flags().IntVar(&scheduledTasksUpdateFlags.maxConsecutiveFailures, "max-consecutive-failures", -1, "updated max consecutive failures (-1 to keep current)")
	scheduledTasksUpdateCmd.Flags().StringSliceVar(&scheduledTasksUpdateFlags.labels, "labels", nil, "updated labels in key=value format")
}

var scheduledTasksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new scheduled task",
	Long: `Create a new scheduled task with a cron schedule.

Examples:
  # Create a daily task at 9am
  mctl scheduled-tasks create --session sess_xxx --name "daily-report" \
    --cron "0 9 * * *" --prompt "Generate a daily status report"

  # Create a weekly task (Mondays at 9am)
  mctl scheduled-tasks create --session sess_xxx --name "weekly-summary" \
    --cron "0 9 * * 1" --timezone "America/Los_Angeles" \
    --prompt-file weekly-prompt.md

  # Create with failure handling
  mctl scheduled-tasks create --session sess_xxx --name "critical-job" \
    --cron "*/30 * * * *" --prompt "Check system status" \
    --on-failure pause_on_failure`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		// Validate required fields
		if scheduledTasksCreateFlags.sessionID == "" {
			return fmt.Errorf("--session is required")
		}
		if scheduledTasksCreateFlags.name == "" {
			return fmt.Errorf("--name is required")
		}
		if scheduledTasksCreateFlags.cronExpression == "" {
			return fmt.Errorf("--cron is required")
		}

		// Get prompt from --prompt or --prompt-file
		promptTemplate := scheduledTasksCreateFlags.promptTemplate
		if scheduledTasksCreateFlags.promptFile != "" {
			data, err := os.ReadFile(scheduledTasksCreateFlags.promptFile)
			if err != nil {
				return fmt.Errorf("failed to read prompt file: %w", err)
			}
			promptTemplate = string(data)
		}
		if promptTemplate == "" {
			return fmt.Errorf("either --prompt or --prompt-file is required")
		}

		// Parse labels
		labels := parseLabels(scheduledTasksCreateFlags.labels)

		opts := client.CreateScheduledTaskOptions{
			SessionID:      scheduledTasksCreateFlags.sessionID,
			Name:           scheduledTasksCreateFlags.name,
			Description:    scheduledTasksCreateFlags.description,
			CronExpression: scheduledTasksCreateFlags.cronExpression,
			Timezone:       scheduledTasksCreateFlags.timezone,
			PromptTemplate: promptTemplate,
			TimeoutSeconds: scheduledTasksCreateFlags.timeoutSeconds,
			MaxRetries:     scheduledTasksCreateFlags.maxRetries,
			OnFailure:      scheduledTasksCreateFlags.onFailure,
			Labels:         labels,
		}

		if scheduledTasksCreateFlags.maxConsecutiveFailures > 0 {
			opts.MaxConsecutiveFailures = &scheduledTasksCreateFlags.maxConsecutiveFailures
		}

		task, err := apiClient.CreateScheduledTask(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create scheduled task: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTask(task)
	},
}

var scheduledTasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled tasks",
	Long: `List scheduled tasks with optional filtering.

Examples:
  # List all scheduled tasks
  mctl scheduled-tasks list

  # List scheduled tasks for a session
  mctl scheduled-tasks list --session sess_xxx

  # List only active scheduled tasks
  mctl scheduled-tasks list --status active`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.ListScheduledTasksOptions{
			Limit:     scheduledTasksListFlags.limit,
			SessionID: scheduledTasksListFlags.sessionID,
			Status:    scheduledTasksListFlags.status,
		}

		result, err := apiClient.ListScheduledTasks(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list scheduled tasks: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No scheduled tasks found.\n")
			return nil
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTaskList(result.Items)
	},
}

var scheduledTasksGetCmd = &cobra.Command{
	Use:   "get SCHEDULED_TASK_ID",
	Short: "Get scheduled task details",
	Long: `Get detailed information about a specific scheduled task.

Example:
  mctl scheduled-tasks get stsk_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		task, err := apiClient.GetScheduledTask(ctx, taskID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to get scheduled task: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTask(task)
	},
}

var scheduledTasksUpdateCmd = &cobra.Command{
	Use:   "update SCHEDULED_TASK_ID",
	Short: "Update a scheduled task",
	Long: `Update an existing scheduled task.

Examples:
  # Update the cron schedule
  mctl scheduled-tasks update stsk_xxx --cron "0 10 * * *"

  # Update the prompt template
  mctl scheduled-tasks update stsk_xxx --prompt "New prompt"

  # Update failure policy
  mctl scheduled-tasks update stsk_xxx --on-failure disable_on_failure`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.UpdateScheduledTaskOptions{}

		// Only set fields that were explicitly provided
		if cmd.Flags().Changed("name") {
			opts.Name = &scheduledTasksUpdateFlags.name
		}
		if cmd.Flags().Changed("description") {
			opts.Description = &scheduledTasksUpdateFlags.description
		}
		if cmd.Flags().Changed("cron") {
			opts.CronExpression = &scheduledTasksUpdateFlags.cronExpression
		}
		if cmd.Flags().Changed("timezone") {
			opts.Timezone = &scheduledTasksUpdateFlags.timezone
		}
		if cmd.Flags().Changed("prompt") {
			opts.PromptTemplate = &scheduledTasksUpdateFlags.promptTemplate
		}
		if cmd.Flags().Changed("prompt-file") {
			data, err := os.ReadFile(scheduledTasksUpdateFlags.promptFile)
			if err != nil {
				return fmt.Errorf("failed to read prompt file: %w", err)
			}
			prompt := string(data)
			opts.PromptTemplate = &prompt
		}
		if cmd.Flags().Changed("timeout") {
			opts.TimeoutSeconds = &scheduledTasksUpdateFlags.timeoutSeconds
		}
		if cmd.Flags().Changed("max-retries") && scheduledTasksUpdateFlags.maxRetries >= 0 {
			opts.MaxRetries = &scheduledTasksUpdateFlags.maxRetries
		}
		if cmd.Flags().Changed("on-failure") {
			opts.OnFailure = &scheduledTasksUpdateFlags.onFailure
		}
		if cmd.Flags().Changed("max-consecutive-failures") && scheduledTasksUpdateFlags.maxConsecutiveFailures >= 0 {
			opts.MaxConsecutiveFailures = &scheduledTasksUpdateFlags.maxConsecutiveFailures
		}
		if cmd.Flags().Changed("labels") {
			opts.Labels = parseLabels(scheduledTasksUpdateFlags.labels)
		}

		task, err := apiClient.UpdateScheduledTask(ctx, taskID, opts)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to update scheduled task: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTask(task)
	},
}

var scheduledTasksDeleteCmd = &cobra.Command{
	Use:   "delete SCHEDULED_TASK_ID",
	Short: "Delete a scheduled task",
	Long: `Delete a scheduled task.

Example:
  mctl scheduled-tasks delete stsk_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.DeleteScheduledTask(ctx, taskID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to delete scheduled task: %w", err)
		}

		printf("Scheduled task %s deleted.\n", taskID)
		return nil
	},
}

var scheduledTasksPauseCmd = &cobra.Command{
	Use:   "pause SCHEDULED_TASK_ID",
	Short: "Pause a scheduled task",
	Long: `Pause a scheduled task. It will not trigger until resumed.

Example:
  mctl scheduled-tasks pause stsk_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		task, err := apiClient.PauseScheduledTask(ctx, taskID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to pause scheduled task: %w", err)
		}

		printf("Scheduled task %s paused.\n", taskID)
		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTask(task)
	},
}

var scheduledTasksResumeCmd = &cobra.Command{
	Use:   "resume SCHEDULED_TASK_ID",
	Short: "Resume a paused scheduled task",
	Long: `Resume a paused scheduled task.

Example:
  mctl scheduled-tasks resume stsk_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		task, err := apiClient.ResumeScheduledTask(ctx, taskID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to resume scheduled task: %w", err)
		}

		printf("Scheduled task %s resumed.\n", taskID)
		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintScheduledTask(task)
	},
}

var scheduledTasksTriggerCmd = &cobra.Command{
	Use:   "trigger SCHEDULED_TASK_ID",
	Short: "Manually trigger a scheduled task",
	Long: `Manually trigger a scheduled task immediately.

Example:
  mctl scheduled-tasks trigger stsk_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		task, err := apiClient.TriggerScheduledTask(ctx, taskID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("scheduled task %q not found", taskID)
			}
			return fmt.Errorf("failed to trigger scheduled task: %w", err)
		}

		printf("Scheduled task triggered. Created task: %s\n", task.ID)
		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTask(task)
	},
}

package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// tasksCreateFlags holds flags for the tasks create command.
var tasksCreateFlags struct {
	sessionID      string
	prompt         string
	promptFile     string
	continueFrom   string
	timeoutSeconds int
	maxRetries     int
	wait           bool
	follow         bool
}

// tasksListFlags holds flags for the tasks list command.
var tasksListFlags struct {
	sessionID string
	status    []string
	limit     int
}

// tasksLogsFlags holds flags for the tasks logs command.
var tasksLogsFlags struct {
	follow bool
	tail   int
}

var tasksCmd = &cobra.Command{
	Use:     "tasks",
	Aliases: []string{"task"},
	Short:   "Manage tasks",
	Long:    `Manage Marionette tasks - units of work executed by AI coding agents.`,
}

func init() {
	tasksCmd.AddCommand(tasksCreateCmd)
	tasksCmd.AddCommand(tasksListCmd)
	tasksCmd.AddCommand(tasksGetCmd)
	tasksCmd.AddCommand(tasksLogsCmd)
	tasksCmd.AddCommand(tasksCancelCmd)

	// Flags for tasks create
	tasksCreateCmd.Flags().StringVar(&tasksCreateFlags.sessionID, "session", "", "session ID (required)")
	tasksCreateCmd.Flags().StringVar(&tasksCreateFlags.prompt, "prompt", "", "task prompt")
	tasksCreateCmd.Flags().StringVar(&tasksCreateFlags.promptFile, "prompt-file", "", "read prompt from file")
	tasksCreateCmd.Flags().StringVar(&tasksCreateFlags.continueFrom, "continue", "", "continue from previous task ID")
	tasksCreateCmd.Flags().IntVar(&tasksCreateFlags.timeoutSeconds, "timeout", 3600, "task timeout in seconds")
	tasksCreateCmd.Flags().IntVar(&tasksCreateFlags.maxRetries, "max-retries", 0, "maximum retry attempts")
	tasksCreateCmd.Flags().BoolVar(&tasksCreateFlags.wait, "wait", false, "wait for task completion")
	tasksCreateCmd.Flags().BoolVarP(&tasksCreateFlags.follow, "follow", "f", false, "follow task logs")

	// Flags for tasks list
	tasksListCmd.Flags().StringVar(&tasksListFlags.sessionID, "session", "", "filter by session ID")
	tasksListCmd.Flags().StringSliceVar(&tasksListFlags.status, "status", nil, "filter by status")
	tasksListCmd.Flags().IntVar(&tasksListFlags.limit, "limit", 50, "maximum number of results")

	// Flags for tasks logs
	tasksLogsCmd.Flags().BoolVarP(&tasksLogsFlags.follow, "follow", "f", false, "stream logs in real-time")
	tasksLogsCmd.Flags().IntVar(&tasksLogsFlags.tail, "tail", 0, "show last N lines (0 for all)")
}

var tasksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	Long: `Create a new task in a session with the specified prompt.

Examples:
  # Create a task with inline prompt
  mctl tasks create --session sess_xxx --prompt "Build a REST API"

  # Create a task from file
  mctl tasks create --session sess_xxx --prompt-file task.md

  # Continue from a previous task
  mctl tasks create --continue task_xxx --prompt "Add authentication"

  # Create and follow logs
  mctl tasks create --session sess_xxx --prompt "Build API" --follow`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		// Get prompt from --prompt or --prompt-file
		prompt := tasksCreateFlags.prompt
		if tasksCreateFlags.promptFile != "" {
			data, err := os.ReadFile(tasksCreateFlags.promptFile)
			if err != nil {
				return fmt.Errorf("failed to read prompt file: %w", err)
			}
			prompt = string(data)
		}

		if prompt == "" && tasksCreateFlags.continueFrom == "" {
			return fmt.Errorf("either --prompt, --prompt-file, or --continue is required")
		}

		sessionID := tasksCreateFlags.sessionID
		if sessionID == "" && tasksCreateFlags.continueFrom == "" {
			return fmt.Errorf("--session is required unless --continue is specified")
		}

		opts := client.CreateTaskOptions{
			SessionID:      sessionID,
			Prompt:         prompt,
			ContinueFrom:   tasksCreateFlags.continueFrom,
			TimeoutSeconds: tasksCreateFlags.timeoutSeconds,
			MaxRetries:     tasksCreateFlags.maxRetries,
		}

		task, err := apiClient.CreateTask(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		if err := printer.PrintTask(task); err != nil {
			return err
		}

		// If --follow is set, stream logs
		if tasksCreateFlags.follow {
			printf("\n--- Logs ---\n")
			return streamTaskLogs(ctx, task.ID, true)
		}

		return nil
	},
}

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	Long: `List tasks with optional filtering.

Examples:
  # List all tasks in a session
  mctl tasks list --session sess_xxx

  # List running tasks
  mctl tasks list --status running

  # List with custom limit
  mctl tasks list --session sess_xxx --limit 100`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.ListTasksOptions{
			Limit:     tasksListFlags.limit,
			SessionID: tasksListFlags.sessionID,
			Status:    tasksListFlags.status,
		}

		result, err := apiClient.ListTasks(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No tasks found.\n")
			return nil
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTaskList(result.Items)
	},
}

var tasksGetCmd = &cobra.Command{
	Use:   "get TASK_ID",
	Short: "Get task details",
	Long: `Get detailed information about a specific task.

Example:
  mctl tasks get task_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		task, err := apiClient.GetTask(ctx, taskID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("task %q not found", taskID)
			}
			return fmt.Errorf("failed to get task: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTask(task)
	},
}

var tasksLogsCmd = &cobra.Command{
	Use:   "logs TASK_ID",
	Short: "Get task logs",
	Long: `Get logs from a task execution. Use --follow to stream logs in real-time.

Examples:
  # Get all logs
  mctl tasks logs task_xxx

  # Stream logs in real-time
  mctl tasks logs task_xxx --follow

  # Get last 100 lines
  mctl tasks logs task_xxx --tail 100`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		return streamTaskLogs(ctx, taskID, tasksLogsFlags.follow)
	},
}

var tasksCancelCmd = &cobra.Command{
	Use:   "cancel TASK_ID",
	Short: "Cancel a task",
	Long: `Cancel a pending or running task.

Example:
  mctl tasks cancel task_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		taskID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.CancelTask(ctx, taskID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("task %q not found", taskID)
			}
			return fmt.Errorf("failed to cancel task: %w", err)
		}

		printf("Task %s canceled.\n", taskID)
		return nil
	},
}

// streamTaskLogs streams logs for a task.
func streamTaskLogs(ctx interface{ Done() <-chan struct{} }, taskID string, follow bool) error {
	opts := client.GetLogsOptions{
		Follow: follow,
		Tail:   tasksLogsFlags.tail,
	}

	iter, err := apiClient.GetTaskLogs(ctx.(interface {
		Done() <-chan struct{}
		Err() error
		Value(key any) any
		Deadline() (deadline time.Time, ok bool)
	}), taskID, opts)
	if err != nil {
		if client.IsNotFound(err) {
			return fmt.Errorf("task %q not found", taskID)
		}
		return fmt.Errorf("failed to get logs: %w", err)
	}
	defer func() { _ = iter.Close() }()

	for {
		log, err := iter.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("error reading logs: %w", err)
		}

		// Format: [timestamp] [level] content
		timestamp := log.CreatedAt.Format("15:04:05")
		printf("[%s] [%s] %s\n", timestamp, log.Level, log.Content)
	}
}

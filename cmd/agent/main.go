// Package main provides the marionette-agent binary.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent"
	"github.com/chunlea/marionette/pkg/agent/executor"
	"github.com/chunlea/marionette/pkg/agent/executor/claude"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Parse command-line flags
	flags := pflag.NewFlagSet("agent", pflag.ExitOnError)
	configPath := flags.String("config", "", "path to config file")
	agent.BindFlags(flags)

	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	// Load configuration with flags
	cfg, err := agent.LoadWithFlags(*configPath, flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := newLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("marionette agent starting",
		zap.String("server", cfg.Server.Address),
		zap.String("name", cfg.Runner.Name),
		zap.String("sandbox_mode", cfg.Sandbox.Mode),
	)

	// Create client
	client := agent.NewClient(cfg, logger)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigC
		logger.Info("received shutdown signal", zap.String("signal", sig.String()))
		cancel()
	}()

	// Connect to server with retry
	if err := connectWithRetry(ctx, client, cfg, logger); err != nil {
		if ctx.Err() != nil {
			logger.Info("shutdown during connection")
		} else {
			logger.Fatal("failed to connect to server", zap.Error(err))
		}
		return
	}

	logger.Info("connected to server",
		zap.String("runner_id", client.RunnerID()),
	)

	// Create workspace manager and command handler
	workspaceMgr := agent.NewWorkspaceManager(cfg.Workspace.BaseDir, logger)
	cmdHandler := agent.NewDefaultCommandHandler(workspaceMgr, logger)

	// Create log streamer for streaming output to server
	logStreamer := agent.NewLogStreamer(client, client.RunnerID(), "", logger)

	// Create executor
	claudeExecutor := claude.New(logger)

	// Wire executor callbacks
	cmdHandler.OnExecuteTask = func(ctx context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
		return executeTask(ctx, cmd, cmdHandler, claudeExecutor, logStreamer, logger)
	}

	cmdHandler.OnKillTask = func(_ context.Context, _ *pb.KillTask) error {
		return claudeExecutor.Kill()
	}

	// Start log streamer
	if err := logStreamer.Start(ctx); err != nil {
		logger.Fatal("failed to start log streamer", zap.Error(err))
	}

	// Create control channel
	controlChannel := agent.NewControlChannel(client, cmdHandler, logger)

	// Start control channel
	if err := controlChannel.Start(ctx); err != nil {
		logger.Fatal("failed to start control channel", zap.Error(err))
	}
	logger.Info("control channel started")

	// Start heartbeat loop
	hbLoop := agent.NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	hbLoop.Start(ctx)

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	logger.Info("initiating graceful shutdown")

	// Stop control channel
	controlChannel.Stop()

	// Stop heartbeat loop
	hbLoop.Stop()

	// Stop log streamer
	logStreamer.Stop()

	// Close client with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	done := make(chan error, 1)
	go func() {
		done <- client.Close()
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("error closing client", zap.Error(err))
		}
	case <-shutdownCtx.Done():
		logger.Warn("shutdown timeout exceeded")
	}

	logger.Info("marionette agent stopped")
}

func connectWithRetry(ctx context.Context, client *agent.Client, _ *agent.Config, logger *zap.Logger) error {
	backoff := agent.NewExponentialBackoff(agent.BackoffConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.2,
		MaxRetries:   -1, // Unlimited retries
	})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := client.Connect(ctx); err != nil {
			delay := backoff.Next()
			if delay == 0 {
				return fmt.Errorf("%w: %v", agent.ErrMaxRetriesExceeded, err)
			}

			logger.Warn("connection failed, retrying",
				zap.Error(err),
				zap.Duration("retry_in", delay),
				zap.Int("attempt", backoff.Attempt()),
			)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}

		return nil
	}
}

func newLogger(level, format string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zap.DebugLevel
	case "info":
		zapLevel = zap.InfoLevel
	case "warn":
		zapLevel = zap.WarnLevel
	case "error":
		zapLevel = zap.ErrorLevel
	default:
		zapLevel = zap.InfoLevel
	}

	var zapCfg zap.Config
	if format == "console" {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(zapLevel)

	return zapCfg.Build()
}

// executeTask runs the Claude executor for a task and returns the completion message.
func executeTask(
	ctx context.Context,
	cmd *pb.ExecuteTask,
	cmdHandler *agent.DefaultCommandHandler,
	exec executor.Executor,
	logStreamer *agent.LogStreamer,
	logger *zap.Logger,
) (*pb.RunnerMessage, error) {
	// Get session state for workspace and agent config
	session, exists := cmdHandler.GetSession(cmd.SessionId)
	if !exists {
		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{
					TaskId:  cmd.TaskId,
					RunId:   cmd.RunId,
					Attempt: cmd.Attempt,
					Success: false,
					Error:   "session not attached",
				},
			},
		}, nil
	}

	// Get absolute workspace path
	workspacePath, ok := cmdHandler.GetWorkspacePath(cmd.SessionId)
	if !ok {
		workspacePath = session.WorkspacePath // Fallback
	}

	// Set log streamer context for this task
	logStreamer.SetTask(cmd.SessionId, cmd.TaskId, cmd.RunId)
	defer logStreamer.ClearTask()

	// Build executor task
	task := &executor.Task{
		ID:        cmd.TaskId,
		RunID:     cmd.RunId,
		SessionID: cmd.SessionId,
		Attempt:   cmd.Attempt,
		Prompt:    cmd.Prompt,
		Timeout:   0, // Use context timeout or default in executor
	}

	// Build agent config from session
	agentConfig := &executor.AgentConfig{
		WorkingDir: workspacePath,
	}
	if session.AgentConfig != nil {
		agentConfig.Agent = session.AgentConfig.Agent
		agentConfig.Model = session.AgentConfig.Model
		agentConfig.APIKey = session.AgentConfig.ApiKey
		agentConfig.BaseURL = session.AgentConfig.BaseUrl
		agentConfig.Extra = session.AgentConfig.Extra
	}

	logger.Info("executing task",
		zap.String("task_id", cmd.TaskId),
		zap.String("run_id", cmd.RunId),
		zap.String("agent", agentConfig.Agent),
		zap.String("model", agentConfig.Model),
		zap.String("working_dir", agentConfig.WorkingDir),
	)

	// Log system message for task start
	logStreamer.Log("info", "Task execution started", map[string]string{
		"task_id": cmd.TaskId,
		"run_id":  cmd.RunId,
		"prompt":  cmd.Prompt,
	})

	// Execute the task
	result, err := exec.Execute(ctx, task, agentConfig, logStreamer)
	if err != nil {
		logger.Error("executor error", zap.Error(err))
		logStreamer.Log("error", "Task execution failed: "+err.Error(), nil)

		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskCompleted{
				TaskCompleted: &pb.TaskCompleted{
					TaskId:  cmd.TaskId,
					RunId:   cmd.RunId,
					Attempt: cmd.Attempt,
					Success: false,
					Error:   err.Error(),
				},
			},
		}, nil
	}

	// Flush any remaining logs
	if err := logStreamer.Flush(ctx); err != nil {
		logger.Warn("failed to flush logs", zap.Error(err))
	}

	logStreamer.Log("info", "Task execution completed", map[string]string{
		"success":   fmt.Sprintf("%t", result.Success),
		"exit_code": fmt.Sprintf("%d", result.ExitCode),
	})

	logger.Info("task completed",
		zap.String("task_id", cmd.TaskId),
		zap.Bool("success", result.Success),
		zap.Int("exit_code", result.ExitCode),
	)

	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:          cmd.TaskId,
				RunId:           cmd.RunId,
				Attempt:         cmd.Attempt,
				Success:         result.Success,
				ExitCode:        int32(result.ExitCode),
				Error:           result.Error,
				TokensInput:     result.TokensInput,
				TokensOutput:    result.TokensOutput,
				ContextSnapshot: result.Context,
			},
		},
	}, nil
}

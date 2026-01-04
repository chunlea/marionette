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
	"github.com/chunlea/marionette/pkg/agent/executor/claude"
	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Parse command-line flags
	flags := pflag.NewFlagSet("agent", pflag.ExitOnError)
	configPath := flags.String("config", "", "path to config file")
	workspaceDir := flags.String("workspace", "/workspace", "Base directory for workspaces")
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
	workspaceMgr := agent.NewWorkspaceManager(*workspaceDir, logger)
	cmdHandler := agent.NewDefaultCommandHandler(workspaceMgr, logger)

	// Create control channel
	controlChannel := agent.NewControlChannel(client, cmdHandler, logger)

	// Create Claude executor
	claudeExec := claude.New()

	// Create task runner with control channel as sender
	taskRunner := agent.NewTaskRunner(claudeExec, controlChannel, workspaceMgr, logger)

	// Wire up task execution callback
	cmdHandler.OnExecuteTask = taskRunner.Execute

	// Wire up permission response callback
	cmdHandler.OnApprovePermission = func(ctx context.Context, cmd *pb.ApprovePermission) error {
		return taskRunner.HandlePermissionResponse(ctx, cmd)
	}

	logger.Info("task runner initialized",
		zap.String("executor", claudeExec.Name()),
	)

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

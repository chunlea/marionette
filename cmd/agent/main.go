// Package main provides the marionette-agent binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
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
	// The agent re-invokes itself as the CLI's PreToolUse permission hook.
	// This must be the very first thing main does: the hook runs per tool
	// call, has a JSON contract on stdin/stdout, and must not touch config,
	// logging or the network.
	if len(os.Args) > 1 && os.Args[1] == claude.PermissionHookCommand {
		os.Exit(runPermissionHook(os.Args[2:]))
	}

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
		zap.String("workspace", cfg.Workspace.BasePath),
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

	// Components below outlive individual connections. Anything that holds a
	// per-connection handle (the control channel, the gRPC client, the runner
	// ID) is reached through an indirection the supervisor re-points on every
	// reconnect, never captured once at construction.
	workspaceMgr := agent.NewWorkspaceManager(cfg.Workspace.BasePath, logger)
	cmdHandler := agent.NewDefaultCommandHandler(workspaceMgr, logger)

	// sender forwards to whichever control channel is currently live.
	sender := newCurrentChannel(logger)

	// Create Claude executor
	claudeExec := claude.New()

	tunnelMgr := agent.NewTunnelManager(
		agent.WithTMLogger(logger),
		agent.WithTMSender(sender),
	)

	// Create desktop stream manager
	desktopStreamMgr := agent.NewDesktopStreamManager(
		agent.DefaultDesktopStreamConfig(),
		logger,
	)

	// Heartbeat loop is created early so TaskRunner can update status. Its
	// stream is attached by the supervisor on every (re)connect.
	hbLoop := agent.NewHeartbeatLoop(client, cfg.Heartbeat, logger)

	// Log streamer is rebound by the supervisor on every (re)connect.
	logStreamer := agent.NewGRPCLogStreamer(nil, "", logger)

	// Create task runner (uses sender to send messages, hbLoop for status updates, logStreamer for logs)
	taskRunner := agent.NewTaskRunner(sender, claudeExec, workspaceMgr, cmdHandler, hbLoop, logStreamer, logger)

	// Wire up desktop stream manager (needs the control channel to send messages)
	desktopStreamMgr.SetSendFunc(func(msg *pb.RunnerMessage) error {
		sender.Send(msg)
		return nil
	})

	// Wire up callbacks
	cmdHandler.OnExecuteTask = taskRunner.Execute
	cmdHandler.OnApprovePermission = func(ctx context.Context, cmd *pb.ApprovePermission) error {
		return taskRunner.HandlePermissionResponse(ctx, cmd)
	}
	cmdHandler.OnDetachSession = taskRunner.CancelTask
	cmdHandler.OnCreateTunnel = tunnelMgr.HandleCreateTunnel
	cmdHandler.OnTunnelData = tunnelMgr.HandleTunnelData
	cmdHandler.OnStartDesktopStream = desktopStreamMgr.HandleStartDesktopStream
	cmdHandler.OnStopDesktopStream = desktopStreamMgr.HandleStopDesktopStream

	// Start heartbeat loop. It survives reconnects; only its stream changes.
	hbLoop.Start(ctx)

	// Own the connection for the rest of the process lifetime. Returns only
	// when ctx is canceled.
	superviseConnection(ctx, connectionDeps{
		client:      client,
		handler:     cmdHandler,
		sender:      sender,
		hbLoop:      hbLoop,
		logStreamer: logStreamer,
		logger:      logger,
	})

	// Graceful shutdown
	logger.Info("initiating graceful shutdown")

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

// runPermissionHook answers one PreToolUse permission question and returns the
// process exit code. It always writes a decision: a hook that exits without
// one makes the CLI fail open and run the tool unapproved.
func runPermissionHook(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: marionette-agent "+claude.PermissionHookCommand+" <socket-path>")
		return 2
	}

	if err := claude.RunPermissionHook(context.Background(), args[0], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "permission hook failed: %v\n", err)
		return 1
	}
	return 0
}

// currentChannel forwards messages to whichever control channel is live.
//
// The control channel is single-use - its stop channels never reopen - so a
// reconnect builds a new one. Long-lived components hold this indirection
// instead of a channel that a reconnect would turn into a dead reference.
type currentChannel struct {
	mu     sync.RWMutex
	cc     *agent.ControlChannel
	logger *zap.Logger
}

func newCurrentChannel(logger *zap.Logger) *currentChannel {
	return &currentChannel{logger: logger.Named("sender")}
}

func (s *currentChannel) set(cc *agent.ControlChannel) {
	s.mu.Lock()
	s.cc = cc
	s.mu.Unlock()
}

// Send implements agent.MessageSender.
func (s *currentChannel) Send(msg *pb.RunnerMessage) {
	s.mu.RLock()
	cc := s.cc
	s.mu.RUnlock()

	if cc == nil {
		s.logger.Warn("dropping message: no control channel is connected")
		return
	}
	cc.Send(msg)
}

var _ agent.MessageSender = (*currentChannel)(nil)

// errControlChannelClosed reports that the control channel ended, which
// includes a clean server-side EOF. A server restart looks exactly like this,
// and it is a reason to reconnect, not to give up.
var errControlChannelClosed = errors.New("control channel closed")

// connectionDeps are the long-lived components a connection has to re-point.
type connectionDeps struct {
	client      *agent.Client
	handler     agent.CommandHandler
	sender      *currentChannel
	hbLoop      *agent.HeartbeatLoop
	logStreamer *agent.GRPCLogStreamer
	logger      *zap.Logger

	// backoff paces reconnect attempts. Nil means the production schedule.
	backoff *agent.ExponentialBackoff
}

// superviseConnection owns the connection for the whole process lifetime:
// connect, register, control channel, heartbeat stream, log streamer, and on
// any exit tear the whole thing down and try again.
//
// Previously the agent connected exactly once and the receive loop returned on
// any stream error, clean EOF included, with nothing to restart it: one server
// restart bricked the agent until an operator noticed.
//
// Returns only when ctx is canceled.
func superviseConnection(ctx context.Context, deps connectionDeps) {
	backoff := deps.backoff
	if backoff == nil {
		backoff = agent.NewExponentialBackoff(agent.BackoffConfig{
			InitialDelay: 1 * time.Second,
			MaxDelay:     60 * time.Second,
			Multiplier:   2.0,
			Jitter:       0.2,
			MaxRetries:   -1, // Unlimited retries
		})
	}

	for {
		if ctx.Err() != nil {
			return
		}

		err := runConnection(ctx, deps, backoff.Reset)
		if ctx.Err() != nil {
			return
		}

		delay := backoff.Next()
		deps.logger.Warn("connection lost, reconnecting",
			zap.Error(err),
			zap.Duration("retry_in", delay),
			zap.Int("attempt", backoff.Attempt()),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// runConnection brings up one connection and blocks until it ends. The
// returned error always describes why; a nil return is not possible.
//
// onRegistered fires once registration succeeds, which is the only evidence
// that the server is actually healthy and so the only correct point to reset
// the backoff.
func runConnection(ctx context.Context, deps connectionDeps, onRegistered func()) error {
	if err := deps.client.Connect(ctx); err != nil {
		return err
	}

	defer func() {
		deps.sender.set(nil)
		deps.hbLoop.SetStream(nil)
		deps.logStreamer.Rebind(nil, "")
		if err := deps.client.Disconnect(); err != nil {
			deps.logger.Warn("error disconnecting", zap.Error(err))
		}
	}()

	onRegistered()
	deps.logger.Info("connected to server", zap.String("runner_id", deps.client.RunnerID()))

	// Re-point everything that holds a per-connection handle.
	deps.logStreamer.Rebind(deps.client.GRPCClient(), deps.client.RunnerID())

	controlChannel := agent.NewControlChannel(deps.client, deps.handler, deps.logger)
	if err := controlChannel.Start(ctx); err != nil {
		return fmt.Errorf("starting control channel: %w", err)
	}
	defer controlChannel.Stop()

	deps.sender.set(controlChannel)

	// The heartbeat stream is the control stream. Without this the heartbeat
	// loop had no stream and silently dropped every beat it produced.
	deps.hbLoop.SetStream(controlChannel.Stream())

	deps.logger.Info("control channel started")

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		controlChannel.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-closed:
		return errControlChannelClosed
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

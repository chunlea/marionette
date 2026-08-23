package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent"
	"github.com/chunlea/marionette/test/perf/fakeexec"
	"go.uber.org/zap"
)

// fakeRunner is a pool runner built from the same exported pkg/agent pieces
// cmd/agent wires, with the Claude executor swapped for a scripted one.
//
// Everything downstream of the executor is the production path: real gRPC
// registration, the real control channel, the real command dispatcher, the real
// TaskRunner, the real log stream. That is the point - a load test against a
// mock server measures the mock.
type fakeRunner struct {
	name         string
	workspaceDir string
	logger       *zap.Logger

	client       *agent.Client
	control      *agent.ControlChannel
	heartbeat    *agent.HeartbeatLoop
	logStreamer  *agent.GRPCLogStreamer
	exec         *fakeexec.Executor
	sender       *currentChannel
	shuttingDown chan struct{}
}

// currentChannel is the indirection long-lived components hold instead of a
// control channel a reconnect would turn into a dead reference. cmd/agent has
// the same type; it is unexported there, and the load test needs its own.
type currentChannel struct {
	mu     sync.RWMutex
	cc     *agent.ControlChannel
	logger *zap.Logger
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

// newFakeRunner builds a runner. It does not connect: Start does that, so a
// failure to reach the server is reported per runner rather than at
// construction.
func newFakeRunner(
	name, serverAddr, token, poolName, workspaceDir string,
	execCfg fakeexec.Config,
	logger *zap.Logger,
) *fakeRunner {
	named := logger.Named(name)

	cfg := &agent.Config{
		Server: agent.ServerConfig{Address: serverAddr},
		Runner: agent.RunnerConfig{
			Token:    token,
			Name:     name,
			PoolName: poolName,
		},
		Workspace: agent.WorkspaceConfig{BasePath: workspaceDir},
		Heartbeat: agent.HeartbeatConfig{
			Interval: 10 * time.Second,
			Timeout:  10 * time.Second,
		},
		// "none" keeps the runner out of the host's sandbox machinery: the
		// load test measures the server, and a per-task sandbox would measure
		// the load generator's kernel instead.
		Sandbox: agent.SandboxConfig{Mode: "none"},
		Storage: agent.StorageConfig{Backend: agent.StorageBackendNone},
	}

	client := agent.NewClient(cfg, named)
	return &fakeRunner{
		name:         name,
		workspaceDir: workspaceDir,
		logger:       named,
		client:       client,
		heartbeat:    agent.NewHeartbeatLoop(client, cfg.Heartbeat, named),
		logStreamer:  agent.NewGRPCLogStreamer(nil, "", named),
		exec:         fakeexec.New(execCfg),
		sender:       &currentChannel{logger: named.Named("sender")},
		shuttingDown: make(chan struct{}),
	}
}

// Start connects, registers and serves commands until ctx ends.
//
// There is deliberately no reconnect loop. cmd/agent has one because a
// production agent must survive a server restart; here a dropped connection is
// a result, not something to paper over, and a silent reconnect would turn a
// server that fell over under load into a run that merely looked slow.
func (r *fakeRunner) Start(ctx context.Context) error {
	if err := r.client.Connect(ctx); err != nil {
		return fmt.Errorf("%s: connect: %w", r.name, err)
	}

	r.logStreamer.Rebind(r.client.GRPCClient(), r.client.RunnerID())

	workspaceMgr := agent.NewWorkspaceManager(r.workspaceDir, r.logger)
	cmdHandler := agent.NewDefaultCommandHandler(workspaceMgr, r.logger)

	taskRunner := agent.NewTaskRunner(
		r.sender, r.exec, workspaceMgr, cmdHandler, r.heartbeat, r.logStreamer, r.logger)

	cmdHandler.OnExecuteTask = taskRunner.Execute
	cmdHandler.OnApprovePermission = func(ctx context.Context, cmd *pb.ApprovePermission) error {
		return taskRunner.HandlePermissionResponse(ctx, cmd)
	}
	cmdHandler.OnDetachSession = taskRunner.CancelTask

	control := agent.NewControlChannel(r.client, cmdHandler, r.logger)
	if err := control.Start(ctx); err != nil {
		return fmt.Errorf("%s: control channel: %w", r.name, err)
	}
	r.control = control
	r.sender.set(control)
	r.heartbeat.SetStream(control.Stream())
	r.heartbeat.Start(ctx)

	go func() {
		control.Wait()
		select {
		case <-r.shuttingDown:
		case <-ctx.Done():
		default:
			r.logger.Warn("control channel ended before shutdown")
		}
	}()

	return nil
}

// RunnerID is the id the server assigned at registration.
func (r *fakeRunner) RunnerID() string { return r.client.RunnerID() }

// Executed reports how many tasks this runner actually ran, which is the
// harness's independent check that the server's completion count is real.
func (r *fakeRunner) Executed() int { return r.exec.Executed() }

// Stop tears the runner down.
func (r *fakeRunner) Stop() {
	close(r.shuttingDown)
	r.heartbeat.Stop()
	if r.control != nil {
		r.control.Stop()
	}
	if _, err := r.logStreamer.Close(); err != nil {
		r.logger.Debug("log streamer close", zap.Error(err))
	}
	if err := r.client.Close(); err != nil {
		r.logger.Debug("client close", zap.Error(err))
	}
}

// runnerWorkspace is where runner n keeps its workspaces.
func runnerWorkspace(base string, n int) string {
	return filepath.Join(base, fmt.Sprintf("runner-%03d", n))
}

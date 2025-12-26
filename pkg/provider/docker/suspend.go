package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/chunlea/marionette/pkg/provider"
)

// Compile-time interface check.
var _ provider.SuspendableProvider = (*Provider)(nil)

// Suspend suspends the runner using the configured or specified strategy.
func (p *Provider) Suspend(ctx context.Context, runnerID string, opts provider.SuspendOptions) (*provider.SuspendResult, error) {
	// Determine strategy to use.
	strategy := opts.Strategy
	if strategy == "" {
		strategy = p.suspendConfig.Strategy
	}

	// Validate strategy is supported.
	if !p.supportsStrategy(strategy) {
		return nil, &provider.ErrStrategyNotSupported{
			Strategy: strategy,
			Provider: p.name,
		}
	}

	// Execute suspend based on strategy.
	var err error
	switch strategy {
	case provider.SuspendStrategyPause:
		err = p.suspendWithPause(ctx, runnerID)
	case provider.SuspendStrategyTerminatePreserveStorage:
		err = p.suspendWithTerminatePreserveStorage(ctx, runnerID)
	case provider.SuspendStrategyTerminate:
		err = p.suspendWithTerminate(ctx, runnerID)
	default:
		// Try fallback if available.
		if p.suspendConfig.Fallback != "" && p.supportsStrategy(p.suspendConfig.Fallback) {
			return p.Suspend(ctx, runnerID, provider.SuspendOptions{
				Strategy:      p.suspendConfig.Fallback,
				SaveSnapshot:  opts.SaveSnapshot,
				SyncWorkspace: opts.SyncWorkspace,
				Timeout:       opts.Timeout,
			})
		}
		return nil, &provider.ErrStrategyNotSupported{
			Strategy: strategy,
			Provider: p.name,
		}
	}

	if err != nil {
		// Try fallback if primary strategy fails.
		if p.suspendConfig.Fallback != "" && p.suspendConfig.Fallback != strategy {
			return p.Suspend(ctx, runnerID, provider.SuspendOptions{
				Strategy:      p.suspendConfig.Fallback,
				SaveSnapshot:  opts.SaveSnapshot,
				SyncWorkspace: opts.SyncWorkspace,
				Timeout:       opts.Timeout,
			})
		}
		return nil, &provider.ErrSuspendFailed{
			RunnerID: runnerID,
			Strategy: strategy,
			Cause:    err,
		}
	}

	return &provider.SuspendResult{
		Strategy:        strategy,
		WorkspaceSynced: opts.SyncWorkspace, // TODO: implement actual sync
		SuspendedAt:     time.Now(),
	}, nil
}

// Resume restores a suspended runner.
func (p *Provider) Resume(ctx context.Context, sessionID string, opts provider.ResumeOptions) (*provider.RunnerInstance, error) {
	if opts.RunnerID == "" {
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("runner ID is required for resume"),
		}
	}

	// Get current runner status.
	status, err := p.Status(ctx, opts.RunnerID)
	if err != nil {
		// Runner not found - need to spawn new one.
		if _, ok := err.(*provider.ErrRunnerNotFound); ok {
			return p.resumeWithNewContainer(ctx, sessionID, opts)
		}
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     err,
		}
	}

	// Resume based on current state.
	switch status.Status {
	case provider.InstanceStatusPaused:
		// Unpause the container.
		if err := p.Unpause(ctx, opts.RunnerID); err != nil {
			return nil, &provider.ErrResumeFailed{
				SessionID: sessionID,
				Cause:     err,
			}
		}
		return &provider.RunnerInstance{
			ID:     opts.RunnerID,
			Status: provider.InstanceStatusRunning,
		}, nil

	case provider.InstanceStatusStopped:
		// Container exists but stopped - start it.
		containerID, err := p.findContainerByRunnerID(ctx, opts.RunnerID)
		if err != nil {
			return nil, &provider.ErrResumeFailed{
				SessionID: sessionID,
				Cause:     err,
			}
		}
		if err := p.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
			return nil, &provider.ErrResumeFailed{
				SessionID: sessionID,
				Cause:     fmt.Errorf("starting container: %w", err),
			}
		}
		return &provider.RunnerInstance{
			ID:     opts.RunnerID,
			Status: provider.InstanceStatusRunning,
		}, nil

	case provider.InstanceStatusRunning:
		// Already running - nothing to do.
		return &provider.RunnerInstance{
			ID:     opts.RunnerID,
			Status: provider.InstanceStatusRunning,
		}, nil

	default:
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("cannot resume from status: %s", status.Status),
		}
	}
}

// suspendWithPause uses docker pause to suspend the runner.
func (p *Provider) suspendWithPause(ctx context.Context, runnerID string) error {
	return p.Pause(ctx, runnerID)
}

// suspendWithTerminatePreserveStorage stops the container but keeps volumes.
func (p *Provider) suspendWithTerminatePreserveStorage(ctx context.Context, runnerID string) error {
	containerID, err := p.findContainerByRunnerID(ctx, runnerID)
	if err != nil {
		return err
	}

	// Stop with timeout.
	timeout := defaultStopTimeout
	if err := p.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		if !isNotRunningError(err) {
			return fmt.Errorf("stopping container: %w", err)
		}
	}

	return nil
}

// suspendWithTerminate fully destroys the runner.
func (p *Provider) suspendWithTerminate(ctx context.Context, runnerID string) error {
	return p.Destroy(ctx, runnerID)
}

// resumeWithNewContainer spawns a new container for resume.
func (p *Provider) resumeWithNewContainer(ctx context.Context, sessionID string, opts provider.ResumeOptions) (*provider.RunnerInstance, error) {
	if opts.SpawnOpts == nil {
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("spawn options required for creating new container"),
		}
	}

	return p.Spawn(ctx, *opts.SpawnOpts)
}

// supportsStrategy checks if a strategy is supported by this provider.
func (p *Provider) supportsStrategy(strategy provider.SuspendStrategy) bool {
	caps := p.Capabilities()
	for _, s := range caps.Suspend.Strategies {
		if s == strategy {
			return true
		}
	}
	return false
}

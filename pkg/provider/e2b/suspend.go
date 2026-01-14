package e2b

import (
	"context"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
)

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

	// Try to find the sandbox by runner ID
	sandboxID, err := p.findSandboxByRunnerID(ctx, opts.RunnerID)
	if err != nil {
		if IsNotFoundError(err) {
			// Sandbox doesn't exist - need to spawn new one
			return p.resumeWithNewSandbox(ctx, sessionID, opts)
		}
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     err,
		}
	}

	// Get current status
	sandbox, err := p.client.GetSandbox(ctx, sandboxID)
	if err != nil {
		if IsNotFoundError(err) {
			return p.resumeWithNewSandbox(ctx, sessionID, opts)
		}
		if IsPausedError(err) {
			// Sandbox is paused - resume it
			if _, err := p.client.ResumeSandbox(ctx, sandboxID); err != nil {
				return nil, &provider.ErrResumeFailed{
					SessionID: sessionID,
					Cause:     fmt.Errorf("resuming sandbox: %w", err),
				}
			}
			return &provider.RunnerInstance{
				ID:         opts.RunnerID,
				ProviderID: sandboxID,
				Status:     provider.InstanceStatusRunning,
			}, nil
		}
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     err,
		}
	}

	// Check if sandbox is still running
	if sandbox.EndedAt != nil {
		// Sandbox was terminated - need to spawn new one
		return p.resumeWithNewSandbox(ctx, sessionID, opts)
	}

	// Try to resume (in case it's paused)
	if _, err := p.client.ResumeSandbox(ctx, sandboxID); err != nil {
		// If resume fails but sandbox is running, that's OK
		if !IsPausedError(err) {
			// Sandbox might already be running
			return &provider.RunnerInstance{
				ID:         opts.RunnerID,
				ProviderID: sandboxID,
				Status:     provider.InstanceStatusRunning,
				Metadata: map[string]string{
					"sandbox_id":  sandbox.SandboxID,
					"template_id": sandbox.TemplateID,
				},
			}, nil
		}
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("resuming sandbox: %w", err),
		}
	}

	return &provider.RunnerInstance{
		ID:         opts.RunnerID,
		ProviderID: sandboxID,
		Status:     provider.InstanceStatusRunning,
		Metadata: map[string]string{
			"sandbox_id":  sandbox.SandboxID,
			"template_id": sandbox.TemplateID,
		},
	}, nil
}

// suspendWithPause uses E2B's native pause (beta) to suspend the sandbox.
func (p *Provider) suspendWithPause(ctx context.Context, runnerID string) error {
	return p.Pause(ctx, runnerID)
}

// suspendWithTerminate kills the sandbox.
func (p *Provider) suspendWithTerminate(ctx context.Context, runnerID string) error {
	return p.Destroy(ctx, runnerID)
}

// resumeWithNewSandbox spawns a new sandbox for resume.
func (p *Provider) resumeWithNewSandbox(ctx context.Context, sessionID string, opts provider.ResumeOptions) (*provider.RunnerInstance, error) {
	if opts.SpawnOpts == nil {
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("spawn options required for creating new sandbox"),
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

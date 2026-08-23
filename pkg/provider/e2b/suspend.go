package e2b

import (
	"context"
	"fmt"

	"github.com/chunlea/marionette/pkg/provider"
)

// suspendDispatcher maps this provider's supported strategies to their
// implementations.
func (p *Provider) suspendDispatcher() provider.SuspendDispatcher {
	return provider.SuspendDispatcher{
		Provider: p.name,
		Config:   *p.suspendConfig,
		Handlers: map[provider.SuspendStrategy]provider.SuspendFunc{
			provider.SuspendStrategyPause:     p.suspendWithPause,
			provider.SuspendStrategyTerminate: p.suspendWithTerminate,
		},
	}
}

// Suspend suspends the runner using the configured or specified strategy.
func (p *Provider) Suspend(ctx context.Context, runnerID string, opts provider.SuspendOptions) (*provider.SuspendResult, error) {
	return p.suspendDispatcher().Suspend(ctx, runnerID, opts)
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
			if _, err := p.client.ResumeSandbox(ctx, sandboxID, p.config.TimeoutSeconds); err != nil {
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
	if _, err := p.client.ResumeSandbox(ctx, sandboxID, p.config.TimeoutSeconds); err != nil {
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

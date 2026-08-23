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

	sandboxID, err := p.findSandboxByRunnerID(ctx, opts.RunnerID, opts.ProviderInstanceID)
	if err != nil {
		if IsNotFoundError(err) {
			// Nothing known about this runner at all: no persisted instance
			// id, nothing cached, nothing in the list. Start over.
			return p.resumeWithNewSandbox(ctx, sessionID, opts)
		}
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     err,
		}
	}

	return p.resumeSandbox(ctx, sessionID, sandboxID, opts)
}

// resumeSandbox brings one known sandbox back.
//
// It calls resume directly instead of reading the sandbox first. A paused E2B
// sandbox is not readable - it is absent from the list and GET answers 404 -
// and the previous version took that 404 as "gone" and spawned a replacement,
// leaking the very sandbox it had been asked to resume. Resume is the only call
// that can tell the three states apart: 201 resumed, 409 already running, 404
// genuinely gone.
func (p *Provider) resumeSandbox(
	ctx context.Context,
	sessionID, sandboxID string,
	opts provider.ResumeOptions,
) (*provider.RunnerInstance, error) {
	sandbox, err := p.client.ResumeSandbox(ctx, sandboxID, p.config.TimeoutSeconds)
	switch {
	case err == nil:
		p.sandboxCache.Store(opts.RunnerID, sandboxID)
		instance := &provider.RunnerInstance{
			ID:         opts.RunnerID,
			ProviderID: sandboxID,
			Status:     provider.InstanceStatusRunning,
			Metadata:   map[string]string{"sandbox_id": sandboxID},
		}
		if sandbox != nil && sandbox.TemplateID != "" {
			instance.Metadata["template_id"] = sandbox.TemplateID
		}
		return instance, nil

	case IsConflictError(err):
		// Already running. Resuming a running sandbox is a no-op, not a
		// failure: the caller asked for a usable runner and there is one.
		p.sandboxCache.Store(opts.RunnerID, sandboxID)
		return &provider.RunnerInstance{
			ID:         opts.RunnerID,
			ProviderID: sandboxID,
			Status:     provider.InstanceStatusRunning,
			Metadata:   map[string]string{"sandbox_id": sandboxID},
		}, nil

	case IsNotFoundError(err):
		// The sandbox really is gone (killed, or expired past E2B's pause
		// retention). Only now is spawning a replacement correct.
		p.sandboxCache.Delete(opts.RunnerID)
		return p.resumeWithNewSandbox(ctx, sessionID, opts)

	default:
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("resuming sandbox %s: %w", sandboxID, err),
		}
	}
}

// suspendWithPause uses E2B's native pause (beta) to suspend the sandbox.
func (p *Provider) suspendWithPause(ctx context.Context, runnerID string, opts provider.SuspendOptions) error {
	return p.pause(ctx, runnerID, opts.ProviderInstanceID)
}

// suspendWithTerminate kills the sandbox.
func (p *Provider) suspendWithTerminate(ctx context.Context, runnerID string, opts provider.SuspendOptions) error {
	return p.Destroy(ctx, runnerID, provider.DestroyOptions{
		ProviderInstanceID: opts.ProviderInstanceID,
	})
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

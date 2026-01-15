package kubernetes

import (
	"context"
	"fmt"
	"slices"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/chunlea/marionette/pkg/provider"
)

// Suspend suspends the runner using the configured or specified strategy.
// Kubernetes only supports terminate_preserve_storage (delete Pod, keep PVC)
// and terminate (delete Pod and PVC).
func (p *Provider) Suspend(ctx context.Context, runnerID string, opts provider.SuspendOptions) (*provider.SuspendResult, error) {
	// Determine strategy to use
	strategy := opts.Strategy
	if strategy == "" {
		strategy = p.suspendConfig.Strategy
	}

	// Validate strategy is supported
	if !p.supportsStrategy(strategy) {
		return nil, &provider.ErrStrategyNotSupported{
			Strategy: strategy,
			Provider: p.name,
		}
	}

	// Execute suspend based on strategy
	var err error
	switch strategy {
	case provider.SuspendStrategyTerminatePreserveStorage:
		err = p.suspendWithTerminatePreserveStorage(ctx, runnerID)
	case provider.SuspendStrategyTerminate:
		err = p.suspendWithTerminate(ctx, runnerID)
	default:
		// Try fallback if available
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
		// Try fallback if primary strategy fails
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
		WorkspaceSynced: opts.SyncWorkspace, // TODO: implement actual sync to CAS
		SuspendedAt:     time.Now(),
	}, nil
}

// Resume restores a suspended runner by creating a new Pod with the existing PVC.
func (p *Provider) Resume(ctx context.Context, sessionID string, opts provider.ResumeOptions) (*provider.RunnerInstance, error) {
	if opts.RunnerID == "" {
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("runner ID is required for resume"),
		}
	}

	// Check if pod still exists
	podName, err := p.findPodNameByRunnerID(ctx, opts.RunnerID)
	if err == nil {
		// Pod still exists, check its status
		status, err := p.Status(ctx, opts.RunnerID)
		if err != nil {
			return nil, &provider.ErrResumeFailed{
				SessionID: sessionID,
				Cause:     err,
			}
		}

		switch status.Status {
		case provider.InstanceStatusRunning:
			// Already running, nothing to do
			return &provider.RunnerInstance{
				ID:     opts.RunnerID,
				Status: provider.InstanceStatusRunning,
				Metadata: map[string]string{
					"pod_name": podName,
				},
			}, nil

		case provider.InstanceStatusPending:
			// Still pending, wait or return current state
			return &provider.RunnerInstance{
				ID:     opts.RunnerID,
				Status: provider.InstanceStatusPending,
				Metadata: map[string]string{
					"pod_name": podName,
				},
			}, nil

		case provider.InstanceStatusFailed, provider.InstanceStatusStopped:
			// Pod exists but in terminal state, delete it and recreate
			if err := p.client.DeletePod(ctx, p.config.Namespace, podName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
				return nil, &provider.ErrResumeFailed{
					SessionID: sessionID,
					Cause:     fmt.Errorf("deleting failed pod: %w", err),
				}
			}
			// Fall through to recreate
		}
	}

	// Pod doesn't exist or was deleted, verify PVC exists and spawn new pod
	pvcName := p.pvcName(opts.RunnerID)
	_, err = p.client.GetPVC(ctx, p.config.Namespace, pvcName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// PVC doesn't exist, need spawn options to create it
			if opts.SpawnOpts == nil {
				return nil, &provider.ErrResumeFailed{
					SessionID: sessionID,
					Cause:     fmt.Errorf("PVC not found and no spawn options provided"),
				}
			}
		} else {
			return nil, &provider.ErrResumeFailed{
				SessionID: sessionID,
				Cause:     fmt.Errorf("checking PVC: %w", err),
			}
		}
	}

	// Need spawn options to create new pod
	if opts.SpawnOpts == nil {
		return nil, &provider.ErrResumeFailed{
			SessionID: sessionID,
			Cause:     fmt.Errorf("spawn options required for creating new pod"),
		}
	}

	// Spawn will reuse existing PVC
	return p.Spawn(ctx, *opts.SpawnOpts)
}

// suspendWithTerminatePreserveStorage deletes the pod but keeps the PVC.
func (p *Provider) suspendWithTerminatePreserveStorage(ctx context.Context, runnerID string) error {
	return p.destroyPod(ctx, runnerID, false)
}

// suspendWithTerminate deletes both the pod and the PVC.
func (p *Provider) suspendWithTerminate(ctx context.Context, runnerID string) error {
	return p.destroyPod(ctx, runnerID, true)
}

// supportsStrategy checks if a strategy is supported by this provider.
func (p *Provider) supportsStrategy(strategy provider.SuspendStrategy) bool {
	return slices.Contains(p.Capabilities().Suspend.Strategies, strategy)
}

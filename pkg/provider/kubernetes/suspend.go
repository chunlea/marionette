package kubernetes

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/chunlea/marionette/pkg/provider"
)

// suspendDispatcher maps this provider's supported strategies to their
// implementations. Kubernetes cannot pause a pod, so there is no pause handler.
func (p *Provider) suspendDispatcher() provider.SuspendDispatcher {
	return provider.SuspendDispatcher{
		Provider: p.name,
		Config:   *p.suspendConfig,
		Handlers: map[provider.SuspendStrategy]provider.SuspendFunc{
			provider.SuspendStrategyTerminatePreserveStorage: p.suspendWithTerminatePreserveStorage,
			provider.SuspendStrategyTerminate:                p.suspendWithTerminate,
		},
	}
}

// Suspend suspends the runner using the configured or specified strategy.
// Kubernetes only supports terminate_preserve_storage (delete Pod, keep PVC)
// and terminate (delete Pod and PVC).
func (p *Provider) Suspend(ctx context.Context, runnerID string, opts provider.SuspendOptions) (*provider.SuspendResult, error) {
	return p.suspendDispatcher().Suspend(ctx, runnerID, opts)
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

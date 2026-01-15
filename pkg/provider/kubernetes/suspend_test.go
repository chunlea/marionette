package kubernetes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/chunlea/marionette/pkg/provider"
)

func TestSuspend(t *testing.T) {
	ctx := context.Background()

	t.Run("terminate_preserve_storage", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add pod to suspend
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-suspend",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_suspend",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		})

		result, err := p.Suspend(ctx, "run_suspend", provider.SuspendOptions{
			Strategy: provider.SuspendStrategyTerminatePreserveStorage,
		})

		require.NoError(t, err)
		assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, result.Strategy)
		assert.NotZero(t, result.SuspendedAt)

		// Pod should be deleted
		assert.Nil(t, client.GetStoredPod("test-ns", "marionette-run-suspend"))
	})

	t.Run("terminate with pvc deletion", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add pod and PVC
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-term",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_term",
				},
			},
		})
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-term",
				Namespace: "test-ns",
			},
		})

		result, err := p.Suspend(ctx, "run_term", provider.SuspendOptions{
			Strategy: provider.SuspendStrategyTerminate,
		})

		require.NoError(t, err)
		assert.Equal(t, provider.SuspendStrategyTerminate, result.Strategy)

		// Both pod and PVC should be deleted
		assert.Nil(t, client.GetStoredPod("test-ns", "marionette-run-term"))
		assert.Nil(t, client.GetStoredPVC("test-ns", "marionette-ws-run-term"))
	})

	t.Run("default strategy", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-default",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_default",
				},
			},
		})

		// Empty strategy should use default (terminate_preserve_storage)
		result, err := p.Suspend(ctx, "run_default", provider.SuspendOptions{})

		require.NoError(t, err)
		assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, result.Strategy)
	})

	t.Run("unsupported strategy falls back", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-pause",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_pause",
				},
			},
		})

		// Pause is not supported, should fail
		_, err := p.Suspend(ctx, "run_pause", provider.SuspendOptions{
			Strategy: provider.SuspendStrategyPause,
		})

		require.Error(t, err)
		var strategyErr *provider.ErrStrategyNotSupported
		require.ErrorAs(t, err, &strategyErr)
	})

	t.Run("runner not found", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		_, err := p.Suspend(ctx, "nonexistent", provider.SuspendOptions{})

		require.Error(t, err)
		var suspendErr *provider.ErrSuspendFailed
		require.ErrorAs(t, err, &suspendErr)
	})
}

func TestResume(t *testing.T) {
	ctx := context.Background()

	t.Run("pod still running", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Pod is still running
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-running",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_running",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		})

		instance, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_running",
		})

		require.NoError(t, err)
		assert.Equal(t, "run_running", instance.ID)
		assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
	})

	t.Run("pod pending", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-pending",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_pending",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		})

		instance, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_pending",
		})

		require.NoError(t, err)
		assert.Equal(t, provider.InstanceStatusPending, instance.Status)
	})

	t.Run("recreate from failed pod with spawn opts", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add failed pod
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-failed",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_failed",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodFailed},
		})

		// Add existing PVC
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-failed",
				Namespace: "test-ns",
			},
		})

		// Set new pod to running after creation
		go func() {
			for {
				if client.GetStoredPod("test-ns", "marionette-run-failed-2") != nil {
					client.SetPodPhase("test-ns", "marionette-run-failed-2", corev1.PodRunning)
					break
				}
			}
		}()

		instance, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_failed",
			SpawnOpts: &provider.SpawnOptions{
				RunnerID:    "run_failed",
				Name:        "run-failed-2",
				ServerURL:   "grpc://server:9090",
				RunnerToken: "token",
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, instance)
	})

	t.Run("no runner id", func(t *testing.T) {
		client := NewMockKubeClient()
		p := newTestProvider(client)

		_, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{})

		require.Error(t, err)
		var resumeErr *provider.ErrResumeFailed
		require.ErrorAs(t, err, &resumeErr)
		assert.Contains(t, resumeErr.Error(), "runner ID is required")
	})

	t.Run("pod deleted, PVC exists, no spawn opts", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Only PVC exists
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-deleted",
				Namespace: "test-ns",
			},
		})

		_, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_deleted",
		})

		require.Error(t, err)
		var resumeErr *provider.ErrResumeFailed
		require.ErrorAs(t, err, &resumeErr)
		assert.Contains(t, resumeErr.Error(), "spawn options required")
	})

	t.Run("no pvc no spawn opts", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		_, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_nopvc",
		})

		require.Error(t, err)
		var resumeErr *provider.ErrResumeFailed
		require.ErrorAs(t, err, &resumeErr)
	})
}

func TestSupportsStrategy(t *testing.T) {
	client := NewMockKubeClient()
	p := newTestProvider(client)

	supported := []provider.SuspendStrategy{
		provider.SuspendStrategyTerminatePreserveStorage,
		provider.SuspendStrategyTerminate,
	}

	unsupported := []provider.SuspendStrategy{
		provider.SuspendStrategyPause,
		provider.SuspendStrategySnapshot,
		provider.SuspendStrategyReleaseToPool,
	}

	for _, s := range supported {
		t.Run("supported:"+string(s), func(t *testing.T) {
			assert.True(t, p.supportsStrategy(s))
		})
	}

	for _, s := range unsupported {
		t.Run("unsupported:"+string(s), func(t *testing.T) {
			assert.False(t, p.supportsStrategy(s))
		})
	}
}

func TestSuspendConfig(t *testing.T) {
	client := NewMockKubeClient()
	p := newTestProvider(client)

	cfg := p.SuspendConfig()
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Strategy)
	assert.Equal(t, provider.SuspendStrategyTerminate, cfg.Fallback)
}

//go:build integration

package kubernetes

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// Run with: go test -tags=integration -v ./pkg/provider/kubernetes/... -run TestIntegration
//
// Prerequisites:
//   - kubectl configured with cluster access
//   - Namespace "marionette-test" created or cluster-admin access
//
// Setup:
//   kind create cluster --name marionette-test
//   kubectl create namespace marionette-test

func TestIntegrationKubernetesProvider(t *testing.T) {
	if os.Getenv("KUBECONFIG") == "" && os.Getenv("HOME") != "" {
		// Try default kubeconfig location
		defaultConfig := os.Getenv("HOME") + "/.kube/config"
		if _, err := os.Stat(defaultConfig); err != nil {
			t.Skip("No kubeconfig found, skipping integration test")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create provider config
	providerCfg := &store.ProviderConfig{
		ID:       "pcfg_integ_test",
		Name:     "k8s-integration-test",
		Provider: "kubernetes",
		Config: []byte(`{
			"namespace": "marionette-test",
			"image": "alpine:latest",
			"cmd": ["sleep", "300"],
			"label_prefix": "marionette.dev",
			"restart_policy": "Never",
			"resources": {
				"memory": "128Mi",
				"cpus": "100m",
				"memory_request": "64Mi",
				"cpu_request": "50m"
			},
			"storage": {
				"size": "100Mi",
				"access_mode": "ReadWriteOnce"
			}
		}`),
		SuspendConfig: []byte(`{
			"strategy": "terminate_preserve_storage"
		}`),
	}

	p, err := New(providerCfg)
	if err != nil {
		t.Skipf("Failed to create provider (no cluster access?): %v", err)
	}

	runnerID := "run_integ_" + time.Now().Format("150405")
	runnerName := "integ-test-runner"

	// Test 1: Spawn
	t.Run("Spawn", func(t *testing.T) {
		instance, err := p.Spawn(ctx, provider.SpawnOptions{
			RunnerID: runnerID,
			Name:     runnerName,
			Environment: map[string]string{
				"TEST_VAR": "hello",
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, instance)
		assert.Equal(t, runnerID, instance.ID)
		t.Logf("Spawned runner: %s", instance.ID)
	})

	// Test 2: Status
	t.Run("Status", func(t *testing.T) {
		status, err := p.Status(ctx, runnerID)
		require.NoError(t, err)
		assert.NotNil(t, status)
		t.Logf("Runner status: %s", status.Status)
	})

	// Test 3: List
	t.Run("List", func(t *testing.T) {
		runners, err := p.List(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, runners)
		t.Logf("Found %d runners", len(runners))
	})

	// Test 4: Suspend (terminate_preserve_storage)
	t.Run("Suspend", func(t *testing.T) {
		result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{
			Strategy: provider.SuspendStrategyTerminatePreserveStorage,
		})
		require.NoError(t, err)
		assert.NotNil(t, result)
		t.Logf("Runner suspended with strategy: %s", result.Strategy)
	})

	// Test 5: Resume
	t.Run("Resume", func(t *testing.T) {
		instance, err := p.Resume(ctx, "sess_test", provider.ResumeOptions{
			RunnerID: runnerID,
			SpawnOpts: &provider.SpawnOptions{
				RunnerID: runnerID,
				Name:     runnerName + "-resumed",
			},
		})
		require.NoError(t, err)
		assert.NotNil(t, instance)
		t.Log("Runner resumed with new Pod")
	})

	// Test 6: Destroy (cleanup)
	t.Run("Destroy", func(t *testing.T) {
		err := p.Destroy(ctx, runnerID)
		require.NoError(t, err)
		t.Log("Runner destroyed (Pod deleted, PVC may still exist)")
	})

	// Note: PVC cleanup is not done here since terminate_preserve_storage keeps PVC.
	// For full cleanup, run: kubectl delete pvc -n marionette-test -l marionette.dev/runner-id
}

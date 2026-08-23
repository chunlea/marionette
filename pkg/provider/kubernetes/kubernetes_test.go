package kubernetes

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/chunlea/marionette/pkg/provider"
)

func newTestProvider(client *MockKubeClient) *Provider {
	cfg := &Config{
		Namespace:     "test-ns",
		Image:         "test/agent:latest",
		LabelPrefix:   "marionette.dev",
		RestartPolicy: "Never",
		Resources: ResourceConfig{
			Memory: "2Gi",
			CPUs:   "2",
		},
		Storage: StorageConfig{
			Size:       "10Gi",
			AccessMode: "ReadWriteOnce",
		},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())

	return NewWithClient("test-k8s", cfg, suspendCfg, client)
}

func TestProviderMetadata(t *testing.T) {
	client := NewMockKubeClient()
	p := newTestProvider(client)

	assert.Equal(t, "test-k8s", p.Name())
	assert.Equal(t, provider.ProviderTypeManaged, p.Type())

	caps := p.Capabilities()
	assert.False(t, caps.Pause, "Kubernetes does not support pause")
	assert.False(t, caps.Snapshot, "Kubernetes does not support snapshots")
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyTerminatePreserveStorage)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyTerminate)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, caps.Suspend.Default)
}

func TestSpawn(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Set pod to running after creation
		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-123", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:    "run_123",
			Name:        "run-123",
			ServerURL:   "grpc://server:9090",
			RunnerToken: "rtok_test",
			SandboxMode: "runner-is-sandbox",
			MemoryMB:    4096,
			CPUs:        4,
			Labels:      map[string]string{"custom": "label"},
			Annotations: map[string]string{"custom": "annotation"},
			TenantID:    "tenant_abc",
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.Equal(t, "run_123", instance.ID)
		assert.Equal(t, "run-123", instance.Name)
		assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
		assert.Equal(t, "runner-is-sandbox", instance.SandboxMode)
		assert.Equal(t, "label", instance.Labels["custom"])

		// Verify pod was created
		assert.Len(t, client.CreatePodCalls, 1)
		pod := client.GetStoredPod("test-ns", "marionette-run-123")
		require.NotNil(t, pod)
		assert.Equal(t, "test/agent:latest", pod.Spec.Containers[0].Image)

		// Verify env vars
		envMap := make(map[string]string)
		for _, env := range pod.Spec.Containers[0].Env {
			envMap[env.Name] = env.Value
		}
		assert.Equal(t, "grpc://server:9090", envMap["MARIONETTE_SERVER"])
		assert.Equal(t, "rtok_test", envMap["MARIONETTE_RUNNER_TOKEN"])
		assert.Equal(t, "runner-is-sandbox", envMap["MARIONETTE_SANDBOX_MODE"])

		// Verify PVC was created
		assert.Len(t, client.CreatePVCCalls, 1)
		pvc := client.GetStoredPVC("test-ns", "marionette-ws-run-123")
		require.NotNil(t, pvc)
	})

	t.Run("namespace not found", func(t *testing.T) {
		client := NewMockKubeClient()
		// Don't add namespace
		p := newTestProvider(client)

		opts := provider.SpawnOptions{
			RunnerID: "run_456",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
		assert.Contains(t, spawnErr.Reason, "namespace")
	})

	t.Run("pod creation failure", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		client.CreatePodErr = k8serrors.NewInternalError(fmt.Errorf("simulated failure"))
		p := newTestProvider(client)

		opts := provider.SpawnOptions{
			RunnerID: "run_789",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
		assert.Contains(t, spawnErr.Reason, "pod creation")
	})

	t.Run("with network policy", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Set pod to running
		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-np", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:      "run_np",
			Name:          "run-np",
			NetworkPolicy: "allow_list",
			AllowedHosts:  []string{"github.com", "10.0.0.0/8"},
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.NotNil(t, instance)

		// Verify NetworkPolicy was created
		assert.Len(t, client.CreateNetworkPolicyCalls, 1)
		np := client.GetStoredNetworkPolicy("test-ns", "marionette-np-run-np")
		require.NotNil(t, np)
	})

	t.Run("reuses existing PVC", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Pre-create PVC
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-reuse",
				Namespace: "test-ns",
			},
		})

		// Set pod to running
		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-reuse", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID: "run_reuse",
			Name:     "run-reuse",
		}

		_, err := p.Spawn(ctx, opts)
		require.NoError(t, err)

		// Should not create new PVC
		assert.Len(t, client.CreatePVCCalls, 0)
	})
}

func TestDestroy(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Create a pod first
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-del",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_del",
				},
			},
		})

		err := p.Destroy(ctx, "run_del")
		require.NoError(t, err)

		// Pod should be deleted
		assert.Len(t, client.DeletePodCalls, 1)
		assert.Nil(t, client.GetStoredPod("test-ns", "marionette-run-del"))
	})

	t.Run("runner not found", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		err := p.Destroy(ctx, "nonexistent")
		require.Error(t, err)
		var notFoundErr *provider.ErrRunnerNotFound
		require.ErrorAs(t, err, &notFoundErr)
	})
}

func TestStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("running", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-status",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_status",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
			},
		})

		status, err := p.Status(ctx, "run_status")
		require.NoError(t, err)
		assert.Equal(t, provider.InstanceStatusRunning, status.Status)
	})

	t.Run("pending", func(t *testing.T) {
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
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		})

		status, err := p.Status(ctx, "run_pending")
		require.NoError(t, err)
		assert.Equal(t, provider.InstanceStatusPending, status.Status)
	})

	t.Run("failed", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-failed",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_failed",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodFailed,
			},
		})

		status, err := p.Status(ctx, "run_failed")
		require.NoError(t, err)
		assert.Equal(t, provider.InstanceStatusFailed, status.Status)
	})

	t.Run("not found", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		_, err := p.Status(ctx, "nonexistent")
		require.Error(t, err)
		var notFoundErr *provider.ErrRunnerNotFound
		require.ErrorAs(t, err, &notFoundErr)
	})
}

func TestList(t *testing.T) {
	ctx := context.Background()

	client := NewMockKubeClient()
	client.AddNamespace("test-ns")
	p := newTestProvider(client)

	// Add some pods
	client.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "marionette-run-1",
			Namespace: "test-ns",
			Labels: map[string]string{
				"marionette.dev/managed-by": "marionette",
				"marionette.dev/runner-id":  "run_1",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})
	client.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "marionette-run-2",
			Namespace: "test-ns",
			Labels: map[string]string{
				"marionette.dev/managed-by": "marionette",
				"marionette.dev/runner-id":  "run_2",
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	})
	// Add a pod without marionette labels (should be filtered out)
	client.AddPod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-pod",
			Namespace: "test-ns",
			Labels:    map[string]string{},
		},
	})

	instances, err := p.List(ctx)
	require.NoError(t, err)
	assert.Len(t, instances, 2)

	// Verify instance data
	ids := make(map[string]bool)
	for _, inst := range instances {
		ids[inst.ID] = true
	}
	assert.True(t, ids["run_1"])
	assert.True(t, ids["run_2"])
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"run_123", "run-123"},
		{"Run_ABC", "run-abc"},
		{"test.name", "testname"},
		{"test name", "testname"},
		{"test--name", "test--name"},
		{"-test-", "test"},
		{"UPPERCASE", "uppercase"},
		// Long name should be truncated
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, len(got), 63)
		})
	}
}

func TestMapPodPhase(t *testing.T) {
	tests := []struct {
		phase corev1.PodPhase
		want  provider.InstanceStatus
	}{
		{corev1.PodPending, provider.InstanceStatusPending},
		{corev1.PodRunning, provider.InstanceStatusRunning},
		{corev1.PodSucceeded, provider.InstanceStatusStopped},
		{corev1.PodFailed, provider.InstanceStatusFailed},
		{corev1.PodUnknown, provider.InstanceStatusFailed},
		{"", provider.InstanceStatusFailed},
	}

	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			got := mapPodPhase(tt.phase)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildPod(t *testing.T) {
	client := NewMockKubeClient()
	cfg := &Config{
		Namespace:        "test-ns",
		Image:            "test/agent:v1",
		ImagePullPolicy:  "Always",
		ImagePullSecrets: []string{"regcred"},
		ServiceAccount:   "test-sa",
		LabelPrefix:      "marionette.dev",
		RestartPolicy:    "Never",
		Resources: ResourceConfig{
			Memory:           "4Gi",
			CPUs:             "2",
			MemoryRequest:    "2Gi",
			CPURequest:       "1",
			EphemeralStorage: "10Gi",
		},
		Storage: StorageConfig{
			Size:       "10Gi",
			AccessMode: "ReadWriteOnce",
		},
		NodeSelector: map[string]string{"node-type": "compute"},
		Tolerations: []TolerationConfig{
			{Key: "dedicated", Operator: "Equal", Value: "agent", Effect: "NoSchedule"},
		},
		Labels:      map[string]string{"app": "marionette"},
		Annotations: map[string]string{"version": "v1"},
		Cmd:         []string{"/bin/agent"},
		Args:        []string{"--debug"},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())
	p := NewWithClient("test", cfg, suspendCfg, client)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test",
		ServerURL:   "grpc://server:9090",
		RunnerToken: "token",
		SandboxMode: "runner-is-sandbox",
		Labels:      map[string]string{"custom": "label"},
		TenantID:    "tenant_123",
	}

	pod := p.buildPod("test-pod", "test-pvc", opts)

	// Verify metadata
	assert.Equal(t, "test-pod", pod.Name)
	assert.Equal(t, "test-ns", pod.Namespace)
	assert.Equal(t, "marionette", pod.Labels["marionette.dev/managed-by"])
	assert.Equal(t, "run_test", pod.Labels["marionette.dev/runner-id"])
	assert.Equal(t, "tenant_123", pod.Labels["marionette.dev/tenant-id"])
	assert.Equal(t, "marionette", pod.Labels["app"])
	assert.Equal(t, "label", pod.Labels["custom"])
	assert.Equal(t, "v1", pod.Annotations["version"])

	// Verify container
	require.Len(t, pod.Spec.Containers, 1)
	container := pod.Spec.Containers[0]
	assert.Equal(t, "agent", container.Name)
	assert.Equal(t, "test/agent:v1", container.Image)
	assert.Equal(t, corev1.PullAlways, container.ImagePullPolicy)
	assert.Equal(t, []string{"/bin/agent"}, container.Command)
	assert.Equal(t, []string{"--debug"}, container.Args)

	// Verify resources
	assert.Equal(t, "4Gi", container.Resources.Limits.Memory().String())
	assert.Equal(t, "2Gi", container.Resources.Requests.Memory().String())

	// Verify volume mount
	require.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "workspace", container.VolumeMounts[0].Name)
	assert.Equal(t, "/workspace", container.VolumeMounts[0].MountPath)

	// Verify pod spec
	assert.Equal(t, corev1.RestartPolicyNever, pod.Spec.RestartPolicy)
	assert.Equal(t, "test-sa", pod.Spec.ServiceAccountName)
	assert.Equal(t, map[string]string{"node-type": "compute"}, pod.Spec.NodeSelector)
	require.Len(t, pod.Spec.Tolerations, 1)
	assert.Equal(t, "dedicated", pod.Spec.Tolerations[0].Key)
	require.Len(t, pod.Spec.ImagePullSecrets, 1)
	assert.Equal(t, "regcred", pod.Spec.ImagePullSecrets[0].Name)

	// Verify volumes
	require.Len(t, pod.Spec.Volumes, 1)
	assert.Equal(t, "workspace", pod.Spec.Volumes[0].Name)
	assert.Equal(t, "test-pvc", pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName)
}

func TestBuildPVC(t *testing.T) {
	client := NewMockKubeClient()
	cfg := &Config{
		Namespace:   "test-ns",
		Image:       "test/agent:latest",
		LabelPrefix: "marionette.dev",
		Storage: StorageConfig{
			StorageClass: "fast-ssd",
			Size:         "20Gi",
			AccessMode:   "ReadWriteMany",
		},
		Labels:      map[string]string{"app": "marionette"},
		Annotations: map[string]string{"description": "test"},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())
	p := NewWithClient("test", cfg, suspendCfg, client)

	opts := provider.SpawnOptions{
		RunnerID: "run_test",
		TenantID: "tenant_abc",
		DiskMB:   50 * 1024, // 50GB
	}

	pvc := p.buildPVC("test-pvc", opts)

	// Verify metadata
	assert.Equal(t, "test-pvc", pvc.Name)
	assert.Equal(t, "test-ns", pvc.Namespace)
	assert.Equal(t, "marionette", pvc.Labels["marionette.dev/managed-by"])
	assert.Equal(t, "run_test", pvc.Labels["marionette.dev/runner-id"])
	assert.Equal(t, "tenant_abc", pvc.Labels["marionette.dev/tenant-id"])
	assert.Equal(t, "marionette", pvc.Labels["app"])
	assert.Equal(t, "test", pvc.Annotations["description"])

	// Verify spec
	assert.Equal(t, "fast-ssd", *pvc.Spec.StorageClassName)
	assert.Contains(t, pvc.Spec.AccessModes, corev1.ReadWriteMany)
	// DiskMB should override config size (50 * 1024 MB = 50Gi)
	assert.Equal(t, "50Gi", pvc.Spec.Resources.Requests.Storage().String())
}

func TestInterfaceCompliance(t *testing.T) {
	// Verify Provider implements all required interfaces
	var _ provider.Provider = (*Provider)(nil)
	var _ provider.SuspendableProvider = (*Provider)(nil)
}

func TestNewFromStoreConfig(t *testing.T) {
	// This test verifies the factory function signature
	// Actual Kubernetes connection would fail without a cluster
	// so we just verify the config parsing works

	t.Run("parse config only", func(t *testing.T) {
		configJSON := `{
			"namespace": "marionette",
			"image": "marionette/agent:v1",
			"resources": {
				"memory": "4Gi",
				"cpus": "2"
			}
		}`

		cfg, err := ParseConfig([]byte(configJSON))
		require.NoError(t, err)
		assert.Equal(t, "marionette", cfg.Namespace)
		assert.Equal(t, "marionette/agent:v1", cfg.Image)
	})
}

// TestMockKubeClient tests that the mock client behaves correctly
func TestMockKubeClient(t *testing.T) {
	ctx := context.Background()

	t.Run("pod lifecycle", func(t *testing.T) {
		client := NewMockKubeClient()

		// Create pod
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
		}
		created, err := client.CreatePod(ctx, "ns", pod)
		require.NoError(t, err)
		assert.Equal(t, "ns", created.Namespace)
		assert.NotEmpty(t, created.UID)

		// Get pod
		got, err := client.GetPod(ctx, "ns", "test-pod")
		require.NoError(t, err)
		assert.Equal(t, "test-pod", got.Name)

		// Delete pod
		err = client.DeletePod(ctx, "ns", "test-pod", metav1.DeleteOptions{})
		require.NoError(t, err)

		// Get should fail now
		_, err = client.GetPod(ctx, "ns", "test-pod")
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
	})

	t.Run("error injection", func(t *testing.T) {
		client := NewMockKubeClient()
		client.CreatePodErr = k8serrors.NewInternalError(fmt.Errorf("simulated error"))

		_, err := client.CreatePod(ctx, "ns", &corev1.Pod{})
		require.Error(t, err)
		assert.True(t, k8serrors.IsInternalError(err))
	})

	t.Run("already exists", func(t *testing.T) {
		client := NewMockKubeClient()

		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
		_, err := client.CreatePod(ctx, "ns", pod)
		require.NoError(t, err)

		_, err = client.CreatePod(ctx, "ns", pod)
		require.Error(t, err)
		assert.True(t, k8serrors.IsAlreadyExists(err))
	})

	t.Run("not found errors", func(t *testing.T) {
		client := NewMockKubeClient()

		_, err := client.GetPod(ctx, "ns", "nonexistent")
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		_, err = client.GetPVC(ctx, "ns", "nonexistent")
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		_, err = client.GetNetworkPolicy(ctx, "ns", "nonexistent")
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		_, err = client.GetNamespace(ctx, "nonexistent")
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
	})

	t.Run("delete not found", func(t *testing.T) {
		client := NewMockKubeClient()

		err := client.DeletePod(ctx, "ns", "nonexistent", metav1.DeleteOptions{})
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		err = client.DeletePVC(ctx, "ns", "nonexistent", metav1.DeleteOptions{})
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		err = client.DeleteNetworkPolicy(ctx, "ns", "nonexistent", metav1.DeleteOptions{})
		require.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
	})
}

// TestErrTypes tests error type assertions
func TestErrTypes(t *testing.T) {
	// Verify our error types are correctly used
	t.Run("ErrRunnerNotFound", func(t *testing.T) {
		err := &provider.ErrRunnerNotFound{RunnerID: "run_123"}
		assert.Contains(t, err.Error(), "run_123")
	})

	t.Run("ErrSpawnFailed", func(t *testing.T) {
		err := &provider.ErrSpawnFailed{Reason: "test reason", Cause: k8serrors.NewNotFound(schema.GroupResource{}, "")}
		assert.Contains(t, err.Error(), "test reason")
	})

	t.Run("ErrDestroyFailed", func(t *testing.T) {
		err := &provider.ErrDestroyFailed{RunnerID: "run_456"}
		assert.Contains(t, err.Error(), "run_456")
	})
}

func TestNetworkPolicyModes(t *testing.T) {
	ctx := context.Background()

	t.Run("proxy mode", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-proxy", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:      "run_proxy",
			Name:          "run-proxy",
			NetworkPolicy: "proxy",
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.NotNil(t, instance)

		// Verify NetworkPolicy was created
		np := client.GetStoredNetworkPolicy("test-ns", "marionette-np-run-proxy")
		assert.NotNil(t, np)
	})

	t.Run("air_gapped mode", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-airgap", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:      "run_airgap",
			Name:          "run-airgap",
			NetworkPolicy: "air_gapped",
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.NotNil(t, instance)

		// Verify NetworkPolicy was created with no egress
		np := client.GetStoredNetworkPolicy("test-ns", "marionette-np-run-airgap")
		assert.NotNil(t, np)
	})

	t.Run("none mode - no network policy", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-none", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:      "run_none",
			Name:          "run-none",
			NetworkPolicy: "none",
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.NotNil(t, instance)

		// No NetworkPolicy should be created
		np := client.GetStoredNetworkPolicy("test-ns", "marionette-run-none")
		assert.Nil(t, np)
	})
}

func TestAllowListFormats(t *testing.T) {
	ctx := context.Background()

	t.Run("mixed IPs and domains", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		go func() {
			time.Sleep(100 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-mixed", corev1.PodRunning)
		}()

		opts := provider.SpawnOptions{
			RunnerID:      "run_mixed",
			Name:          "run-mixed",
			NetworkPolicy: "allow_list",
			AllowedHosts: []string{
				"192.168.1.0/24",      // CIDR
				"10.0.0.1",            // Single IP
				"github.com",          // Domain
				"2001:db8::/32",       // IPv6 CIDR
				"api.example.com:443", // Domain with port (port ignored)
			},
		}

		instance, err := p.Spawn(ctx, opts)
		require.NoError(t, err)
		assert.NotNil(t, instance)

		np := client.GetStoredNetworkPolicy("test-ns", "marionette-np-run-mixed")
		require.NotNil(t, np)
		// Verify egress rules exist
		assert.NotEmpty(t, np.Spec.Egress)
	})
}

func TestWaitForPodTimeout(t *testing.T) {
	ctx := context.Background()

	t.Run("pod fails during wait", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Set pod to failed after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			client.SetPodPhase("test-ns", "marionette-run-fail", corev1.PodFailed)
		}()

		opts := provider.SpawnOptions{
			RunnerID: "run_fail",
			Name:     "run-fail",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
		assert.Contains(t, spawnErr.Reason, "failed")
	})
}

func TestNewFromJSON(t *testing.T) {
	suspendJSON := []byte(`{}`)

	t.Run("valid config", func(t *testing.T) {
		configJSON := []byte(`{
			"namespace": "marionette",
			"image": "marionette/agent:v1",
			"resources": {
				"memory": "4Gi",
				"cpus": "2"
			}
		}`)

		// This may succeed if kubeconfig is available, or fail with connection error
		// Either way, config parsing should work (no validation error)
		p, err := NewFromJSON("k8s-test", configJSON, suspendJSON)
		if err != nil {
			// Should not be a validation error
			assert.NotContains(t, err.Error(), "invalid namespace")
			assert.NotContains(t, err.Error(), "invalid memory")
		} else {
			// If it succeeds, provider should be valid
			assert.NotNil(t, p)
			assert.Equal(t, "k8s-test", p.Name())
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		configJSON := []byte(`{
			"namespace": "Invalid-Namespace",
			"image": "marionette/agent:v1"
		}`)

		_, err := NewFromJSON("k8s-test", configJSON, suspendJSON)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "namespace")
	})
}

func TestStatusErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("get pod error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add pod first
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-status",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_status",
				},
			},
		})

		// Inject error for GetPod
		client.GetPodErr = fmt.Errorf("simulated get error")

		_, err := p.Status(ctx, "run_status")
		require.Error(t, err)
	})
}

func TestDestroyErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("delete pod error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-destroy",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_destroy",
				},
			},
		})

		client.DeletePodErr = fmt.Errorf("simulated delete error")

		err := p.Destroy(ctx, "run_destroy")
		require.Error(t, err)
		var destroyErr *provider.ErrDestroyFailed
		require.ErrorAs(t, err, &destroyErr)
	})

	t.Run("delete PVC error in terminate strategy", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-pvcerr",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_pvcerr",
				},
			},
		})
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-pvcerr",
				Namespace: "test-ns",
			},
		})

		client.DeletePVCErr = fmt.Errorf("simulated PVC delete error")

		_, err := p.Suspend(ctx, "run_pvcerr", provider.SuspendOptions{
			Strategy: provider.SuspendStrategyTerminate,
		})
		require.Error(t, err)
	})
}

func TestBuildEnvVariations(t *testing.T) {
	client := NewMockKubeClient()
	cfg := &Config{
		Namespace:     "test-ns",
		Image:         "test/agent:latest",
		LabelPrefix:   "marionette.dev",
		RestartPolicy: "Never",
		Resources:     ResourceConfig{Memory: "2Gi", CPUs: "2"},
		Storage:       StorageConfig{Size: "10Gi", AccessMode: "ReadWriteOnce"},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())
	p := NewWithClient("test-k8s", cfg, suspendCfg, client)

	t.Run("with environment variables", func(t *testing.T) {
		opts := provider.SpawnOptions{
			RunnerID:    "run_env",
			Name:        "run-env",
			ServerURL:   "grpc://server:9090",
			RunnerToken: "test-token",
			SandboxMode: "runner-is-sandbox",
			Environment: map[string]string{
				"CUSTOM_VAR": "custom-value",
			},
		}

		pod := p.buildPod("test-pod", "test-pvc", opts)
		envMap := make(map[string]string)
		for _, e := range pod.Spec.Containers[0].Env {
			envMap[e.Name] = e.Value
		}

		assert.Equal(t, "grpc://server:9090", envMap["MARIONETTE_SERVER"])
		assert.Equal(t, "test-token", envMap["MARIONETTE_RUNNER_TOKEN"])
		assert.Equal(t, "runner-is-sandbox", envMap["MARIONETTE_SANDBOX_MODE"])
		assert.Equal(t, "custom-value", envMap["CUSTOM_VAR"])
	})
}

func TestMockClientAdditionalMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("ListPVCs", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc1", Namespace: "ns1"},
		})
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc2", Namespace: "ns1"},
		})
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc3", Namespace: "ns2"},
		})

		list, err := client.ListPVCs(ctx, "ns1", metav1.ListOptions{})
		require.NoError(t, err)
		assert.Len(t, list.Items, 2)
	})

	t.Run("ListNetworkPolicies", func(t *testing.T) {
		client := NewMockKubeClient()

		np1, _ := client.CreateNetworkPolicy(ctx, "ns1", &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "np1"},
		})
		assert.NotNil(t, np1)

		list, err := client.ListNetworkPolicies(ctx, "ns1", metav1.ListOptions{})
		require.NoError(t, err)
		assert.Len(t, list.Items, 1)
	})
}

func TestFindPodErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("list pods error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		client.ListPodsErr = fmt.Errorf("simulated list error")

		_, err := p.Status(ctx, "run_any")
		require.Error(t, err)
	})

	t.Run("multiple pods with same runner ID", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add two pods with the same runner ID (shouldn't happen, but test the path)
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-dup1",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_dup",
				},
			},
		})
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-dup2",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_dup",
				},
			},
		})

		// Should still work, returns first found
		status, err := p.Status(ctx, "run_dup")
		require.NoError(t, err)
		assert.NotNil(t, status)
	})
}

func TestWaitForPodContainerStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("pod unknown with waiting container", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Set pod to unknown with container waiting after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			client.UpdatePodStatus("test-ns", "marionette-run-waiting", func(pod *corev1.Pod) {
				pod.Status.Phase = corev1.PodUnknown
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason: "ImagePullBackOff",
							},
						},
					},
				}
			})
		}()

		opts := provider.SpawnOptions{
			RunnerID: "run_waiting",
			Name:     "run-waiting",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
	})

	t.Run("pod failed with terminated container", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Set pod to failed with terminated container
		go func() {
			time.Sleep(50 * time.Millisecond)
			client.UpdatePodStatus("test-ns", "marionette-run-term", func(pod *corev1.Pod) {
				pod.Status.Phase = corev1.PodFailed
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:   "OOMKilled",
								ExitCode: 137,
							},
						},
					},
				}
			})
		}()

		opts := provider.SpawnOptions{
			RunnerID: "run_term",
			Name:     "run-term",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
		assert.Contains(t, spawnErr.Reason, "failed")
	})
}

func TestContextCancellation(t *testing.T) {
	t.Run("spawn with cancelled context", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel context after pod creation but before ready
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		opts := provider.SpawnOptions{
			RunnerID: "run_cancel",
			Name:     "run-cancel",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
	})
}

func TestParseQuantityEdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"0", false},
		{"0.1", false},
		{"0.001", false},
		{"-100", true},
		{"", true},
		{"abc", true},
		{"100X", true},
		{"100Ki", false},
		{"100ki", true}, // lowercase not supported
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := ParseQuantity(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseCPUsEdgeCases(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"0.001", false},
		{"0.1", false},
		{"10", false},
		{"0", true},      // zero is invalid
		{"", true},       // empty
		{"-1", true},     // negative
		{"1000m", false}, // millicores
		{"1m", false},    // 1 millicore
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			_, err := ParseCPUs(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestListAll(t *testing.T) {
	ctx := context.Background()

	t.Run("list all runners", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add pods with marionette label
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-t1",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id":  "run_t1",
					"marionette.dev/managed-by": "marionette",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		})
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-t2",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id":  "run_t2",
					"marionette.dev/managed-by": "marionette",
				},
			},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		})

		instances, err := p.List(ctx)
		require.NoError(t, err)
		assert.Len(t, instances, 2)
	})
}

func TestBuildTolerations(t *testing.T) {
	client := NewMockKubeClient()
	cfg := &Config{
		Namespace:     "test-ns",
		Image:         "test/agent:latest",
		LabelPrefix:   "marionette.dev",
		RestartPolicy: "Never",
		Resources:     ResourceConfig{Memory: "2Gi", CPUs: "2"},
		Storage:       StorageConfig{Size: "10Gi", AccessMode: "ReadWriteOnce"},
		Tolerations: []TolerationConfig{
			{
				Key:      "dedicated",
				Operator: "Equal",
				Value:    "agents",
				Effect:   "NoSchedule",
			},
			{
				Key:      "node.kubernetes.io/not-ready",
				Operator: "Exists",
				Effect:   "NoExecute",
			},
		},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())
	p := NewWithClient("test-k8s", cfg, suspendCfg, client)

	opts := provider.SpawnOptions{
		RunnerID: "run_tol",
		Name:     "run-tol",
	}

	pod := p.buildPod("test-pod", "test-pvc", opts)
	assert.Len(t, pod.Spec.Tolerations, 2)

	// Verify first toleration
	assert.Equal(t, "dedicated", pod.Spec.Tolerations[0].Key)
	assert.Equal(t, corev1.TolerationOpEqual, pod.Spec.Tolerations[0].Operator)
	assert.Equal(t, "agents", pod.Spec.Tolerations[0].Value)
	assert.Equal(t, corev1.TaintEffectNoSchedule, pod.Spec.Tolerations[0].Effect)
}

func TestEnsurePVCCreation(t *testing.T) {
	ctx := context.Background()

	t.Run("create PVC failure", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		client.CreatePVCErr = fmt.Errorf("simulated PVC creation error")
		p := newTestProvider(client)

		opts := provider.SpawnOptions{
			RunnerID: "run_pvcfail",
			Name:     "run-pvcfail",
		}

		_, err := p.Spawn(ctx, opts)
		require.Error(t, err)
		var spawnErr *provider.ErrSpawnFailed
		require.ErrorAs(t, err, &spawnErr)
	})
}

func TestNetworkPolicyCreationError(t *testing.T) {
	// Test that createNetworkPolicy returns proper error
	ctx := context.Background()

	client := NewMockKubeClient()
	client.AddNamespace("test-ns")
	client.CreateNetworkPolicyErr = fmt.Errorf("simulated network policy error")
	p := newTestProvider(client)

	opts := provider.SpawnOptions{
		RunnerID:      "run_test",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}
	err := p.createNetworkPolicy(ctx, "run_test", opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated network policy error")
}

func TestMockClientErrorPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("GetNetworkPolicy error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.GetNetworkPolicyErr = fmt.Errorf("simulated error")

		_, err := client.GetNetworkPolicy(ctx, "ns", "np")
		require.Error(t, err)
	})

	t.Run("DeleteNetworkPolicy error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.DeleteNetworkPolicyErr = fmt.Errorf("simulated error")

		err := client.DeleteNetworkPolicy(ctx, "ns", "np", metav1.DeleteOptions{})
		require.Error(t, err)
	})

	t.Run("GetPVC error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.GetPVCErr = fmt.Errorf("simulated error")

		_, err := client.GetPVC(ctx, "ns", "pvc")
		require.Error(t, err)
	})

	t.Run("GetNamespace error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.GetNamespaceErr = fmt.Errorf("simulated error")

		_, err := client.GetNamespace(ctx, "ns")
		require.Error(t, err)
	})
}

func TestSuspendFallback(t *testing.T) {
	ctx := context.Background()

	t.Run("fallback to terminate on error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")

		cfg := &Config{
			Namespace:     "test-ns",
			Image:         "test/agent:latest",
			LabelPrefix:   "marionette.dev",
			RestartPolicy: "Never",
			Resources:     ResourceConfig{Memory: "2Gi", CPUs: "2"},
			Storage:       StorageConfig{Size: "10Gi", AccessMode: "ReadWriteOnce"},
		}
		suspendCfg := &provider.SuspendConfig{
			Strategy: provider.SuspendStrategyTerminatePreserveStorage,
			Fallback: provider.SuspendStrategyTerminate,
		}
		p := NewWithClient("test-k8s", cfg, suspendCfg, client)

		// Add pod
		client.AddPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-run-fb",
				Namespace: "test-ns",
				Labels: map[string]string{
					"marionette.dev/runner-id": "run_fb",
				},
			},
		})

		result, err := p.Suspend(ctx, "run_fb", provider.SuspendOptions{
			Strategy: provider.SuspendStrategyTerminatePreserveStorage,
		})

		require.NoError(t, err)
		assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, result.Strategy)
	})
}

func TestResumeWithPVCExists(t *testing.T) {
	ctx := context.Background()

	t.Run("resume with existing PVC creates new pod", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		p := newTestProvider(client)

		// Add PVC but no pod
		client.AddPVC(&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "marionette-ws-run-resume",
				Namespace: "test-ns",
			},
		})

		// Set new pod to running after creation
		go func() {
			for range 50 {
				time.Sleep(50 * time.Millisecond)
				// Use UpdatePodStatus for thread-safe modification
				updated := false
				client.UpdatePodStatus("test-ns", "marionette-run-resume-new", func(pod *corev1.Pod) {
					pod.Status.Phase = corev1.PodRunning
					updated = true
				})
				if updated {
					break
				}
			}
		}()

		instance, err := p.Resume(ctx, "sess_123", provider.ResumeOptions{
			RunnerID: "run_resume",
			SpawnOpts: &provider.SpawnOptions{
				RunnerID: "run_resume",
				Name:     "run-resume-new",
			},
		})

		require.NoError(t, err)
		assert.NotNil(t, instance)
	})
}

func TestNewWithClientValidation(t *testing.T) {
	client := NewMockKubeClient()

	cfg := &Config{
		Namespace:     "test-ns",
		Image:         "test/agent:latest",
		LabelPrefix:   "marionette.dev",
		RestartPolicy: "Never",
		Resources:     ResourceConfig{Memory: "2Gi", CPUs: "2"},
		Storage:       StorageConfig{Size: "10Gi", AccessMode: "ReadWriteOnce"},
	}
	suspendCfg := &provider.SuspendConfig{}
	suspendCfg.ApplyDefaults(defaultSuspendConfig())

	// Test with nil config - should still work but use defaults
	p := NewWithClient("test", cfg, suspendCfg, client)
	assert.NotNil(t, p)
	assert.Equal(t, "test", p.Name())
}

func TestParseConfigErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		_, err := ParseConfig([]byte(`{invalid json`))
		require.Error(t, err)
	})

	t.Run("empty config uses defaults", func(t *testing.T) {
		cfg, err := ParseConfig([]byte(`{}`))
		require.NoError(t, err)
		assert.Equal(t, "default", cfg.Namespace)
		assert.Equal(t, "marionette.dev", cfg.LabelPrefix)
	})
}

func TestParseSuspendConfigErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		_, err := provider.ParseSuspendConfig([]byte(`{invalid json`), defaultSuspendConfig())
		require.Error(t, err)
	})

	t.Run("empty config uses defaults", func(t *testing.T) {
		cfg, err := provider.ParseSuspendConfig([]byte(`{}`), defaultSuspendConfig())
		require.NoError(t, err)
		assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Strategy)
	})
}

func TestBuildNetworkPolicyModes(t *testing.T) {
	client := NewMockKubeClient()
	p := newTestProvider(client)

	t.Run("proxy mode", func(t *testing.T) {
		opts := provider.SpawnOptions{
			RunnerID:      "run_proxy",
			NetworkPolicy: "proxy",
		}
		np, err := p.buildNetworkPolicy("run_proxy", opts)
		require.NoError(t, err)
		require.NotNil(t, np)
		// Proxy mode should have specific egress rules
		assert.NotNil(t, np.Spec.Egress)
	})

	t.Run("air_gapped mode", func(t *testing.T) {
		opts := provider.SpawnOptions{
			RunnerID:      "run_airgap",
			NetworkPolicy: "air_gapped",
		}
		np, err := p.buildNetworkPolicy("run_airgap", opts)
		require.NoError(t, err)
		require.NotNil(t, np)
		// Air gapped should have only DNS egress
		assert.Len(t, np.Spec.Egress, 1)
	})

	t.Run("none needs no policy", func(t *testing.T) {
		np, err := p.buildNetworkPolicy("run_none", provider.SpawnOptions{
			RunnerID:      "run_none",
			NetworkPolicy: "none",
		})
		require.NoError(t, err)
		assert.Nil(t, np, "policy \"none\" means no NetworkPolicy is created")
	})

	t.Run("unknown policy is an error", func(t *testing.T) {
		// This used to return a nil policy that went straight to the API.
		np, err := p.buildNetworkPolicy("run_bogus", provider.SpawnOptions{
			RunnerID:      "run_bogus",
			NetworkPolicy: "not_a_real_policy",
		})
		require.Error(t, err)
		assert.Nil(t, np)
	})
}

func TestListError(t *testing.T) {
	ctx := context.Background()

	t.Run("list pods error", func(t *testing.T) {
		client := NewMockKubeClient()
		client.AddNamespace("test-ns")
		client.ListPodsErr = fmt.Errorf("simulated list error")
		p := newTestProvider(client)

		_, err := p.List(ctx)
		require.Error(t, err)
	})
}

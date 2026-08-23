//go:build integration

package docker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
)

// Integration tests require Docker to be running.
// Run with: go test -tags=integration ./pkg/provider/docker/...

func TestDockerProvider_SpawnDestroy_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Create provider with minimal config
	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest", // Use small image for fast tests
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "128m",
			CPUs:   "0.5",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	// Generate unique runner ID
	runnerID := id.New("run")

	// Spawn container
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "integration-test-" + runnerID[4:12], // Short unique name
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		SandboxMode: "runner-is-sandbox",
	})
	require.NoError(t, err)
	require.NotNil(t, instance)

	t.Logf("Spawned container: %s (ID: %s)", instance.Name, instance.ProviderID[:12])

	// Verify instance properties
	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
	assert.NotEmpty(t, instance.ProviderID)

	// Verify status
	status, err := p.Status(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusRunning, status.Status)

	// Verify in list
	instances, err := p.List(ctx)
	require.NoError(t, err)

	found := false
	for _, inst := range instances {
		if inst.ID == runnerID {
			found = true
			break
		}
	}
	assert.True(t, found, "spawned container should appear in list")

	// Cleanup
	err = p.Destroy(ctx, runnerID, provider.DestroyOptions{})
	require.NoError(t, err)

	// Verify destroyed
	_, err = p.Status(ctx, runnerID)
	assert.Error(t, err)

	var notFoundErr *provider.ErrRunnerNotFound
	assert.ErrorAs(t, err, &notFoundErr)
}

func TestDockerProvider_PauseUnpause_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest",
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "128m",
			CPUs:   "0.5",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	runnerID := id.New("run")

	// Spawn container with a long-running command
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "pause-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	t.Logf("Spawned container for pause test: %s", instance.ProviderID[:12])

	// Wait a moment for container to stabilize
	time.Sleep(500 * time.Millisecond)

	// Pause
	err = p.Pause(ctx, runnerID)
	require.NoError(t, err)

	// Wait for pause to take effect
	time.Sleep(200 * time.Millisecond)

	// Verify paused
	status, err := p.Status(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusPaused, status.Status)

	// Unpause
	err = p.Unpause(ctx, runnerID)
	require.NoError(t, err)

	// Wait for unpause to take effect
	time.Sleep(200 * time.Millisecond)

	// Verify running again
	status, err = p.Status(ctx, runnerID)
	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusRunning, status.Status)
}

func TestDockerProvider_NetworkAutoCreate_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// Use a unique network name to avoid conflicts
	networkName := "marionette-test-" + id.New("net")[4:12]

	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest",
		Network:     networkName,
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "128m",
			CPUs:   "0.5",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	runnerID := id.New("run")

	// Spawn container - this should auto-create the network
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "network-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	})
	require.NoError(t, err)

	t.Logf("Spawned container with auto-created network: %s", networkName)

	// Cleanup container
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.NotNil(t, instance)

	// Verify network was created by spawning another container on same network
	runnerID2 := id.New("run")
	instance2, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID2,
		Name:        "network-test2-" + runnerID2[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID2, provider.DestroyOptions{})

	assert.NotNil(t, instance2)
}

func TestDockerProvider_ResourceLimits_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest",
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "256m",
			CPUs:   "1",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	runnerID := id.New("run")

	// Spawn with custom resource limits
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "resource-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		MemoryMB:    512,
		CPUs:        2.0,
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	t.Logf("Spawned container with custom resources: %s", instance.ProviderID[:12])

	// Inspect container to verify limits
	info, err := client.ContainerInspect(ctx, instance.ProviderID)
	require.NoError(t, err)

	// Verify memory limit (512MB)
	expectedMemory := int64(512 * 1024 * 1024)
	assert.Equal(t, expectedMemory, info.HostConfig.Memory)

	// Verify CPU limit (2.0 CPUs = 2e9 NanoCPUs)
	expectedNanoCPUs := int64(2e9)
	assert.Equal(t, expectedNanoCPUs, info.HostConfig.NanoCPUs)
}

func TestDockerProvider_Labels_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest",
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "128m",
			CPUs:   "0.5",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	runnerID := id.New("run")
	tenantID := "tenant_abc123"

	// Spawn with custom labels
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "labels-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		TenantID:    tenantID,
		Labels: map[string]string{
			"custom/label": "custom-value",
		},
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	// Inspect container to verify labels
	info, err := client.ContainerInspect(ctx, instance.ProviderID)
	require.NoError(t, err)

	// Verify marionette labels
	assert.Equal(t, "marionette", info.Config.Labels["marionette.dev/managed-by"])
	assert.Equal(t, runnerID, info.Config.Labels["marionette.dev/runner-id"])
	assert.Equal(t, tenantID, info.Config.Labels["marionette.dev/tenant-id"])

	// Verify custom labels
	assert.Equal(t, "custom-value", info.Config.Labels["custom/label"])
}

func TestDockerProvider_Environment_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "alpine:latest",
		LabelPrefix: "marionette.dev",
		Cmd:         []string{"sh", "-c", "tail -f /dev/null"}, // Keep container running
		Resources: ResourceConfig{
			Memory: "128m",
			CPUs:   "0.5",
		},
	}

	client, err := NewDockerClient(cfg)
	require.NoError(t, err)
	defer client.Close()

	p := NewWithClient("integration-test", cfg, nil, client)

	runnerID := id.New("run")

	// Spawn with custom environment
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "env-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		SandboxMode: "runner-is-sandbox",
		Environment: map[string]string{
			"CUSTOM_VAR": "custom-value",
		},
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	// Inspect container to verify environment
	info, err := client.ContainerInspect(ctx, instance.ProviderID)
	require.NoError(t, err)

	envMap := make(map[string]string)
	for _, e := range info.Config.Env {
		parts := splitEnv(e)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	// Verify marionette environment variables
	assert.Equal(t, "localhost:9090", envMap["MARIONETTE_SERVER"])
	assert.Equal(t, "test-token", envMap["MARIONETTE_RUNNER_TOKEN"])
	assert.Equal(t, "runner-is-sandbox", envMap["MARIONETTE_SANDBOX_MODE"])

	// Verify custom environment
	assert.Equal(t, "custom-value", envMap["CUSTOM_VAR"])
}

func TestDockerProvider_NewFromJSON_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	configJSON := json.RawMessage(`{
		"host": "unix:///var/run/docker.sock",
		"image": "alpine:latest",
		"cmd": ["sh", "-c", "tail -f /dev/null"],
		"resources": {
			"memory": "128m",
			"cpus": "0.5"
		}
	}`)

	suspendConfigJSON := json.RawMessage(`{
		"strategy": "pause",
		"min_duration": "30s",
		"max_duration": "1h"
	}`)

	p, err := NewFromJSON("json-test", configJSON, suspendConfigJSON)
	require.NoError(t, err)
	defer p.Close()

	runnerID := id.New("run")

	// Spawn container to verify provider works
	instance, err := p.Spawn(ctx, provider.SpawnOptions{
		RunnerID:    runnerID,
		Name:        "json-test-" + runnerID[4:12],
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	})
	require.NoError(t, err)
	defer p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)

	// Verify suspend config
	suspendCfg := p.SuspendConfig()
	assert.Equal(t, provider.SuspendStrategyPause, suspendCfg.Strategy)
	assert.Equal(t, 30*time.Second, suspendCfg.MinDuration)
	assert.Equal(t, 1*time.Hour, suspendCfg.MaxDuration)
}

// splitEnv splits an environment variable string "KEY=VALUE" into parts.
func splitEnv(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

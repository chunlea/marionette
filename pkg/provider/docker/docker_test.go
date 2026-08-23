package docker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

// MockDockerClient implements DockerClient for testing.
type MockDockerClient struct {
	mock.Mock
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config,
	hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig,
	platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	args := m.Called(ctx, config, hostConfig, networkingConfig, platform, containerName)
	return args.Get(0).(container.CreateResponse), args.Error(1)
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	args := m.Called(ctx, containerID, options)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	args := m.Called(ctx, containerID)
	return args.Get(0).(types.ContainerJSON), args.Error(1)
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	args := m.Called(ctx, options)
	return args.Get(0).([]types.Container), args.Error(1)
}

func (m *MockDockerClient) ContainerPause(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *MockDockerClient) ContainerUnpause(ctx context.Context, containerID string) error {
	args := m.Called(ctx, containerID)
	return args.Error(0)
}

func (m *MockDockerClient) ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error) {
	args := m.Called(ctx, refStr, options)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockDockerClient) NetworkList(ctx context.Context, options network.ListOptions) ([]network.Summary, error) {
	args := m.Called(ctx, options)
	return args.Get(0).([]network.Summary), args.Error(1)
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	args := m.Called(ctx, name, options)
	return args.Get(0).(network.CreateResponse), args.Error(1)
}

func (m *MockDockerClient) NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error) {
	args := m.Called(ctx, networkID, options)
	return args.Get(0).(network.Inspect), args.Error(1)
}

func (m *MockDockerClient) NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	args := m.Called(ctx, networkID, containerID, config)
	return args.Error(0)
}

func (m *MockDockerClient) NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error {
	args := m.Called(ctx, networkID, containerID, force)
	return args.Error(0)
}

func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	args := m.Called(ctx)
	return args.Get(0).(types.Ping), args.Error(1)
}

func (m *MockDockerClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Ensure MockDockerClient implements DockerClient
var _ DockerClient = (*MockDockerClient)(nil)

func newTestProvider(client *MockDockerClient) *Provider {
	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "marionette/agent:latest",
		Network:     "test-network",
		LabelPrefix: "marionette.dev",
		Resources: ResourceConfig{
			Memory: "2g",
			CPUs:   "2",
		},
	}
	return NewWithClient("test-docker", cfg, nil, client)
}

func TestProvider_Name(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)

	assert.Equal(t, "test-docker", p.Name())
}

func TestProvider_Type(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)

	assert.Equal(t, provider.ProviderTypeManaged, p.Type())
}

func TestProvider_Capabilities(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)

	caps := p.Capabilities()

	assert.True(t, caps.Pause)
	assert.False(t, caps.Snapshot)
	assert.Equal(t, provider.SuspendStrategyPause, caps.Suspend.Default)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyPause)
	assert.Contains(t, caps.Suspend.Strategies, provider.SuspendStrategyTerminatePreserveStorage)
}

func TestProvider_Spawn_Success(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "marionette-test-runner").
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_BxKmNpVq1StGXR8a",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		SandboxMode: "runner-is-sandbox",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.Equal(t, "run_BxKmNpVq1StGXR8a", instance.ID)
	assert.Equal(t, "abc123", instance.ProviderID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)
	assert.Equal(t, "runner-is-sandbox", instance.SandboxMode)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_CreateFails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create fails
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{}, errors.New("image not found"))

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	assert.Nil(t, instance)
	assert.Error(t, err)

	var spawnErr *provider.ErrSpawnFailed
	assert.True(t, errors.As(err, &spawnErr))
	assert.Contains(t, spawnErr.Reason, "container create failed")

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_StartFails_Cleanup(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create succeeds
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)

	// Mock container start fails
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(errors.New("port already in use"))

	// Mock cleanup
	mockClient.On("ContainerRemove", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	assert.Nil(t, instance)
	assert.Error(t, err)

	var spawnErr *provider.ErrSpawnFailed
	assert.True(t, errors.As(err, &spawnErr))
	assert.Contains(t, spawnErr.Reason, "container start failed")

	// Verify cleanup was called
	mockClient.AssertCalled(t, "ContainerRemove", ctx, "abc123", mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_NetworkAutoCreate(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network doesn't exist
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{}, nil)

	// Mock network create
	mockClient.On("NetworkCreate", ctx, "test-network", mock.Anything).
		Return(network.CreateResponse{ID: "net123"}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	// Verify network was created
	mockClient.AssertCalled(t, "NetworkCreate", ctx, "test-network", mock.Anything)
	mockClient.AssertExpectations(t)
}

func TestProvider_Destroy_Success(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock stop and remove
	mockClient.On("ContainerStop", ctx, "abc123", mock.Anything).Return(nil)
	mockClient.On("ContainerRemove", ctx, "abc123", mock.Anything).Return(nil)

	err := p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestProvider_Destroy_NotFound(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_nonexistent"

	// Mock container not found
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, nil)

	err := p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.Error(t, err)

	var notFoundErr *provider.ErrRunnerNotFound
	assert.True(t, errors.As(err, &notFoundErr))
	assert.Equal(t, runnerID, notFoundErr.RunnerID)

	mockClient.AssertExpectations(t)
}

func TestProvider_Status_Running(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock inspect
	mockClient.On("ContainerInspect", ctx, "abc123").Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &container.State{
				Running: true,
			},
		},
	}, nil)

	status, err := p.Status(ctx, runnerID)

	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusRunning, status.Status)
	mockClient.AssertExpectations(t)
}

func TestProvider_Status_Paused(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock inspect - paused (note: both Paused and Running are true when paused)
	mockClient.On("ContainerInspect", ctx, "abc123").Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &container.State{
				Paused:  true,
				Running: true, // Docker sets both to true when paused
			},
		},
	}, nil)

	status, err := p.Status(ctx, runnerID)

	require.NoError(t, err)
	assert.Equal(t, provider.InstanceStatusPaused, status.Status)
	mockClient.AssertExpectations(t)
}

func TestProvider_List(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock container list
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{
			ID:      "abc123",
			Names:   []string{"/marionette-runner1"},
			State:   "running",
			Created: time.Now().Unix(),
			Labels:  map[string]string{"marionette.dev/runner-id": "run_123"},
		},
		{
			ID:      "def456",
			Names:   []string{"/marionette-runner2"},
			State:   "paused",
			Created: time.Now().Unix(),
			Labels:  map[string]string{"marionette.dev/runner-id": "run_456"},
		},
	}, nil)

	instances, err := p.List(ctx)

	require.NoError(t, err)
	assert.Len(t, instances, 2)
	assert.Equal(t, "run_123", instances[0].ID)
	assert.Equal(t, provider.InstanceStatusRunning, instances[0].Status)
	assert.Equal(t, "run_456", instances[1].ID)
	assert.Equal(t, provider.InstanceStatusPaused, instances[1].Status)

	mockClient.AssertExpectations(t)
}

func TestProvider_Pause_Success(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock pause
	mockClient.On("ContainerPause", ctx, "abc123").Return(nil)

	err := p.Pause(ctx, runnerID)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestProvider_Unpause_Success(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock unpause
	mockClient.On("ContainerUnpause", ctx, "abc123").Return(nil)

	err := p.Unpause(ctx, runnerID)

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

// Config tests

func TestConfig_ParseMemory(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"2g", 2 * 1024 * 1024 * 1024, false},
		{"2G", 2 * 1024 * 1024 * 1024, false},
		{"2gb", 2 * 1024 * 1024 * 1024, false},
		{"2048m", 2048 * 1024 * 1024, false},
		{"2048M", 2048 * 1024 * 1024, false},
		{"1024k", 1024 * 1024, false},
		{"2147483648", 2147483648, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"2x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseMemory(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestConfig_ParseCPUs(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		hasError bool
	}{
		{"2", 2.0, false},
		{"1.5", 1.5, false},
		{"0.5", 0.5, false},
		{"4", 4.0, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"0", 0, true},
		{"-1", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseCPUs(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg, err := ParseConfig(nil)

	require.NoError(t, err)
	assert.Equal(t, DefaultHost, cfg.Host)
	assert.Equal(t, DefaultImage, cfg.Image)
	assert.Equal(t, DefaultLabelPrefix, cfg.LabelPrefix)
	assert.Equal(t, DefaultMemory, cfg.Resources.Memory)
	assert.Equal(t, DefaultCPUs, cfg.Resources.CPUs)
}

func TestConfig_Parse(t *testing.T) {
	jsonData := []byte(`{
		"host": "tcp://localhost:2376",
		"image": "custom/agent:v1",
		"network": "custom-net",
		"resources": {
			"memory": "4g",
			"cpus": "4"
		}
	}`)

	cfg, err := ParseConfig(jsonData)

	require.NoError(t, err)
	assert.Equal(t, "tcp://localhost:2376", cfg.Host)
	assert.Equal(t, "custom/agent:v1", cfg.Image)
	assert.Equal(t, "custom-net", cfg.Network)
	assert.Equal(t, "4g", cfg.Resources.Memory)
	assert.Equal(t, "4", cfg.Resources.CPUs)
}

func TestConfig_Validation(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		hasError bool
	}{
		{
			name:     "invalid host",
			json:     `{"host": "invalid://localhost"}`,
			hasError: true,
		},
		{
			name:     "invalid memory",
			json:     `{"resources": {"memory": "invalid"}}`,
			hasError: true,
		},
		{
			name:     "invalid cpus",
			json:     `{"resources": {"cpus": "invalid"}}`,
			hasError: true,
		},
		{
			name:     "valid config",
			json:     `{"host": "unix:///var/run/docker.sock"}`,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.json))
			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSuspendConfig_Defaults(t *testing.T) {
	cfg, err := provider.ParseSuspendConfig(nil, defaultSuspendConfig())

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, cfg.Strategy)
	assert.Equal(t, 60*time.Second, cfg.MinDuration)
	assert.Equal(t, 24*time.Hour, cfg.MaxDuration)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Fallback)
}

func TestSuspendConfig_Parse(t *testing.T) {
	jsonData := []byte(`{
		"strategy": "terminate_preserve_storage",
		"min_duration": "30s",
		"max_duration": "1h",
		"fallback": "terminate"
	}`)

	cfg, err := provider.ParseSuspendConfig(jsonData, defaultSuspendConfig())

	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Strategy)
	assert.Equal(t, 30*time.Second, cfg.MinDuration)
	assert.Equal(t, 1*time.Hour, cfg.MaxDuration)
	assert.Equal(t, provider.SuspendStrategyTerminate, cfg.Fallback)
}

// Additional config tests for coverage

func TestConfig_MemoryMB(t *testing.T) {
	cfg := &Config{
		Resources: ResourceConfig{Memory: "2g"},
	}

	mb, err := cfg.MemoryMB()

	require.NoError(t, err)
	assert.Equal(t, 2048, mb)
}

func TestConfig_MemoryMB_Invalid(t *testing.T) {
	cfg := &Config{
		Resources: ResourceConfig{Memory: "invalid"},
	}

	_, err := cfg.MemoryMB()

	assert.Error(t, err)
}

func TestConfig_CPUs(t *testing.T) {
	cfg := &Config{
		Resources: ResourceConfig{CPUs: "2.5"},
	}

	cpus, err := cfg.CPUs()

	require.NoError(t, err)
	assert.Equal(t, 2.5, cpus)
}

func TestConfig_CPUs_Invalid(t *testing.T) {
	cfg := &Config{
		Resources: ResourceConfig{CPUs: "invalid"},
	}

	_, err := cfg.CPUs()

	assert.Error(t, err)
}

func TestProvider_Pause_NotFound(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_nonexistent"

	// Mock container not found
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, nil)

	err := p.Pause(ctx, runnerID)

	assert.Error(t, err)
	var notFoundErr *provider.ErrRunnerNotFound
	assert.True(t, errors.As(err, &notFoundErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Pause_Fails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock pause fails
	mockClient.On("ContainerPause", ctx, "abc123").Return(errors.New("container not running"))

	err := p.Pause(ctx, runnerID)

	assert.Error(t, err)
	var pauseErr *provider.ErrPauseFailed
	assert.True(t, errors.As(err, &pauseErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Unpause_NotFound(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_nonexistent"

	// Mock container not found
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, nil)

	err := p.Unpause(ctx, runnerID)

	assert.Error(t, err)
	var notFoundErr *provider.ErrRunnerNotFound
	assert.True(t, errors.As(err, &notFoundErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Unpause_Fails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock unpause fails
	mockClient.On("ContainerUnpause", ctx, "abc123").Return(errors.New("container not paused"))

	err := p.Unpause(ctx, runnerID)

	assert.Error(t, err)
	var unpauseErr *provider.ErrUnpauseFailed
	assert.True(t, errors.As(err, &unpauseErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Destroy_StopFails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock stop fails with non-"not running" error
	mockClient.On("ContainerStop", ctx, "abc123", mock.Anything).Return(errors.New("timeout"))

	err := p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.Error(t, err)
	var destroyErr *provider.ErrDestroyFailed
	assert.True(t, errors.As(err, &destroyErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Destroy_StopNotRunning_RemoveSucceeds(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock stop fails with "not running" error
	mockClient.On("ContainerStop", ctx, "abc123", mock.Anything).Return(errors.New("container is not running"))

	// Mock remove succeeds
	mockClient.On("ContainerRemove", ctx, "abc123", mock.Anything).Return(nil)

	err := p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestProvider_Destroy_RemoveFails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock stop succeeds
	mockClient.On("ContainerStop", ctx, "abc123", mock.Anything).Return(nil)

	// Mock remove fails
	mockClient.On("ContainerRemove", ctx, "abc123", mock.Anything).Return(errors.New("permission denied"))

	err := p.Destroy(ctx, runnerID, provider.DestroyOptions{})

	assert.Error(t, err)
	var destroyErr *provider.ErrDestroyFailed
	assert.True(t, errors.As(err, &destroyErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Status_NotFound(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_nonexistent"

	// Mock container not found
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, nil)

	status, err := p.Status(ctx, runnerID)

	assert.Nil(t, status)
	assert.Error(t, err)
	var notFoundErr *provider.ErrRunnerNotFound
	assert.True(t, errors.As(err, &notFoundErr))

	mockClient.AssertExpectations(t)
}

func TestProvider_Status_InspectFails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	runnerID := "run_test123"

	// Mock finding container
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{
		{ID: "abc123", Labels: map[string]string{"marionette.dev/runner-id": runnerID}},
	}, nil)

	// Mock inspect fails
	mockClient.On("ContainerInspect", ctx, "abc123").Return(types.ContainerJSON{}, errors.New("inspect failed"))

	status, err := p.Status(ctx, runnerID)

	assert.Nil(t, status)
	assert.Error(t, err)

	mockClient.AssertExpectations(t)
}

func TestProvider_List_Error(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock list fails
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, errors.New("docker daemon unreachable"))

	instances, err := p.List(ctx)

	assert.Nil(t, instances)
	assert.Error(t, err)

	mockClient.AssertExpectations(t)
}

func TestProvider_Close(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)

	mockClient.On("Close").Return(nil)

	err := p.Close()

	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestProvider_SuspendConfig(t *testing.T) {
	mockClient := new(MockDockerClient)
	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "marionette/agent:latest",
		LabelPrefix: "marionette.dev",
		Resources:   ResourceConfig{Memory: "2g", CPUs: "2"},
	}
	suspendCfg := &provider.SuspendConfig{
		Strategy:    provider.SuspendStrategyPause,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    provider.SuspendStrategyTerminate,
	}
	p := NewWithClient("test", cfg, suspendCfg, mockClient)

	sc := p.SuspendConfig()

	assert.Equal(t, provider.SuspendStrategyPause, sc.Strategy)
	assert.Equal(t, 60*time.Second, sc.MinDuration)
}

// mapContainerState tests for all states

func TestMapContainerState_NilState(t *testing.T) {
	status := mapContainerState(nil)
	assert.Equal(t, provider.InstanceStatusFailed, status)
}

func TestMapContainerState_Dead(t *testing.T) {
	state := &container.State{Dead: true}
	status := mapContainerState(state)
	assert.Equal(t, provider.InstanceStatusFailed, status)
}

func TestMapContainerState_OOMKilled(t *testing.T) {
	state := &container.State{OOMKilled: true}
	status := mapContainerState(state)
	assert.Equal(t, provider.InstanceStatusFailed, status)
}

func TestMapContainerState_Created(t *testing.T) {
	state := &container.State{Status: "created"}
	status := mapContainerState(state)
	assert.Equal(t, provider.InstanceStatusPending, status)
}

func TestMapContainerState_Stopped(t *testing.T) {
	state := &container.State{Status: "exited"}
	status := mapContainerState(state)
	assert.Equal(t, provider.InstanceStatusStopped, status)
}

// mapContainerStateString tests for all states

func TestMapContainerStateString_Exited(t *testing.T) {
	status := mapContainerStateString("exited")
	assert.Equal(t, provider.InstanceStatusStopped, status)
}

func TestMapContainerStateString_Dead(t *testing.T) {
	status := mapContainerStateString("dead")
	assert.Equal(t, provider.InstanceStatusStopped, status)
}

func TestMapContainerStateString_Unknown(t *testing.T) {
	status := mapContainerStateString("unknown")
	assert.Equal(t, provider.InstanceStatusFailed, status)
}

// isNotRunningError tests

func TestIsNotRunningError_Nil(t *testing.T) {
	assert.False(t, isNotRunningError(nil))
}

func TestIsNotRunningError_NotRunning(t *testing.T) {
	err := errors.New("container abc123 is not running")
	assert.True(t, isNotRunningError(err))
}

func TestIsNotRunningError_NoSuchContainer(t *testing.T) {
	err := errors.New("No such container: abc123")
	assert.True(t, isNotRunningError(err))
}

func TestIsNotRunningError_OtherError(t *testing.T) {
	err := errors.New("some other error")
	assert.False(t, isNotRunningError(err))
}

// Additional Spawn tests

func TestProvider_Spawn_WithEnvironment(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.MatchedBy(func(cfg *container.Config) bool {
		// Verify environment includes custom env vars
		for _, env := range cfg.Env {
			if env == "CUSTOM_VAR=custom_value" {
				return true
			}
		}
		return false
	}), mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		Environment: map[string]string{"CUSTOM_VAR": "custom_value"},
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_WithTenantID(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.MatchedBy(func(cfg *container.Config) bool {
		// Verify labels include tenant ID
		return cfg.Labels["marionette.dev/tenant-id"] == "tenant_123"
	}), mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		TenantID:    "tenant_123",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_WithWorkspaceMount(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.MatchedBy(func(hc *container.HostConfig) bool {
		// Verify mount is configured
		for _, m := range hc.Mounts {
			if m.Source == "/host/workspace" && m.Target == "/workspace" {
				return true
			}
		}
		return false
	}), mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:       "run_test123",
		Name:           "test-runner",
		ServerURL:      "localhost:9090",
		RunnerToken:    "test-token",
		WorkspaceMount: "/host/workspace",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_WithResourceOverrides(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.MatchedBy(func(hc *container.HostConfig) bool {
		// Verify resource overrides
		return hc.Resources.Memory == 4*1024*1024*1024 && hc.Resources.NanoCPUs == int64(4*1e9)
	}), mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
		MemoryMB:    4096,
		CPUs:        4.0,
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_NoNetwork(t *testing.T) {
	mockClient := new(MockDockerClient)
	cfg := &Config{
		Host:        "unix:///var/run/docker.sock",
		Image:       "marionette/agent:latest",
		Network:     "", // No network
		LabelPrefix: "marionette.dev",
		Resources:   ResourceConfig{Memory: "2g", CPUs: "2"},
	}
	p := NewWithClient("test", cfg, nil, mockClient)
	ctx := context.Background()

	// Mock container create and start (no network list needed)
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, (*network.NetworkingConfig)(nil), mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_NetworkCreateFails(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network doesn't exist
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{}, nil)

	// Mock network create fails
	mockClient.On("NetworkCreate", ctx, "test-network", mock.Anything).
		Return(network.CreateResponse{}, errors.New("network create failed"))

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "test-runner",
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	assert.Nil(t, instance)
	assert.Error(t, err)

	var spawnErr *provider.ErrSpawnFailed
	assert.True(t, errors.As(err, &spawnErr))
	assert.Contains(t, spawnErr.Reason, "network setup failed")

	mockClient.AssertExpectations(t)
}

func TestProvider_Spawn_ContainerNameWithoutName(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock network exists
	mockClient.On("NetworkList", ctx, mock.Anything).Return([]network.Summary{
		{Name: "test-network"},
	}, nil)

	// Mock container create and start - container name should be based on runnerID
	mockClient.On("ContainerCreate", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "marionette-run_test123").
		Return(container.CreateResponse{ID: "abc123"}, nil)
	mockClient.On("ContainerStart", ctx, "abc123", mock.Anything).
		Return(nil)

	opts := provider.SpawnOptions{
		RunnerID:    "run_test123",
		Name:        "", // No name, should use runnerID
		ServerURL:   "localhost:9090",
		RunnerToken: "test-token",
	}

	instance, err := p.Spawn(ctx, opts)

	require.NoError(t, err)
	assert.NotNil(t, instance)

	mockClient.AssertExpectations(t)
}

// Test findContainerByRunnerID list error
func TestProvider_FindContainer_ListError(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := newTestProvider(mockClient)
	ctx := context.Background()

	// Mock list fails
	mockClient.On("ContainerList", ctx, mock.Anything).Return([]types.Container{}, errors.New("docker error"))

	err := p.Destroy(ctx, "run_test123", provider.DestroyOptions{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "listing containers")

	mockClient.AssertExpectations(t)
}

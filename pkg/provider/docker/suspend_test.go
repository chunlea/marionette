package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/provider"
)

func TestProvider_Suspend_Pause(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, &provider.SuspendConfig{
		Strategy: provider.SuspendStrategyPause,
		Fallback: provider.SuspendStrategyTerminatePreserveStorage,
	}, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	containerID := "container123"

	// Setup mock
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	mockClient.On("ContainerPause", mock.Anything, containerID).Return(nil)

	result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{})
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyPause, result.Strategy)
	assert.NotZero(t, result.SuspendedAt)

	mockClient.AssertExpectations(t)
}

func TestProvider_Suspend_TerminatePreserveStorage(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, &provider.SuspendConfig{
		Strategy: provider.SuspendStrategyTerminatePreserveStorage,
	}, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	containerID := "container123"

	// Setup mock
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	timeout := 30
	mockClient.On("ContainerStop", mock.Anything, containerID, container.StopOptions{Timeout: &timeout}).Return(nil)

	result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{
		Strategy: provider.SuspendStrategyTerminatePreserveStorage,
	})
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, result.Strategy)

	mockClient.AssertExpectations(t)
}

func TestProvider_Suspend_Terminate(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, &provider.SuspendConfig{
		Strategy: provider.SuspendStrategyTerminate,
	}, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	containerID := "container123"

	// Setup mock for Destroy
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	timeout := 30
	mockClient.On("ContainerStop", mock.Anything, containerID, container.StopOptions{Timeout: &timeout}).Return(nil)
	mockClient.On("ContainerRemove", mock.Anything, containerID, container.RemoveOptions{
		RemoveVolumes: false,
		Force:         true,
	}).Return(nil)

	result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{
		Strategy: provider.SuspendStrategyTerminate,
	})
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminate, result.Strategy)

	mockClient.AssertExpectations(t)
}

func TestProvider_Suspend_StrategyNotSupported(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"

	_, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{
		Strategy: provider.SuspendStrategySnapshot, // Not supported by Docker
	})

	var strategyErr *provider.ErrStrategyNotSupported
	assert.True(t, errors.As(err, &strategyErr))
	assert.Equal(t, provider.SuspendStrategySnapshot, strategyErr.Strategy)
}

func TestProvider_Suspend_Fallback(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, &provider.SuspendConfig{
		Strategy: provider.SuspendStrategyPause,
		Fallback: provider.SuspendStrategyTerminatePreserveStorage,
	}, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	containerID := "container123"

	// Pause fails, should fallback to terminate_preserve_storage
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	mockClient.On("ContainerPause", mock.Anything, containerID).Return(errors.New("pause failed"))
	timeout := 30
	mockClient.On("ContainerStop", mock.Anything, containerID, container.StopOptions{Timeout: &timeout}).Return(nil)

	result, err := p.Suspend(ctx, runnerID, provider.SuspendOptions{})
	require.NoError(t, err)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, result.Strategy)

	mockClient.AssertExpectations(t)
}

func TestProvider_Resume_FromPaused(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	sessionID := "sess_test123"
	containerID := "container123"

	// Setup mocks
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	mockClient.On("ContainerInspect", mock.Anything, containerID).Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &container.State{
				Paused:  true,
				Running: true,
			},
		},
	}, nil)
	mockClient.On("ContainerUnpause", mock.Anything, containerID).Return(nil)

	instance, err := p.Resume(ctx, sessionID, provider.ResumeOptions{
		RunnerID: runnerID,
	})
	require.NoError(t, err)
	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)

	mockClient.AssertExpectations(t)
}

func TestProvider_Resume_FromStopped(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	sessionID := "sess_test123"
	containerID := "container123"

	// Setup mocks
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	mockClient.On("ContainerInspect", mock.Anything, containerID).Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &container.State{
				Running: false,
				Status:  "exited",
			},
		},
	}, nil)
	mockClient.On("ContainerStart", mock.Anything, containerID, container.StartOptions{}).Return(nil)

	instance, err := p.Resume(ctx, sessionID, provider.ResumeOptions{
		RunnerID: runnerID,
	})
	require.NoError(t, err)
	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)

	mockClient.AssertExpectations(t)
}

func TestProvider_Resume_AlreadyRunning(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	runnerID := "run_test123"
	sessionID := "sess_test123"
	containerID := "container123"

	// Setup mocks
	mockClient.On("ContainerList", mock.Anything, mock.Anything).Return([]types.Container{
		{ID: containerID, Labels: map[string]string{"marionette.io/runner-id": runnerID}},
	}, nil)
	mockClient.On("ContainerInspect", mock.Anything, containerID).Return(types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &container.State{
				Running: true,
				Paused:  false,
			},
		},
	}, nil)

	instance, err := p.Resume(ctx, sessionID, provider.ResumeOptions{
		RunnerID: runnerID,
	})
	require.NoError(t, err)
	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)

	mockClient.AssertExpectations(t)
}

func TestProvider_Resume_NoRunnerID(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	sessionID := "sess_test123"

	_, err := p.Resume(ctx, sessionID, provider.ResumeOptions{})

	var resumeErr *provider.ErrResumeFailed
	assert.True(t, errors.As(err, &resumeErr))
	assert.Equal(t, sessionID, resumeErr.SessionID)
}

func TestProvider_Resume_ContainerNotFound_SpawnNew(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	ctx := context.Background()
	runnerID := "run_new123"
	sessionID := "sess_test123"
	containerID := "newcontainer123"

	// First call: runner not found
	mockClient.On("ContainerList", mock.Anything, mock.MatchedBy(func(_ container.ListOptions) bool {
		return true
	})).Return([]types.Container{}, nil).Once()

	// Second call for Spawn
	mockClient.On("ContainerCreate", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(container.CreateResponse{ID: containerID}, nil)
	mockClient.On("ContainerStart", mock.Anything, containerID, container.StartOptions{}).Return(nil)

	instance, err := p.Resume(ctx, sessionID, provider.ResumeOptions{
		RunnerID: runnerID,
		SpawnOpts: &provider.SpawnOptions{
			RunnerID:    runnerID,
			Name:        "test-runner",
			ServerURL:   "localhost:9090",
			RunnerToken: "token123",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, runnerID, instance.ID)
	assert.Equal(t, provider.InstanceStatusRunning, instance.Status)

	mockClient.AssertExpectations(t)
}

func TestProvider_SupportsStrategy(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, nil, mockClient)

	assert.True(t, p.suspendDispatcher().Supports(provider.SuspendStrategyPause))
	assert.True(t, p.suspendDispatcher().Supports(provider.SuspendStrategyTerminatePreserveStorage))
	assert.True(t, p.suspendDispatcher().Supports(provider.SuspendStrategyTerminate))
	assert.False(t, p.suspendDispatcher().Supports(provider.SuspendStrategySnapshot))
	assert.False(t, p.suspendDispatcher().Supports(provider.SuspendStrategyReleaseToPool))
}

func TestProvider_SuspendConfigFromSuspend(t *testing.T) {
	mockClient := new(MockDockerClient)
	p := NewWithClient("test", &Config{
		Image:       "test:latest",
		LabelPrefix: "marionette.io",
	}, &provider.SuspendConfig{
		Strategy:    provider.SuspendStrategyPause,
		MinDuration: 60 * time.Second,
		MaxDuration: 24 * time.Hour,
		Fallback:    provider.SuspendStrategyTerminatePreserveStorage,
	}, mockClient)

	cfg := p.SuspendConfig()
	assert.Equal(t, provider.SuspendStrategyPause, cfg.Strategy)
	assert.Equal(t, 60*time.Second, cfg.MinDuration)
	assert.Equal(t, 24*time.Hour, cfg.MaxDuration)
	assert.Equal(t, provider.SuspendStrategyTerminatePreserveStorage, cfg.Fallback)
}

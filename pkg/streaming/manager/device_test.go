package manager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/chunlea/marionette/pkg/streaming"
)

// mockDeviceProvider implements both StreamProvider and DeviceEnumerator.
type mockDeviceProvider struct {
	name         string
	streamTypes  []streaming.StreamType
	devices      []streaming.DeviceInfo
	listErr      error
	getErr       error
	startErr     error
	stopErr      error
	getInfoErr   error
	startInfo    *streaming.StreamInfo
	stopCalled   bool
	getInfoCalls []string
}

func (m *mockDeviceProvider) Name() string {
	return m.name
}

func (m *mockDeviceProvider) SupportedTypes() []streaming.StreamType {
	return m.streamTypes
}

func (m *mockDeviceProvider) Start(_ context.Context, _ streaming.StreamOptions) (*streaming.StreamInfo, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	return m.startInfo, nil
}

func (m *mockDeviceProvider) Stop(_ context.Context, _ string) error {
	m.stopCalled = true
	return m.stopErr
}

func (m *mockDeviceProvider) GetInfo(_ context.Context, id string) (*streaming.StreamInfo, error) {
	m.getInfoCalls = append(m.getInfoCalls, id)
	if m.getInfoErr != nil {
		return nil, m.getInfoErr
	}
	return m.startInfo, nil
}

func (m *mockDeviceProvider) ListDevices(_ context.Context) ([]streaming.DeviceInfo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.devices, nil
}

func (m *mockDeviceProvider) GetDevice(_ context.Context, deviceID string) (*streaming.DeviceInfo, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for _, d := range m.devices {
		if d.ID == deviceID {
			return &d, nil
		}
	}
	return nil, streaming.ErrDeviceNotFound
}

func TestManager_ListDevices(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register a provider with devices
	provider := &mockDeviceProvider{
		name:        "test-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		devices: []streaming.DeviceInfo{
			{ID: "device1", Name: "Device 1", Type: streaming.StreamTypeAndroid, State: "connected"},
			{ID: "device2", Name: "Device 2", Type: streaming.StreamTypeAndroid, State: "connected"},
		},
	}
	err = manager.RegisterProvider(provider)
	require.NoError(t, err)

	// List devices
	devices, err := manager.ListDevices(context.Background())
	require.NoError(t, err)
	assert.Len(t, devices, 2)
	assert.Equal(t, "device1", devices[0].ID)
	assert.Equal(t, "device2", devices[1].ID)
}

func TestManager_ListDevices_MultipleProviders(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register two providers with devices
	provider1 := &mockDeviceProvider{
		name:        "android-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		devices: []streaming.DeviceInfo{
			{ID: "android1", Name: "Android 1", Type: streaming.StreamTypeAndroid, State: "connected"},
		},
	}
	provider2 := &mockDeviceProvider{
		name:        "ios-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeIOS},
		devices: []streaming.DeviceInfo{
			{ID: "ios1", Name: "iOS 1", Type: streaming.StreamTypeIOS, State: "connected"},
		},
	}
	err = manager.RegisterProvider(provider1)
	require.NoError(t, err)
	err = manager.RegisterProvider(provider2)
	require.NoError(t, err)

	// List devices (should get devices from both providers)
	devices, err := manager.ListDevices(context.Background())
	require.NoError(t, err)
	assert.Len(t, devices, 2)
}

func TestManager_ListDevices_NoDeviceEnumerator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register a provider without DeviceEnumerator (using regular mock)
	provider := newMockProvider("test-provider", []streaming.StreamType{streaming.StreamTypeDesktop})
	err = manager.RegisterProvider(provider)
	require.NoError(t, err)

	// List devices (should return empty slice since provider doesn't implement DeviceEnumerator)
	devices, err := manager.ListDevices(context.Background())
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestManager_ListDevices_ProviderError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register a provider that returns an error
	provider := &mockDeviceProvider{
		name:        "error-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		listErr:     assert.AnError,
	}
	err = manager.RegisterProvider(provider)
	require.NoError(t, err)

	// List devices (should return empty slice, error is logged but not returned)
	devices, err := manager.ListDevices(context.Background())
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestManager_ListDevices_Closed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Close the manager
	err = manager.Stop(context.Background())
	require.NoError(t, err)

	// List devices should fail
	_, err = manager.ListDevices(context.Background())
	assert.ErrorIs(t, err, streaming.ErrStreamClosed)
}

func TestManager_GetDevice(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register a provider with devices
	provider := &mockDeviceProvider{
		name:        "test-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		devices: []streaming.DeviceInfo{
			{ID: "device1", Name: "Device 1", Type: streaming.StreamTypeAndroid, State: "connected"},
			{ID: "device2", Name: "Device 2", Type: streaming.StreamTypeAndroid, State: "connected"},
		},
	}
	err = manager.RegisterProvider(provider)
	require.NoError(t, err)

	// Get specific device
	device, err := manager.GetDevice(context.Background(), "device1")
	require.NoError(t, err)
	assert.Equal(t, "device1", device.ID)
	assert.Equal(t, "Device 1", device.Name)
}

func TestManager_GetDevice_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register a provider with devices
	provider := &mockDeviceProvider{
		name:        "test-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		devices: []streaming.DeviceInfo{
			{ID: "device1", Name: "Device 1", Type: streaming.StreamTypeAndroid, State: "connected"},
		},
	}
	err = manager.RegisterProvider(provider)
	require.NoError(t, err)

	// Get device that doesn't exist
	_, err = manager.GetDevice(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, streaming.ErrDeviceNotFound)
}

func TestManager_GetDevice_Closed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Close the manager
	err = manager.Stop(context.Background())
	require.NoError(t, err)

	// Get device should fail
	_, err = manager.GetDevice(context.Background(), "device1")
	assert.ErrorIs(t, err, streaming.ErrStreamClosed)
}

func TestManager_ListDevicesByType(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Register providers with different device types
	androidProvider := &mockDeviceProvider{
		name:        "android-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeAndroid},
		devices: []streaming.DeviceInfo{
			{ID: "android1", Name: "Android 1", Type: streaming.StreamTypeAndroid, State: "connected"},
			{ID: "android2", Name: "Android 2", Type: streaming.StreamTypeAndroid, State: "connected"},
		},
	}
	iosProvider := &mockDeviceProvider{
		name:        "ios-provider",
		streamTypes: []streaming.StreamType{streaming.StreamTypeIOS},
		devices: []streaming.DeviceInfo{
			{ID: "ios1", Name: "iOS 1", Type: streaming.StreamTypeIOS, State: "connected"},
		},
	}
	err = manager.RegisterProvider(androidProvider)
	require.NoError(t, err)
	err = manager.RegisterProvider(iosProvider)
	require.NoError(t, err)

	// List only Android devices
	devices, err := manager.ListDevicesByType(context.Background(), streaming.StreamTypeAndroid)
	require.NoError(t, err)
	assert.Len(t, devices, 2)
	for _, d := range devices {
		assert.Equal(t, streaming.StreamTypeAndroid, d.Type)
	}

	// List only iOS devices
	devices, err = manager.ListDevicesByType(context.Background(), streaming.StreamTypeIOS)
	require.NoError(t, err)
	assert.Len(t, devices, 1)
	assert.Equal(t, streaming.StreamTypeIOS, devices[0].Type)
}

func TestManager_ListDevicesByType_Closed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	store := newMockStreamStore()
	manager, err := New(DefaultConfig(), store, logger)
	require.NoError(t, err)

	// Close the manager
	err = manager.Stop(context.Background())
	require.NoError(t, err)

	// List devices should fail
	_, err = manager.ListDevicesByType(context.Background(), streaming.StreamTypeAndroid)
	assert.ErrorIs(t, err, streaming.ErrStreamClosed)
}

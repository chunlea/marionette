package manager

import (
	"context"
	"slices"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// DeviceEnumerator is an optional interface that StreamProviders can implement
// to support device discovery (e.g., Android devices, iOS simulators).
type DeviceEnumerator interface {
	// ListDevices returns all available devices for this provider.
	ListDevices(ctx context.Context) ([]streaming.DeviceInfo, error)

	// GetDevice returns a specific device by ID.
	GetDevice(ctx context.Context, deviceID string) (*streaming.DeviceInfo, error)
}

// ListDevices returns devices from all providers that support enumeration.
func (m *Manager) ListDevices(ctx context.Context) ([]streaming.DeviceInfo, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, streaming.ErrStreamClosed
	}
	m.mu.RUnlock()

	var devices []streaming.DeviceInfo
	for _, p := range m.registry.List() {
		if enumerator, ok := p.(DeviceEnumerator); ok {
			providerDevices, err := enumerator.ListDevices(ctx)
			if err != nil {
				m.logger.Warn("failed to list devices from provider",
					zap.String("provider", p.Name()),
					zap.Error(err),
				)
				continue
			}
			devices = append(devices, providerDevices...)
		}
	}
	return devices, nil
}

// GetDevice returns a device by ID, searching across all providers.
func (m *Manager) GetDevice(ctx context.Context, deviceID string) (*streaming.DeviceInfo, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, streaming.ErrStreamClosed
	}
	m.mu.RUnlock()

	for _, p := range m.registry.List() {
		if enumerator, ok := p.(DeviceEnumerator); ok {
			device, err := enumerator.GetDevice(ctx, deviceID)
			if err == nil && device != nil {
				return device, nil
			}
		}
	}
	return nil, streaming.ErrDeviceNotFound
}

// ListDevicesByType returns devices from providers that support the given stream type.
func (m *Manager) ListDevicesByType(ctx context.Context, streamType streaming.StreamType) ([]streaming.DeviceInfo, error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, streaming.ErrStreamClosed
	}
	m.mu.RUnlock()

	var devices []streaming.DeviceInfo
	for _, p := range m.registry.List() {
		// Check if provider supports this type
		if !slices.Contains(p.SupportedTypes(), streamType) {
			continue
		}

		// Check if provider implements DeviceEnumerator
		if enumerator, ok := p.(DeviceEnumerator); ok {
			providerDevices, err := enumerator.ListDevices(ctx)
			if err != nil {
				m.logger.Warn("failed to list devices from provider",
					zap.String("provider", p.Name()),
					zap.Error(err),
				)
				continue
			}
			// Filter devices by type
			for _, d := range providerDevices {
				if d.Type == streamType {
					devices = append(devices, d)
				}
			}
		}
	}
	return devices, nil
}

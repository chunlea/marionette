package scrcpy

import (
	"context"
	"fmt"
	"maps"

	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/android"
)

// StreamProviderAdapter wraps the scrcpy Provider to implement
// the unified streaming.StreamProvider interface and DeviceEnumerator.
type StreamProviderAdapter struct {
	provider *Provider
}

// NewStreamProviderAdapter creates a new StreamProviderAdapter wrapping the given Provider.
func NewStreamProviderAdapter(provider *Provider) *StreamProviderAdapter {
	return &StreamProviderAdapter{provider: provider}
}

// Name returns the unique name of this provider.
func (a *StreamProviderAdapter) Name() string {
	return "android-scrcpy"
}

// SupportedTypes returns the stream types this provider supports.
func (a *StreamProviderAdapter) SupportedTypes() []streaming.StreamType {
	return []streaming.StreamType{streaming.StreamTypeAndroid}
}

// Start initiates a stream with the given options.
func (a *StreamProviderAdapter) Start(ctx context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	// Extract device serial from metadata
	deviceSerial, ok := opts.Metadata["device_serial"]
	if !ok || deviceSerial == "" {
		return nil, fmt.Errorf("device_serial is required in metadata for Android streaming")
	}

	// Convert unified StreamOptions to Android StreamOptions
	androidOpts := android.StreamOptions{
		DeviceSerial: deviceSerial,
		MaxWidth:     opts.Resolution.Width,
		MaxHeight:    opts.Resolution.Height,
		Bitrate:      opts.BitRate,
		MaxFPS:       opts.FrameRate,
		AudioEnabled: opts.AudioEnabled,
		NoControl:    !opts.InputEnabled,
	}

	// Apply defaults
	androidOpts = androidOpts.WithDefaults()

	// Start the stream
	info, err := a.provider.StartStream(ctx, androidOpts)
	if err != nil {
		return nil, err
	}

	// Build metadata for the response
	metadata := make(map[string]string)
	if opts.Metadata != nil {
		maps.Copy(metadata, opts.Metadata)
	}
	metadata["device_serial"] = deviceSerial
	if info.Device != nil {
		metadata["device_name"] = info.Device.Model
		metadata["android_version"] = info.Device.AndroidVersion
	}

	// Convert Android StreamInfo to unified StreamInfo
	return &streaming.StreamInfo{
		ID:           info.ID,
		SignalingURL: fmt.Sprintf("/streams/%s/ws", info.ID),
		Resolution: streaming.Resolution{
			Width:  info.Width,
			Height: info.Height,
		},
		FrameRate:  androidOpts.MaxFPS,
		BitRate:    androidOpts.Bitrate,
		VideoCodec: androidOpts.VideoCodec,
		AudioCodec: androidOpts.AudioCodec,
		Metadata:   metadata,
	}, nil
}

// Stop stops the stream with the given provider stream ID.
func (a *StreamProviderAdapter) Stop(ctx context.Context, providerStreamID string) error {
	return a.provider.StopStream(ctx, providerStreamID)
}

// GetInfo returns the current info for a stream.
func (a *StreamProviderAdapter) GetInfo(ctx context.Context, providerStreamID string) (*streaming.StreamInfo, error) {
	info, err := a.provider.GetStream(ctx, providerStreamID)
	if err != nil {
		return nil, err
	}

	// Build metadata
	metadata := make(map[string]string)
	if info.Device != nil {
		metadata["device_serial"] = info.Device.Serial
		metadata["device_name"] = info.Device.Model
		metadata["android_version"] = info.Device.AndroidVersion
	}
	if info.Options != nil {
		metadata["video_codec"] = info.Options.VideoCodec
		metadata["audio_codec"] = info.Options.AudioCodec
	}

	// Convert to unified StreamInfo
	return &streaming.StreamInfo{
		ID:           info.ID,
		SignalingURL: fmt.Sprintf("/streams/%s/ws", info.ID),
		Resolution: streaming.Resolution{
			Width:  info.Width,
			Height: info.Height,
		},
		FrameRate:  info.Options.MaxFPS,
		BitRate:    info.Options.Bitrate,
		VideoCodec: info.Options.VideoCodec,
		AudioCodec: info.Options.AudioCodec,
		Metadata:   metadata,
	}, nil
}

// ListDevices returns all available Android devices.
// This implements the DeviceEnumerator interface.
func (a *StreamProviderAdapter) ListDevices(ctx context.Context) ([]streaming.DeviceInfo, error) {
	devices, err := a.provider.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]streaming.DeviceInfo, len(devices))
	for i, d := range devices {
		result[i] = convertDevice(d)
	}
	return result, nil
}

// GetDevice returns a specific device by ID (serial number).
// This implements the DeviceEnumerator interface.
func (a *StreamProviderAdapter) GetDevice(ctx context.Context, deviceID string) (*streaming.DeviceInfo, error) {
	device, err := a.provider.GetDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	info := convertDevice(*device)
	return &info, nil
}

// convertDevice converts an Android Device to a streaming DeviceInfo.
func convertDevice(d android.Device) streaming.DeviceInfo {
	// Determine display name
	name := d.Model
	if name == "" {
		name = d.Product
	}
	if name == "" {
		name = d.Device
	}
	if name == "" {
		name = d.Serial
	}

	// Build metadata
	metadata := map[string]string{
		"serial":          d.Serial,
		"android_version": d.AndroidVersion,
		"screen_size":     d.ScreenSize,
	}
	if d.IsEmulator {
		metadata["is_emulator"] = "true"
	}
	if d.TransportID != "" {
		metadata["transport_id"] = d.TransportID
	}
	if d.ScreenDensity > 0 {
		metadata["screen_density"] = fmt.Sprintf("%d", d.ScreenDensity)
	}

	return streaming.DeviceInfo{
		ID:       d.Serial,
		Name:     name,
		Type:     streaming.StreamTypeAndroid,
		State:    string(d.State),
		Metadata: metadata,
	}
}

// GetProvider returns the underlying scrcpy Provider.
func (a *StreamProviderAdapter) GetProvider() *Provider {
	return a.provider
}

// SendInput sends an input event to a device.
// This is Android-specific and not part of the unified interface,
// but is exposed for use by handlers that need input forwarding.
func (a *StreamProviderAdapter) SendInput(ctx context.Context, serial string, event android.InputEvent) error {
	return a.provider.SendInput(ctx, serial, event)
}

// Ensure StreamProviderAdapter implements streaming.StreamProvider.
var _ streaming.StreamProvider = (*StreamProviderAdapter)(nil)

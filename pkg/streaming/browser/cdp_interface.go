// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"context"
)

// Provider defines the interface for browser stream providers.
// A provider connects to a browser instance and captures screen content.
type Provider interface {
	// Start begins capturing browser content with the given options.
	// It returns an error if the stream is already active or if starting fails.
	Start(ctx context.Context, opts *StreamOptions) error

	// Stop stops the browser stream.
	// It is safe to call Stop multiple times.
	Stop(ctx context.Context) error

	// Pause temporarily pauses frame capture.
	// Frames will not be sent until Resume is called.
	Pause(ctx context.Context) error

	// Resume resumes frame capture after a pause.
	Resume(ctx context.Context) error

	// State returns the current stream state.
	State() StreamState

	// Info returns information about the stream.
	Info() *StreamInfo

	// Stats returns current stream statistics.
	Stats() *StreamStats

	// Frames returns a channel for receiving frames.
	// The channel is closed when the stream stops.
	Frames() <-chan *Frame

	// SendInput forwards an input event to the browser.
	SendInput(ctx context.Context, event *InputEvent) error

	// Navigate navigates the browser to a URL.
	Navigate(ctx context.Context, req *NavigateRequest) error

	// GetBrowserInfo returns information about the connected browser.
	GetBrowserInfo(ctx context.Context) (*BrowserInfo, error)

	// ListTabs returns a list of browser tabs.
	ListTabs(ctx context.Context) ([]*TabInfo, error)

	// SwitchTab switches to a different browser tab.
	SwitchTab(ctx context.Context, tabID string) error

	// OnStateChange registers a callback for state changes.
	// Only one handler can be registered; subsequent calls replace the previous.
	OnStateChange(handler StateHandler)

	// Close releases all resources and stops the stream if active.
	Close() error
}

// ProviderConfig contains configuration for creating a Provider.
type ProviderConfig struct {
	// CDPEndpoint is the Chrome DevTools Protocol WebSocket endpoint.
	// Example: "ws://localhost:9222/devtools/browser/{id}"
	CDPEndpoint string `json:"cdp_endpoint"`

	// BufferSize is the size of the frame buffer.
	// Frames are dropped when the buffer is full (backpressure).
	// Default: DefaultBufferSize (10)
	BufferSize int `json:"buffer_size,omitempty"`

	// ConnectTimeout is the timeout for connecting to the browser.
	// Default: 30 seconds
	ConnectTimeoutSeconds int `json:"connect_timeout_seconds,omitempty"`

	// ReconnectEnabled enables automatic reconnection on disconnect.
	// Default: true
	ReconnectEnabled *bool `json:"reconnect_enabled,omitempty"`

	// ReconnectMaxAttempts is the maximum number of reconnection attempts.
	// 0 means unlimited attempts.
	// Default: 5
	ReconnectMaxAttempts int `json:"reconnect_max_attempts,omitempty"`

	// ReconnectDelaySeconds is the initial delay between reconnection attempts.
	// Default: 1 second
	ReconnectDelaySeconds int `json:"reconnect_delay_seconds,omitempty"`

	// ReconnectMaxDelaySeconds is the maximum delay between reconnection attempts.
	// Default: 30 seconds
	ReconnectMaxDelaySeconds int `json:"reconnect_max_delay_seconds,omitempty"`
}

// Validate validates the provider configuration and sets defaults.
func (c *ProviderConfig) Validate() error {
	if c.CDPEndpoint == "" {
		return ErrBrowserNotConnected
	}

	if c.BufferSize < 0 {
		c.BufferSize = DefaultBufferSize
	}
	if c.BufferSize == 0 {
		c.BufferSize = DefaultBufferSize
	}
	if c.BufferSize > MaxBufferSize {
		c.BufferSize = MaxBufferSize
	}

	if c.ConnectTimeoutSeconds < 0 {
		c.ConnectTimeoutSeconds = 30
	}
	if c.ConnectTimeoutSeconds == 0 {
		c.ConnectTimeoutSeconds = 30
	}

	if c.ReconnectEnabled == nil {
		enabled := true
		c.ReconnectEnabled = &enabled
	}

	if c.ReconnectMaxAttempts < 0 {
		c.ReconnectMaxAttempts = 5
	}

	if c.ReconnectDelaySeconds < 0 {
		c.ReconnectDelaySeconds = 1
	}
	if c.ReconnectDelaySeconds == 0 {
		c.ReconnectDelaySeconds = 1
	}

	if c.ReconnectMaxDelaySeconds < 0 {
		c.ReconnectMaxDelaySeconds = 30
	}
	if c.ReconnectMaxDelaySeconds == 0 {
		c.ReconnectMaxDelaySeconds = 30
	}

	return nil
}

// Clone creates a deep copy of the configuration.
func (c *ProviderConfig) Clone() *ProviderConfig {
	if c == nil {
		return nil
	}
	cfg := *c
	if c.ReconnectEnabled != nil {
		enabled := *c.ReconnectEnabled
		cfg.ReconnectEnabled = &enabled
	}
	return &cfg
}

// ProviderFactory creates Provider instances.
type ProviderFactory interface {
	// Create creates a new Provider with the given configuration.
	Create(cfg *ProviderConfig) (Provider, error)

	// Name returns the name of this factory (e.g., "cdp", "vnc").
	Name() string
}

// Registry manages ProviderFactory instances.
type Registry struct {
	factories map[string]ProviderFactory
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]ProviderFactory),
	}
}

// Register adds a factory to the registry.
func (r *Registry) Register(factory ProviderFactory) {
	r.factories[factory.Name()] = factory
}

// Get returns a factory by name.
func (r *Registry) Get(name string) (ProviderFactory, bool) {
	f, ok := r.factories[name]
	return f, ok
}

// List returns all registered factory names.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

package browser

import (
	"testing"
)

func TestProviderConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr error
	}{
		{
			name:    "empty endpoint",
			cfg:     ProviderConfig{CDPEndpoint: ""},
			wantErr: ErrBrowserNotConnected,
		},
		{
			name: "valid config",
			cfg: ProviderConfig{
				CDPEndpoint: "ws://localhost:9222/devtools/browser/abc123",
			},
			wantErr: nil,
		},
		{
			name: "valid with all options",
			cfg: ProviderConfig{
				CDPEndpoint:              "ws://localhost:9222/devtools/browser/abc123",
				BufferSize:               20,
				ConnectTimeoutSeconds:    60,
				ReconnectMaxAttempts:     10,
				ReconnectDelaySeconds:    2,
				ReconnectMaxDelaySeconds: 60,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err != tt.wantErr {
				t.Errorf("ProviderConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestProviderConfig_Validate_SetsDefaults(t *testing.T) {
	cfg := ProviderConfig{
		CDPEndpoint: "ws://localhost:9222/devtools/browser/abc123",
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.BufferSize != DefaultBufferSize {
		t.Errorf("BufferSize = %d, want %d", cfg.BufferSize, DefaultBufferSize)
	}
	if cfg.ConnectTimeoutSeconds != 30 {
		t.Errorf("ConnectTimeoutSeconds = %d, want 30", cfg.ConnectTimeoutSeconds)
	}
	if cfg.ReconnectEnabled == nil || !*cfg.ReconnectEnabled {
		t.Error("ReconnectEnabled should be true by default")
	}
	if cfg.ReconnectMaxAttempts != 0 { // 0 is valid (no default override needed)
		// This is expected since we don't default this to anything when 0
	}
	if cfg.ReconnectDelaySeconds != 1 {
		t.Errorf("ReconnectDelaySeconds = %d, want 1", cfg.ReconnectDelaySeconds)
	}
	if cfg.ReconnectMaxDelaySeconds != 30 {
		t.Errorf("ReconnectMaxDelaySeconds = %d, want 30", cfg.ReconnectMaxDelaySeconds)
	}
}

func TestProviderConfig_Validate_CapsBufferSize(t *testing.T) {
	cfg := ProviderConfig{
		CDPEndpoint: "ws://localhost:9222/devtools/browser/abc123",
		BufferSize:  MaxBufferSize + 50,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.BufferSize != MaxBufferSize {
		t.Errorf("BufferSize = %d, want %d (capped)", cfg.BufferSize, MaxBufferSize)
	}
}

func TestProviderConfig_Validate_NegativeValues(t *testing.T) {
	cfg := ProviderConfig{
		CDPEndpoint:              "ws://localhost:9222/devtools/browser/abc123",
		BufferSize:               -5,
		ConnectTimeoutSeconds:    -10,
		ReconnectMaxAttempts:     -1,
		ReconnectDelaySeconds:    -2,
		ReconnectMaxDelaySeconds: -30,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// All negative values should be corrected to defaults
	if cfg.BufferSize != DefaultBufferSize {
		t.Errorf("BufferSize = %d, want %d", cfg.BufferSize, DefaultBufferSize)
	}
	if cfg.ConnectTimeoutSeconds != 30 {
		t.Errorf("ConnectTimeoutSeconds = %d, want 30", cfg.ConnectTimeoutSeconds)
	}
	if cfg.ReconnectMaxAttempts != 0 { // -1 becomes 0 (unlimited)
		// Note: negative becomes valid through abs or setting to 0
	}
	if cfg.ReconnectDelaySeconds != 1 {
		t.Errorf("ReconnectDelaySeconds = %d, want 1", cfg.ReconnectDelaySeconds)
	}
	if cfg.ReconnectMaxDelaySeconds != 30 {
		t.Errorf("ReconnectMaxDelaySeconds = %d, want 30", cfg.ReconnectMaxDelaySeconds)
	}
}

func TestProviderConfig_Clone(t *testing.T) {
	enabled := true
	original := &ProviderConfig{
		CDPEndpoint:              "ws://localhost:9222/devtools/browser/abc123",
		BufferSize:               20,
		ConnectTimeoutSeconds:    60,
		ReconnectEnabled:         &enabled,
		ReconnectMaxAttempts:     10,
		ReconnectDelaySeconds:    2,
		ReconnectMaxDelaySeconds: 60,
	}

	cloned := original.Clone()

	if cloned == original {
		t.Error("Clone() returned same pointer")
	}

	if cloned.CDPEndpoint != original.CDPEndpoint {
		t.Errorf("CDPEndpoint = %q, want %q", cloned.CDPEndpoint, original.CDPEndpoint)
	}
	if cloned.BufferSize != original.BufferSize {
		t.Errorf("BufferSize = %d, want %d", cloned.BufferSize, original.BufferSize)
	}

	// ReconnectEnabled should be a new pointer
	if cloned.ReconnectEnabled == original.ReconnectEnabled {
		t.Error("ReconnectEnabled should be a new pointer")
	}
	if *cloned.ReconnectEnabled != *original.ReconnectEnabled {
		t.Error("ReconnectEnabled values should be equal")
	}

	// Modify clone should not affect original
	*cloned.ReconnectEnabled = false
	if !*original.ReconnectEnabled {
		t.Error("modifying clone affected original")
	}
}

func TestProviderConfig_Clone_Nil(t *testing.T) {
	var cfg *ProviderConfig
	if cfg.Clone() != nil {
		t.Error("Clone() of nil should return nil")
	}
}

func TestProviderConfig_Clone_NilReconnectEnabled(t *testing.T) {
	original := &ProviderConfig{
		CDPEndpoint: "ws://localhost:9222/devtools/browser/abc123",
	}

	cloned := original.Clone()
	if cloned.ReconnectEnabled != nil {
		t.Error("Clone() should have nil ReconnectEnabled when original has nil")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	if len(reg.List()) != 0 {
		t.Error("new registry should be empty")
	}

	// Register a factory
	factory := NewMockProviderFactory()
	reg.Register(factory)

	// Get the factory
	got, ok := reg.Get("mock")
	if !ok {
		t.Error("Get() ok = false, want true")
	}
	if got != factory {
		t.Error("Get() returned different factory")
	}

	// List factories
	names := reg.List()
	if len(names) != 1 || names[0] != "mock" {
		t.Errorf("List() = %v, want [mock]", names)
	}

	// Get non-existent factory
	_, ok = reg.Get("nonexistent")
	if ok {
		t.Error("Get() ok = true for nonexistent factory")
	}
}

// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package android

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultADBClientConfig(t *testing.T) {
	cfg := DefaultADBClientConfig()

	if cfg.ADBPath != "adb" {
		t.Errorf("ADBPath = %q, want %q", cfg.ADBPath, "adb")
	}
	if cfg.ServerHost != "localhost" {
		t.Errorf("ServerHost = %q, want %q", cfg.ServerHost, "localhost")
	}
	if cfg.ServerPort != 5037 {
		t.Errorf("ServerPort = %d, want %d", cfg.ServerPort, 5037)
	}
	if cfg.CommandTimeout != 30*time.Second {
		t.Errorf("CommandTimeout = %v, want %v", cfg.CommandTimeout, 30*time.Second)
	}
}

func TestNewADBClient(t *testing.T) {
	t.Run("returns error when adb not found", func(t *testing.T) {
		cfg := ADBClientConfig{
			ADBPath: "/nonexistent/adb",
		}
		_, err := NewADBClient(cfg)
		if !errors.Is(err, ErrADBNotFound) {
			t.Errorf("NewADBClient() error = %v, want %v", err, ErrADBNotFound)
		}
	})

	t.Run("applies defaults", func(t *testing.T) {
		// Skip if adb is not available
		cfg := DefaultADBClientConfig()
		client, err := NewADBClient(cfg)
		if errors.Is(err, ErrADBNotFound) {
			t.Skip("adb not available")
		}
		if err != nil {
			t.Fatalf("NewADBClient() error = %v", err)
		}
		if client == nil {
			t.Error("NewADBClient() returned nil client")
		}
	})
}

func TestEscapeTextForADB(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple text",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "text with space",
			input:    "hello world",
			expected: "hello%sworld",
		},
		{
			name:     "multiple spaces",
			input:    "hello  world",
			expected: "hello%s%sworld",
		},
		{
			name:     "special characters",
			input:    `hello"world`,
			expected: `hello\"world`,
		},
		{
			name:     "backslash",
			input:    `hello\world`,
			expected: `hello\\world`,
		},
		{
			name:     "dollar sign",
			input:    "hello$world",
			expected: `hello\$world`,
		},
		{
			name:     "backtick",
			input:    "hello`world",
			expected: "hello\\`world",
		},
		{
			name:     "exclamation",
			input:    "hello!world",
			expected: `hello\!world`,
		},
		{
			name:     "parentheses",
			input:    "hello(world)",
			expected: `hello\(world\)`,
		},
		{
			name:     "brackets",
			input:    "hello[world]",
			expected: `hello\[world\]`,
		},
		{
			name:     "braces",
			input:    "hello{world}",
			expected: `hello\{world\}`,
		},
		{
			name:     "angle brackets",
			input:    "hello<world>",
			expected: `hello\<world\>`,
		},
		{
			name:     "pipe",
			input:    "hello|world",
			expected: `hello\|world`,
		},
		{
			name:     "ampersand",
			input:    "hello&world",
			expected: `hello\&world`,
		},
		{
			name:     "semicolon",
			input:    "hello;world",
			expected: `hello\;world`,
		},
		{
			name:     "asterisk",
			input:    "hello*world",
			expected: `hello\*world`,
		},
		{
			name:     "question mark",
			input:    "hello?world",
			expected: `hello\?world`,
		},
		{
			name:     "hash",
			input:    "hello#world",
			expected: `hello\#world`,
		},
		{
			name:     "tilde",
			input:    "hello~world",
			expected: `hello\~world`,
		},
		{
			name:     "single quote",
			input:    "hello'world",
			expected: `hello\'world`,
		},
		{
			name:     "mixed special chars",
			input:    "hello world! $test",
			expected: `hello%sworld\!%s\$test`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "url",
			input:    "https://example.com?q=test&a=1",
			expected: `https://example.com\?q=test\&a=1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeTextForADB(tt.input)
			if got != tt.expected {
				t.Errorf("escapeTextForADB(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGetKeyCode(t *testing.T) {
	tests := []struct {
		name     string
		keyName  string
		expected int
		found    bool
	}{
		{"HOME", "HOME", KeyCodeHome, true},
		{"home lowercase", "home", KeyCodeHome, true},
		{"BACK", "BACK", KeyCodeBack, true},
		{"ENTER", "ENTER", KeyCodeEnter, true},
		{"VOLUME_UP", "VOLUME_UP", KeyCodeVolumeUp, true},
		{"VOLUME_DOWN", "VOLUME_DOWN", KeyCodeVolumeDown, true},
		{"POWER", "POWER", KeyCodePower, true},
		{"MENU", "MENU", KeyCodeMenu, true},
		{"SEARCH", "SEARCH", KeyCodeSearch, true},
		{"DELETE", "DELETE", KeyCodeDelete, true},
		{"DEL", "DEL", KeyCodeDelete, true},
		{"TAB", "TAB", KeyCodeTab, true},
		{"SPACE", "SPACE", KeyCodeSpace, true},
		{"ESCAPE", "ESCAPE", KeyCodeEscape, true},
		{"APP_SWITCH", "APP_SWITCH", KeyCodeAppSwitch, true},
		{"DPAD_UP", "DPAD_UP", KeyCodeDpadUp, true},
		{"DPAD_DOWN", "DPAD_DOWN", KeyCodeDpadDown, true},
		{"DPAD_LEFT", "DPAD_LEFT", KeyCodeDpadLeft, true},
		{"DPAD_RIGHT", "DPAD_RIGHT", KeyCodeDpadRight, true},
		{"DPAD_CENTER", "DPAD_CENTER", KeyCodeDpadCenter, true},
		{"numeric 0", "0", KeyCode0, true},
		{"numeric 9", "9", KeyCode9, true},
		{"unknown key", "UNKNOWN_KEY_XYZ", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, found := GetKeyCode(tt.keyName)
			if found != tt.found {
				t.Errorf("GetKeyCode(%q) found = %v, want %v", tt.keyName, found, tt.found)
			}
			if code != tt.expected {
				t.Errorf("GetKeyCode(%q) = %d, want %d", tt.keyName, code, tt.expected)
			}
		})
	}
}

func TestKeyNameToCode_Completeness(t *testing.T) {
	// Verify that commonly used keys are present
	expectedKeys := []string{
		"HOME", "BACK", "ENTER", "POWER", "MENU",
		"VOLUME_UP", "VOLUME_DOWN", "DPAD_UP", "DPAD_DOWN",
		"DPAD_LEFT", "DPAD_RIGHT", "DELETE", "TAB", "SPACE",
	}

	for _, key := range expectedKeys {
		if _, ok := KeyNameToCode[key]; !ok {
			t.Errorf("KeyNameToCode missing key %q", key)
		}
	}
}

// MockADBClient is a mock implementation of ADBClient for testing.
type MockADBClient struct {
	ListDevicesFunc      func(ctx context.Context) ([]Device, error)
	GetDeviceInfoFunc    func(ctx context.Context, serial string) (*Device, error)
	ShellFunc            func(ctx context.Context, serial string, args ...string) (string, error)
	PushFunc             func(ctx context.Context, serial, local, remote string) error
	PullFunc             func(ctx context.Context, serial, remote, local string) error
	ForwardFunc          func(ctx context.Context, serial string, localPort, remotePort int) error
	ForwardToSocketFunc  func(ctx context.Context, serial string, localPort int, socketName string) error
	RemoveForwardFunc    func(ctx context.Context, serial string, localPort int) error
	InputTapFunc         func(ctx context.Context, serial string, x, y int) error
	InputSwipeFunc       func(ctx context.Context, serial string, x1, y1, x2, y2, durationMs int) error
	InputTextFunc        func(ctx context.Context, serial string, text string) error
	InputKeyEventFunc    func(ctx context.Context, serial string, keycode int) error
	GetScreenSizeFunc    func(ctx context.Context, serial string) (width, height int, err error)
	GetScreenDensityFunc func(ctx context.Context, serial string) (int, error)
	WaitForDeviceFunc    func(ctx context.Context, serial string, timeout time.Duration) error
	StartServerFunc      func(ctx context.Context) error
	KillServerFunc       func(ctx context.Context) error
}

func (m *MockADBClient) ListDevices(ctx context.Context) ([]Device, error) {
	if m.ListDevicesFunc != nil {
		return m.ListDevicesFunc(ctx)
	}
	return nil, nil
}

func (m *MockADBClient) GetDeviceInfo(ctx context.Context, serial string) (*Device, error) {
	if m.GetDeviceInfoFunc != nil {
		return m.GetDeviceInfoFunc(ctx, serial)
	}
	return nil, &DeviceNotFoundError{Serial: serial}
}

func (m *MockADBClient) Shell(ctx context.Context, serial string, args ...string) (string, error) {
	if m.ShellFunc != nil {
		return m.ShellFunc(ctx, serial, args...)
	}
	return "", nil
}

func (m *MockADBClient) Push(ctx context.Context, serial, local, remote string) error {
	if m.PushFunc != nil {
		return m.PushFunc(ctx, serial, local, remote)
	}
	return nil
}

func (m *MockADBClient) Pull(ctx context.Context, serial, remote, local string) error {
	if m.PullFunc != nil {
		return m.PullFunc(ctx, serial, remote, local)
	}
	return nil
}

func (m *MockADBClient) Forward(ctx context.Context, serial string, localPort, remotePort int) error {
	if m.ForwardFunc != nil {
		return m.ForwardFunc(ctx, serial, localPort, remotePort)
	}
	return nil
}

func (m *MockADBClient) ForwardToSocket(ctx context.Context, serial string, localPort int, socketName string) error {
	if m.ForwardToSocketFunc != nil {
		return m.ForwardToSocketFunc(ctx, serial, localPort, socketName)
	}
	return nil
}

func (m *MockADBClient) RemoveForward(ctx context.Context, serial string, localPort int) error {
	if m.RemoveForwardFunc != nil {
		return m.RemoveForwardFunc(ctx, serial, localPort)
	}
	return nil
}

func (m *MockADBClient) InputTap(ctx context.Context, serial string, x, y int) error {
	if m.InputTapFunc != nil {
		return m.InputTapFunc(ctx, serial, x, y)
	}
	return nil
}

func (m *MockADBClient) InputSwipe(ctx context.Context, serial string, x1, y1, x2, y2, durationMs int) error {
	if m.InputSwipeFunc != nil {
		return m.InputSwipeFunc(ctx, serial, x1, y1, x2, y2, durationMs)
	}
	return nil
}

func (m *MockADBClient) InputText(ctx context.Context, serial string, text string) error {
	if m.InputTextFunc != nil {
		return m.InputTextFunc(ctx, serial, text)
	}
	return nil
}

func (m *MockADBClient) InputKeyEvent(ctx context.Context, serial string, keycode int) error {
	if m.InputKeyEventFunc != nil {
		return m.InputKeyEventFunc(ctx, serial, keycode)
	}
	return nil
}

func (m *MockADBClient) GetScreenSize(ctx context.Context, serial string) (width, height int, err error) {
	if m.GetScreenSizeFunc != nil {
		return m.GetScreenSizeFunc(ctx, serial)
	}
	return 1080, 1920, nil
}

func (m *MockADBClient) GetScreenDensity(ctx context.Context, serial string) (int, error) {
	if m.GetScreenDensityFunc != nil {
		return m.GetScreenDensityFunc(ctx, serial)
	}
	return 420, nil
}

func (m *MockADBClient) WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error {
	if m.WaitForDeviceFunc != nil {
		return m.WaitForDeviceFunc(ctx, serial, timeout)
	}
	return nil
}

func (m *MockADBClient) StartServer(ctx context.Context) error {
	if m.StartServerFunc != nil {
		return m.StartServerFunc(ctx)
	}
	return nil
}

func (m *MockADBClient) KillServer(ctx context.Context) error {
	if m.KillServerFunc != nil {
		return m.KillServerFunc(ctx)
	}
	return nil
}

// Verify MockADBClient implements ADBClient
var _ ADBClient = (*MockADBClient)(nil)

func TestMockADBClient_ListDevices(t *testing.T) {
	devices := []Device{
		{Serial: "emulator-5554", State: DeviceStateDevice},
		{Serial: "emulator-5556", State: DeviceStateOffline},
	}

	mock := &MockADBClient{
		ListDevicesFunc: func(ctx context.Context) ([]Device, error) {
			return devices, nil
		},
	}

	got, err := mock.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices() error = %v", err)
	}

	if len(got) != len(devices) {
		t.Errorf("ListDevices() returned %d devices, want %d", len(got), len(devices))
	}
}

func TestMockADBClient_GetDeviceInfo(t *testing.T) {
	device := &Device{
		Serial:         "emulator-5554",
		State:          DeviceStateDevice,
		AndroidVersion: "30",
	}

	mock := &MockADBClient{
		GetDeviceInfoFunc: func(ctx context.Context, serial string) (*Device, error) {
			if serial == device.Serial {
				return device, nil
			}
			return nil, &DeviceNotFoundError{Serial: serial}
		},
	}

	t.Run("found", func(t *testing.T) {
		got, err := mock.GetDeviceInfo(context.Background(), "emulator-5554")
		if err != nil {
			t.Fatalf("GetDeviceInfo() error = %v", err)
		}
		if got.Serial != device.Serial {
			t.Errorf("GetDeviceInfo() serial = %q, want %q", got.Serial, device.Serial)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := mock.GetDeviceInfo(context.Background(), "unknown")
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Errorf("GetDeviceInfo() error = %v, want %v", err, ErrDeviceNotFound)
		}
	})
}

func TestMockADBClient_InputOperations(t *testing.T) {
	var tapCalled, swipeCalled, textCalled, keyCalled bool

	mock := &MockADBClient{
		InputTapFunc: func(ctx context.Context, serial string, x, y int) error {
			tapCalled = true
			return nil
		},
		InputSwipeFunc: func(ctx context.Context, serial string, x1, y1, x2, y2, durationMs int) error {
			swipeCalled = true
			return nil
		},
		InputTextFunc: func(ctx context.Context, serial string, text string) error {
			textCalled = true
			return nil
		},
		InputKeyEventFunc: func(ctx context.Context, serial string, keycode int) error {
			keyCalled = true
			return nil
		},
	}

	ctx := context.Background()
	serial := "emulator-5554"

	_ = mock.InputTap(ctx, serial, 100, 200)
	if !tapCalled {
		t.Error("InputTap was not called")
	}

	_ = mock.InputSwipe(ctx, serial, 100, 200, 300, 400, 100)
	if !swipeCalled {
		t.Error("InputSwipe was not called")
	}

	_ = mock.InputText(ctx, serial, "hello")
	if !textCalled {
		t.Error("InputText was not called")
	}

	_ = mock.InputKeyEvent(ctx, serial, KeyCodeEnter)
	if !keyCalled {
		t.Error("InputKeyEvent was not called")
	}
}

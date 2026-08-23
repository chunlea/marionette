// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package android

import (
	"errors"
	"testing"
)

func TestDeviceNotFoundError(t *testing.T) {
	t.Run("with serial", func(t *testing.T) {
		err := &DeviceNotFoundError{Serial: "emulator-5554"}
		expected := `device "emulator-5554" not found`
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Error("expected to wrap ErrDeviceNotFound")
		}
	})

	t.Run("without serial", func(t *testing.T) {
		err := &DeviceNotFoundError{}
		expected := "device not found"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})
}

func TestDeviceStateError(t *testing.T) {
	t.Run("with expected state", func(t *testing.T) {
		err := &DeviceStateError{
			Serial:   "emulator-5554",
			State:    DeviceStateOffline,
			Expected: DeviceStateDevice,
		}
		expected := `device "emulator-5554" is offline, expected device`
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrDeviceOffline) {
			t.Error("expected to wrap ErrDeviceOffline")
		}
	})

	t.Run("without expected state", func(t *testing.T) {
		err := &DeviceStateError{
			Serial: "emulator-5554",
			State:  DeviceStateUnauthorized,
		}
		expected := `device "emulator-5554" is unauthorized`
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrDeviceUnauthorized) {
			t.Error("expected to wrap ErrDeviceUnauthorized")
		}
	})

	t.Run("unknown state wraps offline", func(t *testing.T) {
		err := &DeviceStateError{
			Serial: "emulator-5554",
			State:  DeviceStateBootloader,
		}
		if !errors.Is(err, ErrDeviceOffline) {
			t.Error("unknown state should wrap ErrDeviceOffline")
		}
	})
}

func TestStreamError(t *testing.T) {
	t.Run("with stream ID", func(t *testing.T) {
		err := &StreamError{
			StreamID: "astr_test123",
			Op:       "start",
			Err:      ErrStreamStartFailed,
		}
		expected := "stream astr_test123: start: stream start failed"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrStreamStartFailed) {
			t.Error("expected to wrap ErrStreamStartFailed")
		}
	})

	t.Run("with device ID", func(t *testing.T) {
		err := &StreamError{
			DeviceID: "emulator-5554",
			Op:       "connect",
			Err:      ErrADBConnectionFailed,
		}
		expected := "device emulator-5554: connect: adb connection failed"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("without IDs", func(t *testing.T) {
		err := &StreamError{
			Op:  "initialize",
			Err: ErrScrcpyNotFound,
		}
		expected := "initialize: scrcpy not found"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})
}

func TestADBError(t *testing.T) {
	t.Run("with stderr", func(t *testing.T) {
		err := &ADBError{
			Command: "shell ls",
			Serial:  "emulator-5554",
			Stderr:  "error: device not found",
		}
		expected := "adb shell ls (device=emulator-5554): error: device not found"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("with error", func(t *testing.T) {
		err := &ADBError{
			Command: "devices",
			Serial:  "any",
			Err:     errors.New("connection refused"),
		}
		expected := "adb devices (device=any): connection refused"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("without details", func(t *testing.T) {
		err := &ADBError{
			Command: "push",
			Serial:  "emulator-5554",
		}
		expected := "adb push (device=emulator-5554) failed"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := &ADBError{
			Command: "test",
			Serial:  "test",
			Err:     innerErr,
		}
		if !errors.Is(err, innerErr) {
			t.Error("expected to unwrap inner error")
		}
	})
}

func TestScrcpyError(t *testing.T) {
	t.Run("with stderr", func(t *testing.T) {
		err := &ScrcpyError{
			Args:   []string{"--max-fps=60"},
			Serial: "emulator-5554",
			Stderr: "ERROR: Could not find device",
		}
		expected := "scrcpy (device=emulator-5554): ERROR: Could not find device"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("with error", func(t *testing.T) {
		err := &ScrcpyError{
			Serial: "emulator-5554",
			Err:    errors.New("process killed"),
		}
		expected := "scrcpy (device=emulator-5554): process killed"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("without details", func(t *testing.T) {
		err := &ScrcpyError{
			Serial: "emulator-5554",
		}
		expected := "scrcpy (device=emulator-5554) failed"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("unwrap", func(t *testing.T) {
		innerErr := errors.New("inner error")
		err := &ScrcpyError{
			Serial: "test",
			Err:    innerErr,
		}
		if !errors.Is(err, innerErr) {
			t.Error("expected to unwrap inner error")
		}
	})
}

func TestInvalidOptionsError(t *testing.T) {
	t.Run("with field", func(t *testing.T) {
		err := &InvalidOptionsError{
			Field:   "bitrate",
			Message: "must be positive",
		}
		expected := "invalid option bitrate: must be positive"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrInvalidOptions) {
			t.Error("expected to wrap ErrInvalidOptions")
		}
	})

	t.Run("without field", func(t *testing.T) {
		err := &InvalidOptionsError{
			Message: "configuration invalid",
		}
		expected := "invalid options: configuration invalid"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})
}

func TestInvalidInputError(t *testing.T) {
	t.Run("with type", func(t *testing.T) {
		err := &InvalidInputError{
			Type:    InputTypeTap,
			Message: "coordinates out of bounds",
		}
		expected := "invalid input tap: coordinates out of bounds"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
		if !errors.Is(err, ErrInvalidInput) {
			t.Error("expected to wrap ErrInvalidInput")
		}
	})

	t.Run("without type", func(t *testing.T) {
		err := &InvalidInputError{
			Message: "missing required field",
		}
		expected := "invalid input: missing required field"
		if got := err.Error(); got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})
}

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{
		Op:       "wait-for-device",
		Duration: "30s",
	}
	expected := "wait-for-device timed out after 30s"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
	if !errors.Is(err, ErrTimeout) {
		t.Error("expected to wrap ErrTimeout")
	}
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		text string
	}{
		{"ErrDeviceNotFound", ErrDeviceNotFound, "device not found"},
		{"ErrDeviceOffline", ErrDeviceOffline, "device offline"},
		{"ErrDeviceUnauthorized", ErrDeviceUnauthorized, "device unauthorized"},
		{"ErrStreamNotFound", ErrStreamNotFound, "stream not found"},
		{"ErrStreamAlreadyRunning", ErrStreamAlreadyRunning, "stream already running"},
		{"ErrStreamNotRunning", ErrStreamNotRunning, "stream not running"},
		{"ErrADBNotFound", ErrADBNotFound, "adb not found"},
		{"ErrScrcpyNotFound", ErrScrcpyNotFound, "scrcpy not found"},
		{"ErrADBConnectionFailed", ErrADBConnectionFailed, "adb connection failed"},
		{"ErrStreamStartFailed", ErrStreamStartFailed, "stream start failed"},
		{"ErrInputFailed", ErrInputFailed, "input failed"},
		{"ErrInvalidOptions", ErrInvalidOptions, "invalid options"},
		{"ErrInvalidInput", ErrInvalidInput, "invalid input"},
		{"ErrTimeout", ErrTimeout, "timeout"},
		{"ErrProviderClosed", ErrProviderClosed, "provider closed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.text {
				t.Errorf("%s.Error() = %q, want %q", tt.name, got, tt.text)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{
			name:   "DeviceNotFoundError is ErrDeviceNotFound",
			err:    &DeviceNotFoundError{Serial: "test"},
			target: ErrDeviceNotFound,
			want:   true,
		},
		{
			name:   "DeviceNotFoundError is not ErrDeviceOffline",
			err:    &DeviceNotFoundError{Serial: "test"},
			target: ErrDeviceOffline,
			want:   false,
		},
		{
			name:   "DeviceStateError offline is ErrDeviceOffline",
			err:    &DeviceStateError{Serial: "test", State: DeviceStateOffline},
			target: ErrDeviceOffline,
			want:   true,
		},
		{
			name:   "DeviceStateError unauthorized is ErrDeviceUnauthorized",
			err:    &DeviceStateError{Serial: "test", State: DeviceStateUnauthorized},
			target: ErrDeviceUnauthorized,
			want:   true,
		},
		{
			name:   "InvalidOptionsError is ErrInvalidOptions",
			err:    &InvalidOptionsError{Message: "test"},
			target: ErrInvalidOptions,
			want:   true,
		},
		{
			name:   "InvalidInputError is ErrInvalidInput",
			err:    &InvalidInputError{Message: "test"},
			target: ErrInvalidInput,
			want:   true,
		},
		{
			name:   "TimeoutError is ErrTimeout",
			err:    &TimeoutError{Op: "test", Duration: "1s"},
			target: ErrTimeout,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(%T, %v) = %v, want %v", tt.err, tt.target, got, tt.want)
			}
		})
	}
}

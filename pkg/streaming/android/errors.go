// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package android

import (
	"errors"
	"fmt"
)

// Sentinel errors for Android streaming operations.
var (
	// ErrDeviceNotFound indicates the specified device was not found.
	ErrDeviceNotFound = errors.New("device not found")

	// ErrDeviceOffline indicates the device is offline.
	ErrDeviceOffline = errors.New("device offline")

	// ErrDeviceUnauthorized indicates USB debugging is not authorized.
	ErrDeviceUnauthorized = errors.New("device unauthorized")

	// ErrStreamNotFound indicates the stream was not found.
	ErrStreamNotFound = errors.New("stream not found")

	// ErrStreamAlreadyRunning indicates a stream is already running for the device.
	ErrStreamAlreadyRunning = errors.New("stream already running")

	// ErrStreamNotRunning indicates no stream is running for the device.
	ErrStreamNotRunning = errors.New("stream not running")

	// ErrADBNotFound indicates adb binary was not found.
	ErrADBNotFound = errors.New("adb not found")

	// ErrScrcpyNotFound indicates scrcpy binary was not found.
	ErrScrcpyNotFound = errors.New("scrcpy not found")

	// ErrADBConnectionFailed indicates ADB connection failed.
	ErrADBConnectionFailed = errors.New("adb connection failed")

	// ErrStreamStartFailed indicates stream failed to start.
	ErrStreamStartFailed = errors.New("stream start failed")

	// ErrInputFailed indicates input forwarding failed.
	ErrInputFailed = errors.New("input failed")

	// ErrInvalidOptions indicates invalid stream options.
	ErrInvalidOptions = errors.New("invalid options")

	// ErrInvalidInput indicates invalid input event.
	ErrInvalidInput = errors.New("invalid input")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("timeout")

	// ErrProviderClosed indicates the provider has been closed.
	ErrProviderClosed = errors.New("provider closed")
)

// DeviceNotFoundError provides details about which device was not found.
type DeviceNotFoundError struct {
	Serial string
}

func (e *DeviceNotFoundError) Error() string {
	if e.Serial == "" {
		return "device not found"
	}
	return fmt.Sprintf("device %q not found", e.Serial)
}

func (e *DeviceNotFoundError) Unwrap() error {
	return ErrDeviceNotFound
}

// DeviceStateError indicates the device is in an invalid state.
type DeviceStateError struct {
	Serial   string
	State    DeviceState
	Expected DeviceState
}

func (e *DeviceStateError) Error() string {
	if e.Expected != "" {
		return fmt.Sprintf("device %q is %s, expected %s", e.Serial, e.State, e.Expected)
	}
	return fmt.Sprintf("device %q is %s", e.Serial, e.State)
}

func (e *DeviceStateError) Unwrap() error {
	switch e.State {
	case DeviceStateOffline:
		return ErrDeviceOffline
	case DeviceStateUnauthorized:
		return ErrDeviceUnauthorized
	default:
		return ErrDeviceOffline
	}
}

// StreamError provides details about a streaming error.
type StreamError struct {
	StreamID string
	DeviceID string
	Op       string
	Err      error
}

func (e *StreamError) Error() string {
	if e.StreamID != "" {
		return fmt.Sprintf("stream %s: %s: %v", e.StreamID, e.Op, e.Err)
	}
	if e.DeviceID != "" {
		return fmt.Sprintf("device %s: %s: %v", e.DeviceID, e.Op, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

// ADBError represents an ADB command error.
type ADBError struct {
	Command string
	Serial  string
	Stderr  string
	Err     error
}

func (e *ADBError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("adb %s (device=%s): %s", e.Command, e.Serial, e.Stderr)
	}
	if e.Err != nil {
		return fmt.Sprintf("adb %s (device=%s): %v", e.Command, e.Serial, e.Err)
	}
	return fmt.Sprintf("adb %s (device=%s) failed", e.Command, e.Serial)
}

func (e *ADBError) Unwrap() error {
	return e.Err
}

// ScrcpyError represents a scrcpy command error.
type ScrcpyError struct {
	Args   []string
	Serial string
	Stderr string
	Err    error
}

func (e *ScrcpyError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("scrcpy (device=%s): %s", e.Serial, e.Stderr)
	}
	if e.Err != nil {
		return fmt.Sprintf("scrcpy (device=%s): %v", e.Serial, e.Err)
	}
	return fmt.Sprintf("scrcpy (device=%s) failed", e.Serial)
}

func (e *ScrcpyError) Unwrap() error {
	return e.Err
}

// InvalidOptionsError indicates invalid stream options.
type InvalidOptionsError struct {
	Field   string
	Message string
}

func (e *InvalidOptionsError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("invalid option %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("invalid options: %s", e.Message)
}

func (e *InvalidOptionsError) Unwrap() error {
	return ErrInvalidOptions
}

// InvalidInputError indicates an invalid input event.
type InvalidInputError struct {
	Type    InputType
	Message string
}

func (e *InvalidInputError) Error() string {
	if e.Type != "" {
		return fmt.Sprintf("invalid input %s: %s", e.Type, e.Message)
	}
	return fmt.Sprintf("invalid input: %s", e.Message)
}

func (e *InvalidInputError) Unwrap() error {
	return ErrInvalidInput
}

// TimeoutError indicates an operation timed out.
type TimeoutError struct {
	Op       string
	Duration string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s timed out after %s", e.Op, e.Duration)
}

func (e *TimeoutError) Unwrap() error {
	return ErrTimeout
}

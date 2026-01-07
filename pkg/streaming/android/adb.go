package android

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ADBClient provides methods for interacting with Android Debug Bridge.
type ADBClient interface {
	// ListDevices returns all connected devices.
	ListDevices(ctx context.Context) ([]Device, error)

	// GetDeviceInfo returns detailed information about a device.
	GetDeviceInfo(ctx context.Context, serial string) (*Device, error)

	// Shell executes a shell command on the device.
	Shell(ctx context.Context, serial string, args ...string) (string, error)

	// Push pushes a file to the device.
	Push(ctx context.Context, serial, local, remote string) error

	// Pull pulls a file from the device.
	Pull(ctx context.Context, serial, remote, local string) error

	// Forward sets up port forwarding from local to remote.
	Forward(ctx context.Context, serial string, localPort, remotePort int) error

	// RemoveForward removes port forwarding.
	RemoveForward(ctx context.Context, serial string, localPort int) error

	// InputTap sends a tap event.
	InputTap(ctx context.Context, serial string, x, y int) error

	// InputSwipe sends a swipe event.
	InputSwipe(ctx context.Context, serial string, x1, y1, x2, y2, durationMs int) error

	// InputText sends text input.
	InputText(ctx context.Context, serial string, text string) error

	// InputKeyEvent sends a key event.
	InputKeyEvent(ctx context.Context, serial string, keycode int) error

	// GetScreenSize returns the device screen size.
	GetScreenSize(ctx context.Context, serial string) (width, height int, err error)

	// GetScreenDensity returns the device screen density.
	GetScreenDensity(ctx context.Context, serial string) (int, error)

	// WaitForDevice waits for a device to be ready.
	WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error

	// StartServer starts the ADB server.
	StartServer(ctx context.Context) error

	// KillServer kills the ADB server.
	KillServer(ctx context.Context) error
}

// ADBClientConfig configures the ADB client.
type ADBClientConfig struct {
	// ADBPath is the path to the adb binary.
	// If empty, uses "adb" from PATH.
	ADBPath string

	// ServerHost is the ADB server host (default: "localhost").
	ServerHost string

	// ServerPort is the ADB server port (default: 5037).
	ServerPort int

	// CommandTimeout is the default timeout for ADB commands.
	CommandTimeout time.Duration

	// Logger is the logger instance.
	Logger *zap.Logger
}

// DefaultADBClientConfig returns a default configuration.
func DefaultADBClientConfig() ADBClientConfig {
	return ADBClientConfig{
		ADBPath:        "adb",
		ServerHost:     "localhost",
		ServerPort:     5037,
		CommandTimeout: 30 * time.Second,
	}
}

// adbClient is the default implementation of ADBClient.
type adbClient struct {
	cfg    ADBClientConfig
	logger *zap.Logger
}

// NewADBClient creates a new ADB client.
func NewADBClient(cfg ADBClientConfig) (ADBClient, error) {
	if cfg.ADBPath == "" {
		cfg.ADBPath = "adb"
	}
	if cfg.ServerHost == "" {
		cfg.ServerHost = "localhost"
	}
	if cfg.ServerPort == 0 {
		cfg.ServerPort = 5037
	}
	if cfg.CommandTimeout == 0 {
		cfg.CommandTimeout = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}

	// Verify adb is available
	_, err := exec.LookPath(cfg.ADBPath)
	if err != nil {
		return nil, ErrADBNotFound
	}

	return &adbClient{
		cfg:    cfg,
		logger: cfg.Logger.Named("adb"),
	}, nil
}

func (c *adbClient) exec(ctx context.Context, args ...string) (string, error) {
	// Add timeout if not set in context
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.CommandTimeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, c.cfg.ADBPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	c.logger.Debug("executing adb command",
		zap.Strings("args", args),
	)

	err := cmd.Run()
	if err != nil {
		return "", &ADBError{
			Command: strings.Join(args, " "),
			Stderr:  stderr.String(),
			Err:     err,
		}
	}

	return strings.TrimSpace(stdout.String()), nil
}

func (c *adbClient) execDevice(ctx context.Context, serial string, args ...string) (string, error) {
	fullArgs := append([]string{"-s", serial}, args...)
	return c.exec(ctx, fullArgs...)
}

func (c *adbClient) ListDevices(ctx context.Context) ([]Device, error) {
	output, err := c.exec(ctx, "devices", "-l")
	if err != nil {
		return nil, err
	}

	var devices []Device
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip header line "List of devices attached"
	if scanner.Scan() {
		// Skip first line
	}

	// Parse device lines
	// Format: <serial> <state> [properties...]
	// Example: emulator-5554 device product:sdk_gphone_x86_64 model:sdk_gphone_x86_64 device:generic_x86_64 transport_id:1
	deviceLineRe := regexp.MustCompile(`^(\S+)\s+(\S+)(.*)$`)
	propRe := regexp.MustCompile(`(\w+):(\S+)`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		matches := deviceLineRe.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		device := Device{
			Serial: matches[1],
			State:  DeviceState(matches[2]),
		}

		// Check if it's an emulator
		device.IsEmulator = strings.HasPrefix(device.Serial, "emulator-")

		// Parse properties
		props := propRe.FindAllStringSubmatch(matches[3], -1)
		for _, prop := range props {
			switch prop[1] {
			case "product":
				device.Product = prop[2]
			case "model":
				device.Model = prop[2]
			case "device":
				device.Device = prop[2]
			case "transport_id":
				device.TransportID = prop[2]
			}
		}

		devices = append(devices, device)
	}

	return devices, nil
}

func (c *adbClient) GetDeviceInfo(ctx context.Context, serial string) (*Device, error) {
	devices, err := c.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	for _, d := range devices {
		if d.Serial == serial {
			// Get additional info if device is connected
			if d.State.IsConnected() {
				// Get Android version
				version, err := c.Shell(ctx, serial, "getprop", "ro.build.version.sdk")
				if err == nil {
					d.AndroidVersion = strings.TrimSpace(version)
				}

				// Get screen size
				width, height, err := c.GetScreenSize(ctx, serial)
				if err == nil {
					d.ScreenSize = fmt.Sprintf("%dx%d", width, height)
				}

				// Get screen density
				density, err := c.GetScreenDensity(ctx, serial)
				if err == nil {
					d.ScreenDensity = density
				}
			}
			return &d, nil
		}
	}

	return nil, &DeviceNotFoundError{Serial: serial}
}

func (c *adbClient) Shell(ctx context.Context, serial string, args ...string) (string, error) {
	shellArgs := append([]string{"shell"}, args...)
	return c.execDevice(ctx, serial, shellArgs...)
}

func (c *adbClient) Push(ctx context.Context, serial, local, remote string) error {
	_, err := c.execDevice(ctx, serial, "push", local, remote)
	return err
}

func (c *adbClient) Pull(ctx context.Context, serial, remote, local string) error {
	_, err := c.execDevice(ctx, serial, "pull", remote, local)
	return err
}

func (c *adbClient) Forward(ctx context.Context, serial string, localPort, remotePort int) error {
	local := fmt.Sprintf("tcp:%d", localPort)
	remote := fmt.Sprintf("tcp:%d", remotePort)
	_, err := c.execDevice(ctx, serial, "forward", local, remote)
	return err
}

func (c *adbClient) RemoveForward(ctx context.Context, serial string, localPort int) error {
	local := fmt.Sprintf("tcp:%d", localPort)
	_, err := c.execDevice(ctx, serial, "forward", "--remove", local)
	return err
}

func (c *adbClient) InputTap(ctx context.Context, serial string, x, y int) error {
	_, err := c.Shell(ctx, serial, "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

func (c *adbClient) InputSwipe(ctx context.Context, serial string, x1, y1, x2, y2, durationMs int) error {
	args := []string{"input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2),
	}
	if durationMs > 0 {
		args = append(args, strconv.Itoa(durationMs))
	}
	_, err := c.Shell(ctx, serial, args...)
	return err
}

func (c *adbClient) InputText(ctx context.Context, serial string, text string) error {
	// Escape special characters for shell
	escaped := escapeTextForADB(text)
	_, err := c.Shell(ctx, serial, "input", "text", escaped)
	return err
}

func (c *adbClient) InputKeyEvent(ctx context.Context, serial string, keycode int) error {
	_, err := c.Shell(ctx, serial, "input", "keyevent", strconv.Itoa(keycode))
	return err
}

func (c *adbClient) GetScreenSize(ctx context.Context, serial string) (width, height int, err error) {
	// Try wm size first
	output, err := c.Shell(ctx, serial, "wm", "size")
	if err == nil {
		// Parse "Physical size: 1080x1920" or "Override size: 1080x1920"
		re := regexp.MustCompile(`(\d+)x(\d+)`)
		matches := re.FindStringSubmatch(output)
		if len(matches) >= 3 {
			width, _ = strconv.Atoi(matches[1])
			height, _ = strconv.Atoi(matches[2])
			return width, height, nil
		}
	}

	// Fallback to dumpsys
	output, err = c.Shell(ctx, serial, "dumpsys", "window", "displays", "|", "grep", "'init='")
	if err == nil {
		re := regexp.MustCompile(`init=(\d+)x(\d+)`)
		matches := re.FindStringSubmatch(output)
		if len(matches) >= 3 {
			width, _ = strconv.Atoi(matches[1])
			height, _ = strconv.Atoi(matches[2])
			return width, height, nil
		}
	}

	return 0, 0, fmt.Errorf("unable to determine screen size")
}

func (c *adbClient) GetScreenDensity(ctx context.Context, serial string) (int, error) {
	output, err := c.Shell(ctx, serial, "wm", "density")
	if err != nil {
		return 0, err
	}

	// Parse "Physical density: 420" or "Override density: 420"
	re := regexp.MustCompile(`density:\s*(\d+)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) >= 2 {
		return strconv.Atoi(matches[1])
	}

	return 0, fmt.Errorf("unable to determine screen density")
}

func (c *adbClient) WaitForDevice(ctx context.Context, serial string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-s", serial, "wait-for-device"}
	_, err := c.exec(ctx, args...)
	if ctx.Err() == context.DeadlineExceeded {
		return &TimeoutError{Op: "wait-for-device", Duration: timeout.String()}
	}
	return err
}

func (c *adbClient) StartServer(ctx context.Context) error {
	_, err := c.exec(ctx, "start-server")
	return err
}

func (c *adbClient) KillServer(ctx context.Context) error {
	_, err := c.exec(ctx, "kill-server")
	return err
}

// escapeTextForADB escapes text for ADB input text command.
func escapeTextForADB(text string) string {
	// Characters that need escaping in shell
	// Space needs to be replaced with %s
	// Special characters need backslash escape
	var builder strings.Builder
	for _, r := range text {
		switch r {
		case ' ':
			builder.WriteString("%s")
		case '\\', '"', '\'', '`', '$', '!', '(', ')', '[', ']', '{', '}', '<', '>', '|', '&', ';', '*', '?', '#', '~':
			builder.WriteRune('\\')
			builder.WriteRune(r)
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// Android Key Codes
// See: https://developer.android.com/reference/android/view/KeyEvent
const (
	KeyCodeUnknown        = 0
	KeyCodeSoftLeft       = 1
	KeyCodeSoftRight      = 2
	KeyCodeHome           = 3
	KeyCodeBack           = 4
	KeyCodeCall           = 5
	KeyCodeEndCall        = 6
	KeyCode0              = 7
	KeyCode1              = 8
	KeyCode2              = 9
	KeyCode3              = 10
	KeyCode4              = 11
	KeyCode5              = 12
	KeyCode6              = 13
	KeyCode7              = 14
	KeyCode8              = 15
	KeyCode9              = 16
	KeyCodeStar           = 17
	KeyCodePound          = 18
	KeyCodeDpadUp         = 19
	KeyCodeDpadDown       = 20
	KeyCodeDpadLeft       = 21
	KeyCodeDpadRight      = 22
	KeyCodeDpadCenter     = 23
	KeyCodeVolumeUp       = 24
	KeyCodeVolumeDown     = 25
	KeyCodePower          = 26
	KeyCodeCamera         = 27
	KeyCodeClear          = 28
	KeyCodeEnter          = 66
	KeyCodeDelete         = 67
	KeyCodeTab            = 61
	KeyCodeSpace          = 62
	KeyCodeMenu           = 82
	KeyCodeSearch         = 84
	KeyCodeMediaPlayPause = 85
	KeyCodeMediaStop      = 86
	KeyCodeMediaNext      = 87
	KeyCodeMediaPrevious  = 88
	KeyCodePageUp         = 92
	KeyCodePageDown       = 93
	KeyCodeEscape         = 111
	KeyCodeAppSwitch      = 187
)

// KeyNameToCode maps key names to Android key codes.
var KeyNameToCode = map[string]int{
	"UNKNOWN":          KeyCodeUnknown,
	"SOFT_LEFT":        KeyCodeSoftLeft,
	"SOFT_RIGHT":       KeyCodeSoftRight,
	"HOME":             KeyCodeHome,
	"BACK":             KeyCodeBack,
	"CALL":             KeyCodeCall,
	"ENDCALL":          KeyCodeEndCall,
	"0":                KeyCode0,
	"1":                KeyCode1,
	"2":                KeyCode2,
	"3":                KeyCode3,
	"4":                KeyCode4,
	"5":                KeyCode5,
	"6":                KeyCode6,
	"7":                KeyCode7,
	"8":                KeyCode8,
	"9":                KeyCode9,
	"STAR":             KeyCodeStar,
	"POUND":            KeyCodePound,
	"DPAD_UP":          KeyCodeDpadUp,
	"DPAD_DOWN":        KeyCodeDpadDown,
	"DPAD_LEFT":        KeyCodeDpadLeft,
	"DPAD_RIGHT":       KeyCodeDpadRight,
	"DPAD_CENTER":      KeyCodeDpadCenter,
	"VOLUME_UP":        KeyCodeVolumeUp,
	"VOLUME_DOWN":      KeyCodeVolumeDown,
	"POWER":            KeyCodePower,
	"CAMERA":           KeyCodeCamera,
	"CLEAR":            KeyCodeClear,
	"ENTER":            KeyCodeEnter,
	"DEL":              KeyCodeDelete,
	"DELETE":           KeyCodeDelete,
	"TAB":              KeyCodeTab,
	"SPACE":            KeyCodeSpace,
	"MENU":             KeyCodeMenu,
	"SEARCH":           KeyCodeSearch,
	"MEDIA_PLAY_PAUSE": KeyCodeMediaPlayPause,
	"MEDIA_STOP":       KeyCodeMediaStop,
	"MEDIA_NEXT":       KeyCodeMediaNext,
	"MEDIA_PREVIOUS":   KeyCodeMediaPrevious,
	"PAGE_UP":          KeyCodePageUp,
	"PAGE_DOWN":        KeyCodePageDown,
	"ESCAPE":           KeyCodeEscape,
	"APP_SWITCH":       KeyCodeAppSwitch,
}

// GetKeyCode returns the key code for a key name.
func GetKeyCode(name string) (int, bool) {
	code, ok := KeyNameToCode[strings.ToUpper(name)]
	return code, ok
}

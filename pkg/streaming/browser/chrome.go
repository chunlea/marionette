// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

package browser

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ChromeConfig contains configuration for launching Chrome.
type ChromeConfig struct {
	// BinaryPath is the path to the Chrome binary.
	// If empty, auto-detection will be used.
	BinaryPath string

	// UserDataDir is the directory for Chrome user data.
	// If empty, a temporary directory will be created.
	UserDataDir string

	// Headless enables headless mode.
	Headless bool

	// RemoteDebuggingPort is the port for CDP.
	// If 0, a random available port will be used.
	RemoteDebuggingPort int

	// WindowSize sets the initial window size.
	WindowSize *WindowSize

	// ExtraArgs are additional command-line arguments.
	ExtraArgs []string
}

// WindowSize represents browser window dimensions.
type WindowSize struct {
	Width  int
	Height int
}

// Chrome manages a Chrome browser process.
type Chrome struct {
	mu sync.RWMutex

	config  *ChromeConfig
	cmd     *exec.Cmd
	dataDir string // actual user data dir (may be temp)
	tempDir bool   // whether dataDir is temporary

	// CDP endpoint discovered from Chrome stderr
	cdpEndpoint string

	// Process state
	started   bool
	pid       int
	startedAt time.Time
	exitErr   error
	exitCh    chan struct{}
}

// cdpURLPattern matches the DevTools WebSocket URL from Chrome stderr.
var cdpURLPattern = regexp.MustCompile(`DevTools listening on (ws://[^\s]+)`)

// NewChrome creates a new Chrome instance with the given configuration.
func NewChrome(cfg *ChromeConfig) *Chrome {
	if cfg == nil {
		cfg = &ChromeConfig{}
	}

	// Set defaults
	if cfg.WindowSize == nil {
		cfg.WindowSize = &WindowSize{Width: 1280, Height: 720}
	}

	return &Chrome{
		config: cfg,
		exitCh: make(chan struct{}),
	}
}

// Start launches the Chrome process.
func (c *Chrome) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return fmt.Errorf("chrome already started")
	}

	// Find Chrome binary
	binaryPath := c.config.BinaryPath
	if binaryPath == "" {
		var err error
		binaryPath, err = findChromeBinary()
		if err != nil {
			return fmt.Errorf("finding chrome binary: %w", err)
		}
	}

	// Setup user data directory
	if c.config.UserDataDir != "" {
		c.dataDir = c.config.UserDataDir
		c.tempDir = false
	} else {
		tmpDir, err := os.MkdirTemp("", "chrome-cdp-*")
		if err != nil {
			return fmt.Errorf("creating temp dir: %w", err)
		}
		c.dataDir = tmpDir
		c.tempDir = true
	}

	// Build command-line arguments
	args := c.buildArgs()

	// Create command
	c.cmd = exec.CommandContext(ctx, binaryPath, args...)

	// Capture stderr to find CDP endpoint
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating stderr pipe: %w", err)
	}

	// Start Chrome
	if err := c.cmd.Start(); err != nil {
		c.cleanup()
		return fmt.Errorf("starting chrome: %w", err)
	}

	c.started = true
	c.pid = c.cmd.Process.Pid
	c.startedAt = time.Now()

	// Wait for CDP endpoint in stderr
	endpointCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if matches := cdpURLPattern.FindStringSubmatch(line); len(matches) > 1 {
				endpointCh <- matches[1]
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("reading stderr: %w", err)
		} else {
			errCh <- fmt.Errorf("chrome exited without providing CDP endpoint")
		}
	}()

	// Monitor process exit
	go func() {
		err := c.cmd.Wait()
		c.mu.Lock()
		c.exitErr = err
		c.mu.Unlock()
		close(c.exitCh)
	}()

	// Wait for endpoint or timeout
	select {
	case endpoint := <-endpointCh:
		c.cdpEndpoint = endpoint
		return nil
	case err := <-errCh:
		_ = c.Stop()
		return err
	case <-c.exitCh:
		c.mu.RLock()
		exitErr := c.exitErr
		c.mu.RUnlock()
		return fmt.Errorf("chrome exited unexpectedly: %w", exitErr)
	case <-time.After(30 * time.Second):
		_ = c.Stop()
		return fmt.Errorf("timeout waiting for CDP endpoint")
	case <-ctx.Done():
		_ = c.Stop()
		return ctx.Err()
	}
}

// Stop terminates the Chrome process.
func (c *Chrome) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown first
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited
		if !isProcessExited(err) {
			return fmt.Errorf("sending SIGTERM: %w", err)
		}
	}

	// Wait for exit with timeout
	select {
	case <-c.exitCh:
		// Process exited
	case <-time.After(5 * time.Second):
		// Force kill
		if err := c.cmd.Process.Kill(); err != nil {
			if !isProcessExited(err) {
				return fmt.Errorf("killing chrome: %w", err)
			}
		}
		<-c.exitCh
	}

	c.cleanup()
	return nil
}

// cleanup removes temporary directories.
func (c *Chrome) cleanup() {
	if c.tempDir && c.dataDir != "" {
		_ = os.RemoveAll(c.dataDir)
	}
}

// CDPEndpoint returns the Chrome DevTools Protocol WebSocket endpoint.
func (c *Chrome) CDPEndpoint() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cdpEndpoint
}

// PID returns the Chrome process ID.
func (c *Chrome) PID() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pid
}

// IsRunning returns whether Chrome is running.
func (c *Chrome) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return false
	}

	select {
	case <-c.exitCh:
		return false
	default:
		return true
	}
}

// ExitError returns the exit error if Chrome has exited.
func (c *Chrome) ExitError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.exitErr
}

// buildArgs constructs Chrome command-line arguments.
func (c *Chrome) buildArgs() []string {
	args := []string{
		"--disable-background-networking",
		"--disable-background-timer-throttling",
		"--disable-backgrounding-occluded-windows",
		"--disable-breakpad",
		"--disable-component-extensions-with-background-pages",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--disable-hang-monitor",
		"--disable-ipc-flooding-protection",
		"--disable-popup-blocking",
		"--disable-prompt-on-repost",
		"--disable-renderer-backgrounding",
		"--disable-sync",
		"--disable-translate",
		"--enable-features=NetworkService,NetworkServiceInProcess",
		"--force-color-profile=srgb",
		"--metrics-recording-only",
		"--no-first-run",
		"--safebrowsing-disable-auto-update",
		fmt.Sprintf("--user-data-dir=%s", c.dataDir),
	}

	// Headless mode
	if c.config.Headless {
		args = append(args, "--headless=new")
	}

	// Remote debugging
	if c.config.RemoteDebuggingPort > 0 {
		args = append(args, fmt.Sprintf("--remote-debugging-port=%d", c.config.RemoteDebuggingPort))
	} else {
		args = append(args, "--remote-debugging-port=0") // Random port
	}

	// Window size
	if c.config.WindowSize != nil {
		args = append(args, fmt.Sprintf("--window-size=%d,%d",
			c.config.WindowSize.Width, c.config.WindowSize.Height))
	}

	// Check if running as root
	if os.Getuid() == 0 {
		args = append(args, "--no-sandbox")
	}

	// Extra arguments
	args = append(args, c.config.ExtraArgs...)

	// Start with about:blank
	args = append(args, "about:blank")

	return args
}

// findChromeBinary searches for Chrome in common locations.
func findChromeBinary() (string, error) {
	// Check CHROME_PATH environment variable first
	if path := os.Getenv("CHROME_PATH"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Platform-specific paths
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
		}
	case "linux":
		paths = []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("PROGRAMFILES")
		programFilesX86 := os.Getenv("PROGRAMFILES(X86)")
		paths = []string{
			filepath.Join(localAppData, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		}
	}

	// Also check PATH
	if path, err := exec.LookPath("google-chrome"); err == nil {
		paths = append([]string{path}, paths...)
	}
	if path, err := exec.LookPath("chromium"); err == nil {
		paths = append([]string{path}, paths...)
	}

	// Find first existing path
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("chrome binary not found; set CHROME_PATH environment variable")
}

// isProcessExited checks if an error indicates the process has already exited.
func isProcessExited(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "process already finished") ||
		strings.Contains(errStr, "no such process") ||
		strings.Contains(errStr, "process has exited")
}

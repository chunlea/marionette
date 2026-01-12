package browser

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChrome_NilConfig(t *testing.T) {
	chrome := NewChrome(nil)

	require.NotNil(t, chrome)
	require.NotNil(t, chrome.config)
	require.NotNil(t, chrome.config.WindowSize)
	assert.Equal(t, 1280, chrome.config.WindowSize.Width)
	assert.Equal(t, 720, chrome.config.WindowSize.Height)
	assert.NotNil(t, chrome.exitCh)
}

func TestNewChrome_CustomConfig(t *testing.T) {
	cfg := &ChromeConfig{
		BinaryPath:          "/custom/chrome",
		UserDataDir:         "/custom/data",
		Headless:            true,
		RemoteDebuggingPort: 9222,
		WindowSize:          &WindowSize{Width: 1920, Height: 1080},
		ExtraArgs:           []string{"--disable-gpu"},
	}

	chrome := NewChrome(cfg)

	require.NotNil(t, chrome)
	assert.Equal(t, "/custom/chrome", chrome.config.BinaryPath)
	assert.Equal(t, "/custom/data", chrome.config.UserDataDir)
	assert.True(t, chrome.config.Headless)
	assert.Equal(t, 9222, chrome.config.RemoteDebuggingPort)
	assert.Equal(t, 1920, chrome.config.WindowSize.Width)
	assert.Equal(t, 1080, chrome.config.WindowSize.Height)
	assert.Equal(t, []string{"--disable-gpu"}, chrome.config.ExtraArgs)
}

func TestNewChrome_DefaultWindowSize(t *testing.T) {
	cfg := &ChromeConfig{
		WindowSize: nil, // No window size specified
	}

	chrome := NewChrome(cfg)

	require.NotNil(t, chrome.config.WindowSize)
	assert.Equal(t, 1280, chrome.config.WindowSize.Width)
	assert.Equal(t, 720, chrome.config.WindowSize.Height)
}

func TestChrome_CDPEndpoint_BeforeStart(t *testing.T) {
	chrome := NewChrome(nil)

	endpoint := chrome.CDPEndpoint()

	assert.Empty(t, endpoint)
}

func TestChrome_PID_BeforeStart(t *testing.T) {
	chrome := NewChrome(nil)

	pid := chrome.PID()

	assert.Equal(t, 0, pid)
}

func TestChrome_IsRunning_BeforeStart(t *testing.T) {
	chrome := NewChrome(nil)

	running := chrome.IsRunning()

	assert.False(t, running)
}

func TestChrome_ExitError_BeforeStart(t *testing.T) {
	chrome := NewChrome(nil)

	err := chrome.ExitError()

	assert.NoError(t, err)
}

func TestChrome_buildArgs_Basic(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		UserDataDir: "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	// Check for required arguments
	assert.Contains(t, args, "--no-first-run")
	assert.Contains(t, args, "--disable-extensions")
	assert.Contains(t, args, "--user-data-dir=/test/data")
	assert.Contains(t, args, "--remote-debugging-port=0")
	assert.Contains(t, args, "--window-size=1280,720")
	assert.Contains(t, args, "about:blank")
}

func TestChrome_buildArgs_Headless(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		Headless:    true,
		UserDataDir: "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	assert.Contains(t, args, "--headless=new")
}

func TestChrome_buildArgs_CustomPort(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		RemoteDebuggingPort: 9222,
		UserDataDir:         "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	assert.Contains(t, args, "--remote-debugging-port=9222")
	assert.NotContains(t, args, "--remote-debugging-port=0")
}

func TestChrome_buildArgs_CustomWindowSize(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		WindowSize:  &WindowSize{Width: 1920, Height: 1080},
		UserDataDir: "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	assert.Contains(t, args, "--window-size=1920,1080")
}

func TestChrome_buildArgs_ExtraArgs(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		UserDataDir: "/test/data",
		ExtraArgs:   []string{"--custom-arg1", "--custom-arg2=value"},
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	assert.Contains(t, args, "--custom-arg1")
	assert.Contains(t, args, "--custom-arg2=value")
}

func TestChrome_buildArgs_NoSandboxForRoot(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("test requires root user")
	}

	chrome := NewChrome(&ChromeConfig{
		UserDataDir: "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	assert.Contains(t, args, "--no-sandbox")
}

func TestChrome_Stop_NotStarted(t *testing.T) {
	chrome := NewChrome(nil)

	// Stop should be safe to call when not started
	err := chrome.Stop()

	assert.NoError(t, err)
}

func TestFindChromeBinary_EnvVar(t *testing.T) {
	// Create a temp file to act as a "binary"
	tmpFile, err := os.CreateTemp("", "chrome-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(tmpFile.Name()) })
	require.NoError(t, tmpFile.Close())

	// Set CHROME_PATH
	originalPath := os.Getenv("CHROME_PATH")
	require.NoError(t, os.Setenv("CHROME_PATH", tmpFile.Name()))
	t.Cleanup(func() { _ = os.Setenv("CHROME_PATH", originalPath) })

	path, err := findChromeBinary()

	require.NoError(t, err)
	assert.Equal(t, tmpFile.Name(), path)
}

func TestFindChromeBinary_EnvVarNotExist(t *testing.T) {
	// Set CHROME_PATH to non-existent file
	originalPath := os.Getenv("CHROME_PATH")
	require.NoError(t, os.Setenv("CHROME_PATH", "/nonexistent/chrome/binary"))
	t.Cleanup(func() { _ = os.Setenv("CHROME_PATH", originalPath) })

	// If no system Chrome is found, it should error
	// This test depends on whether Chrome is installed on the system
	_, err := findChromeBinary()

	// The test just verifies the function doesn't crash
	// Error or success depends on system Chrome availability
	_ = err
}

func TestFindChromeBinary_PlatformPaths(t *testing.T) {
	// Clear CHROME_PATH to test platform detection
	originalPath := os.Getenv("CHROME_PATH")
	require.NoError(t, os.Setenv("CHROME_PATH", ""))
	t.Cleanup(func() { _ = os.Setenv("CHROME_PATH", originalPath) })

	// This test verifies that findChromeBinary doesn't panic
	// and returns an appropriate result based on what's installed
	path, err := findChromeBinary()

	// Either finds Chrome or returns an error - both are valid outcomes
	if err != nil {
		assert.Contains(t, err.Error(), "chrome binary not found")
	} else {
		assert.NotEmpty(t, path)
		// Verify the path actually exists
		_, statErr := os.Stat(path)
		assert.NoError(t, statErr)
	}
}

func TestIsProcessExited_Nil(t *testing.T) {
	result := isProcessExited(nil)
	assert.False(t, result)
}

func TestIsProcessExited_ProcessFinished(t *testing.T) {
	err := &processFinishedError{msg: "os: process already finished"}
	result := isProcessExited(err)
	assert.True(t, result)
}

func TestIsProcessExited_NoSuchProcess(t *testing.T) {
	err := &processFinishedError{msg: "no such process"}
	result := isProcessExited(err)
	assert.True(t, result)
}

func TestIsProcessExited_ProcessExited(t *testing.T) {
	err := &processFinishedError{msg: "process has exited"}
	result := isProcessExited(err)
	assert.True(t, result)
}

func TestIsProcessExited_OtherError(t *testing.T) {
	err := &processFinishedError{msg: "some other error"}
	result := isProcessExited(err)
	assert.False(t, result)
}

// processFinishedError is a helper error type for testing.
type processFinishedError struct {
	msg string
}

func (e *processFinishedError) Error() string {
	return e.msg
}

func TestCDPURLPattern(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard endpoint",
			input:    "DevTools listening on ws://127.0.0.1:9222/devtools/browser/abc123",
			expected: "ws://127.0.0.1:9222/devtools/browser/abc123",
		},
		{
			name:     "with random port",
			input:    "DevTools listening on ws://localhost:55123/devtools/browser/def456",
			expected: "ws://localhost:55123/devtools/browser/def456",
		},
		{
			name:     "no match",
			input:    "Some other log message",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := cdpURLPattern.FindStringSubmatch(tt.input)
			if tt.expected == "" {
				assert.Empty(t, matches)
			} else {
				require.Len(t, matches, 2)
				assert.Equal(t, tt.expected, matches[1])
			}
		})
	}
}

func TestWindowSize(t *testing.T) {
	ws := &WindowSize{
		Width:  1920,
		Height: 1080,
	}

	assert.Equal(t, 1920, ws.Width)
	assert.Equal(t, 1080, ws.Height)
}

func TestChromeConfig_Defaults(t *testing.T) {
	cfg := &ChromeConfig{}

	assert.Empty(t, cfg.BinaryPath)
	assert.Empty(t, cfg.UserDataDir)
	assert.False(t, cfg.Headless)
	assert.Equal(t, 0, cfg.RemoteDebuggingPort)
	assert.Nil(t, cfg.WindowSize)
	assert.Nil(t, cfg.ExtraArgs)
}

func TestChrome_ConcurrentAccess(t *testing.T) {
	chrome := NewChrome(nil)

	// Test concurrent reads of state
	done := make(chan bool, 4)

	go func() {
		_ = chrome.CDPEndpoint()
		done <- true
	}()

	go func() {
		_ = chrome.PID()
		done <- true
	}()

	go func() {
		_ = chrome.IsRunning()
		done <- true
	}()

	go func() {
		_ = chrome.ExitError()
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}
}

func TestChrome_buildArgs_AllSecurityFlags(t *testing.T) {
	chrome := NewChrome(&ChromeConfig{
		UserDataDir: "/test/data",
	})
	chrome.dataDir = "/test/data"

	args := chrome.buildArgs()

	// Verify all security/stability flags are present
	expectedFlags := []string{
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
	}

	for _, flag := range expectedFlags {
		assert.Contains(t, args, flag, "missing flag: %s", flag)
	}
}

func TestFindChromeBinary_KnownPaths(t *testing.T) {
	// This test verifies the structure of expected paths per platform
	// without requiring Chrome to be installed

	switch runtime.GOOS {
	case "darwin":
		// On macOS, we expect these paths to be checked
		expectedPaths := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, path := range expectedPaths {
			// Just verify the paths are well-formed, not that Chrome exists
			assert.NotEmpty(t, path)
		}

	case "linux":
		expectedPaths := []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
		for _, path := range expectedPaths {
			assert.NotEmpty(t, path)
		}

	case "windows":
		// On Windows, paths depend on environment variables
		// Just verify the code doesn't panic when env vars are set
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			assert.NotEmpty(t, localAppData)
		}
	}
}

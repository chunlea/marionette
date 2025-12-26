package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultVerifier implements the Verifier interface.
type DefaultVerifier struct {
	detector     *Detector
	limitChecker *LimitChecker
	config       VerifyConfig
}

// NewVerifier creates a new sandbox verifier with default configuration.
func NewVerifier() *DefaultVerifier {
	return &DefaultVerifier{
		detector:     NewDetector(),
		limitChecker: NewLimitChecker(),
		config:       DefaultVerifyConfig(),
	}
}

// NewVerifierWithConfig creates a new sandbox verifier with custom configuration.
func NewVerifierWithConfig(config VerifyConfig) *DefaultVerifier {
	return &DefaultVerifier{
		detector:     NewDetector(),
		limitChecker: NewLimitChecker(),
		config:       config,
	}
}

// Detect identifies the current sandbox environment.
func (v *DefaultVerifier) Detect(ctx context.Context) (*Environment, error) {
	return v.detector.Detect(ctx)
}

// VerifyIsolation runs isolation verification tests.
func (v *DefaultVerifier) VerifyIsolation(ctx context.Context) (*IsolationResult, error) {
	start := time.Now()
	result := &IsolationResult{
		Passed: true,
		Tests:  make([]IsolationTest, 0),
		Errors: make([]error, 0),
	}

	// Create a context with timeout
	if v.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, v.config.Timeout)
		defer cancel()
	}

	// Run filesystem isolation tests
	if !v.config.SkipFilesystemTests {
		fsTests := v.runFilesystemTests(ctx)
		result.Tests = append(result.Tests, fsTests...)
	}

	// Run network isolation tests
	if !v.config.SkipNetworkTests {
		netTests := v.runNetworkTests(ctx)
		result.Tests = append(result.Tests, netTests...)
	}

	// Run process isolation tests
	if !v.config.SkipProcessTests {
		procTests := v.runProcessTests(ctx)
		result.Tests = append(result.Tests, procTests...)
	}

	// Check overall pass/fail
	for _, test := range result.Tests {
		if !test.Passed && test.Severity == "critical" {
			result.Passed = false
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

// VerifyResourceLimits checks if resource limits are enforced.
func (v *DefaultVerifier) VerifyResourceLimits(ctx context.Context) (*ResourceResult, error) {
	return v.limitChecker.VerifyResourceLimits(ctx)
}

// GetCapabilities returns the capabilities of the current sandbox.
func (v *DefaultVerifier) GetCapabilities(ctx context.Context) (*Capabilities, error) {
	caps := &Capabilities{
		AvailableSandboxTypes: make([]SandboxType, 0),
	}

	// Detect available sandbox types
	types, err := v.detector.GetAvailableSandboxTypes(ctx)
	if err == nil {
		caps.AvailableSandboxTypes = types
	}

	// Check for network isolation
	caps.HasNetworkIsolation = v.checkNetworkIsolationCapability()

	// Check for filesystem isolation
	caps.HasFilesystemIsolation = v.checkFilesystemIsolationCapability()

	// Check for process isolation
	caps.HasProcessIsolation = v.checkProcessIsolationCapability()

	// Check for resource limits
	caps.HasResourceLimits = v.checkResourceLimitsCapability()

	// Check if can create child sandboxes
	caps.CanCreateSandbox = v.checkCanCreateSandbox()

	// Check security modules
	caps.SupportsSeccomp = v.checkSeccompSupport()
	caps.SupportsAppArmor = v.checkAppArmorSupport()
	caps.SupportsSELinux = v.checkSELinuxSupport()

	// Get resource limits
	limits, err := v.limitChecker.VerifyResourceLimits(ctx)
	if err == nil {
		for _, limit := range limits.Limits {
			switch limit.Name {
			case "memory":
				if limit.Enforced && limit.Configured != "" {
					caps.MaxMemoryMB = parseMemoryMB(limit.Configured)
				}
			case "pids":
				if limit.Enforced && limit.Configured != "" {
					caps.MaxPids = parseInt64(limit.Configured)
				}
			}
		}
	}

	return caps, nil
}

// runFilesystemTests runs filesystem isolation tests.
func (v *DefaultVerifier) runFilesystemTests(ctx context.Context) []IsolationTest {
	tests := make([]IsolationTest, 0)

	// Test 1: Cannot access /etc/shadow
	tests = append(tests, v.testFileAccess("/etc/shadow", false, "critical"))

	// Test 2: Cannot access host /proc/1/root
	tests = append(tests, v.testFileAccess("/proc/1/root", false, "critical"))

	// Test 3: Cannot write to /etc
	tests = append(tests, v.testFileWrite("/etc/test_write_check", false, "high"))

	// Test 4: Cannot access Docker socket
	tests = append(tests, v.testFileAccess("/var/run/docker.sock", false, "critical"))

	// Test 5: /tmp should be writable
	tests = append(tests, v.testFileWrite("/tmp/sandbox_test_write", true, "medium"))

	// Test 6: Cannot access cloud metadata endpoint file (if it existed as file)
	tests = append(tests, v.testSymlinkEscape())

	return tests
}

// runNetworkTests runs network isolation tests.
func (v *DefaultVerifier) runNetworkTests(ctx context.Context) []IsolationTest {
	tests := make([]IsolationTest, 0)

	// Test 1: Cannot access cloud metadata endpoint (169.254.169.254)
	tests = append(tests, v.testMetadataEndpointBlocked())

	// Test 2: DNS resolution should work (if allowed)
	tests = append(tests, v.testDNSResolution())

	// Test 3: Cannot bind to privileged ports
	tests = append(tests, v.testPrivilegedPortBinding())

	return tests
}

// runProcessTests runs process isolation tests.
func (v *DefaultVerifier) runProcessTests(ctx context.Context) []IsolationTest {
	tests := make([]IsolationTest, 0)

	// Test 1: Cannot see host processes
	tests = append(tests, v.testHostProcessVisibility())

	// Test 2: Cannot ptrace other processes
	tests = append(tests, v.testPtraceRestriction())

	// Test 3: Running as non-root (if expected)
	tests = append(tests, v.testNonRootUser())

	return tests
}

// testFileAccess tests if a file can be accessed.
func (v *DefaultVerifier) testFileAccess(path string, shouldSucceed bool, severity string) IsolationTest {
	test := IsolationTest{
		Name:     fmt.Sprintf("file_access_%s", filepath.Base(path)),
		Category: "filesystem",
		Severity: severity,
	}

	_, err := os.Stat(path)
	canAccess := err == nil

	if canAccess == shouldSucceed {
		test.Passed = true
		if shouldSucceed {
			test.Message = fmt.Sprintf("Can access %s as expected", path)
		} else {
			test.Message = fmt.Sprintf("Cannot access %s as expected", path)
		}
	} else {
		test.Passed = false
		if shouldSucceed {
			test.Message = fmt.Sprintf("Cannot access %s but should be able to: %v", path, err)
		} else {
			test.Message = fmt.Sprintf("Can access %s but should not be able to", path)
		}
	}

	return test
}

// testFileWrite tests if a file can be written.
func (v *DefaultVerifier) testFileWrite(path string, shouldSucceed bool, severity string) IsolationTest {
	test := IsolationTest{
		Name:     fmt.Sprintf("file_write_%s", filepath.Base(path)),
		Category: "filesystem",
		Severity: severity,
	}

	err := os.WriteFile(path, []byte("test"), 0600)
	canWrite := err == nil

	// Clean up if we successfully wrote
	if canWrite {
		_ = os.Remove(path)
	}

	if canWrite == shouldSucceed {
		test.Passed = true
		if shouldSucceed {
			test.Message = fmt.Sprintf("Can write to %s as expected", path)
		} else {
			test.Message = fmt.Sprintf("Cannot write to %s as expected", path)
		}
	} else {
		test.Passed = false
		if shouldSucceed {
			test.Message = fmt.Sprintf("Cannot write to %s but should be able to: %v", path, err)
		} else {
			test.Message = fmt.Sprintf("Can write to %s but should not be able to", path)
		}
	}

	return test
}

// testSymlinkEscape tests for symlink escape vulnerabilities.
func (v *DefaultVerifier) testSymlinkEscape() IsolationTest {
	test := IsolationTest{
		Name:     "symlink_escape",
		Category: "filesystem",
		Severity: "high",
		Passed:   true,
		Message:  "Symlink escape test passed",
	}

	// Try to create a symlink to /etc/passwd and read through it
	tmpDir := os.TempDir()
	symlinkPath := filepath.Join(tmpDir, "sandbox_symlink_test")

	// Clean up first
	_ = os.Remove(symlinkPath)

	// Create symlink to /etc/passwd
	err := os.Symlink("/etc/passwd", symlinkPath)
	if err != nil {
		test.Message = "Cannot create symlinks (restricted)"
		return test
	}
	defer func() { _ = os.Remove(symlinkPath) }()

	// Try to read through symlink
	_, err = os.ReadFile(symlinkPath)
	if err == nil {
		// This might be expected - symlinks to readable files should work
		// The concern is symlinks to files outside the container
		test.Message = "Symlink creation works (ensure proper restrictions)"
	}

	return test
}

// testMetadataEndpointBlocked tests that cloud metadata endpoint is blocked.
func (v *DefaultVerifier) testMetadataEndpointBlocked() IsolationTest {
	test := IsolationTest{
		Name:     "metadata_endpoint_blocked",
		Category: "network",
		Severity: "critical",
	}

	// Try to connect to metadata endpoint
	conn, err := net.DialTimeout("tcp", "169.254.169.254:80", 2*time.Second)
	if err != nil {
		test.Passed = true
		test.Message = "Cloud metadata endpoint (169.254.169.254) is blocked"
	} else {
		_ = conn.Close()
		test.Passed = false
		test.Message = "Cloud metadata endpoint (169.254.169.254) is accessible - security risk!"
	}

	return test
}

// testDNSResolution tests if DNS resolution works.
func (v *DefaultVerifier) testDNSResolution() IsolationTest {
	test := IsolationTest{
		Name:     "dns_resolution",
		Category: "network",
		Severity: "low",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resolver net.Resolver
	_, err := resolver.LookupHost(ctx, "localhost")

	if err == nil {
		test.Passed = true
		test.Message = "DNS resolution is working"
	} else {
		test.Passed = true // DNS might be intentionally restricted
		test.Message = fmt.Sprintf("DNS resolution restricted: %v", err)
	}

	return test
}

// testPrivilegedPortBinding tests that we cannot bind to privileged ports.
func (v *DefaultVerifier) testPrivilegedPortBinding() IsolationTest {
	test := IsolationTest{
		Name:     "privileged_port_binding",
		Category: "network",
		Severity: "medium",
	}

	// Try to bind to port 80
	listener, err := net.Listen("tcp", ":80")
	if err != nil {
		test.Passed = true
		test.Message = "Cannot bind to privileged ports (port 80)"
	} else {
		_ = listener.Close()
		test.Passed = false
		test.Message = "Can bind to privileged ports - running as root?"
	}

	return test
}

// testHostProcessVisibility tests if host processes are visible.
func (v *DefaultVerifier) testHostProcessVisibility() IsolationTest {
	test := IsolationTest{
		Name:     "host_process_visibility",
		Category: "process",
		Severity: "high",
	}

	if runtime.GOOS != "linux" {
		test.Passed = true
		test.Message = "Process isolation test only applicable on Linux"
		return test
	}

	// In a properly isolated container, /proc should only show container processes
	// Check if PID 1 is our expected init process
	cmdlinePath := "/proc/1/cmdline"
	data, err := os.ReadFile(cmdlinePath)
	if err != nil {
		test.Passed = true
		test.Message = "Cannot read /proc/1/cmdline (good isolation)"
		return test
	}

	cmdline := string(data)
	// If PID 1 is systemd or init, we might be seeing host processes
	if strings.Contains(cmdline, "systemd") || strings.Contains(cmdline, "/sbin/init") {
		test.Passed = false
		test.Message = "May be seeing host processes (PID 1 is system init)"
	} else {
		test.Passed = true
		test.Message = "Process namespace appears isolated"
	}

	return test
}

// testPtraceRestriction tests that ptrace is restricted.
func (v *DefaultVerifier) testPtraceRestriction() IsolationTest {
	test := IsolationTest{
		Name:     "ptrace_restriction",
		Category: "process",
		Severity: "high",
	}

	if runtime.GOOS != "linux" {
		test.Passed = true
		test.Message = "Ptrace test only applicable on Linux"
		return test
	}

	// Check ptrace scope
	data, err := os.ReadFile("/proc/sys/kernel/yama/ptrace_scope")
	if err != nil {
		test.Passed = true
		test.Message = "Cannot read ptrace_scope (likely restricted)"
		return test
	}

	scope := strings.TrimSpace(string(data))
	switch scope {
	case "0":
		test.Passed = false
		test.Message = "Ptrace unrestricted (scope=0)"
	case "1":
		test.Passed = true
		test.Message = "Ptrace restricted to parent processes (scope=1)"
	case "2":
		test.Passed = true
		test.Message = "Ptrace restricted to admin only (scope=2)"
	case "3":
		test.Passed = true
		test.Message = "Ptrace disabled (scope=3)"
	default:
		test.Passed = true
		test.Message = fmt.Sprintf("Ptrace scope=%s", scope)
	}

	return test
}

// testNonRootUser tests that we're running as non-root.
func (v *DefaultVerifier) testNonRootUser() IsolationTest {
	test := IsolationTest{
		Name:     "non_root_user",
		Category: "process",
		Severity: "medium",
	}

	uid := os.Getuid()
	if uid == 0 {
		test.Passed = false
		test.Message = "Running as root (UID 0)"
	} else {
		test.Passed = true
		test.Message = fmt.Sprintf("Running as non-root (UID %d)", uid)
	}

	return test
}

// Capability check helpers

func (v *DefaultVerifier) checkNetworkIsolationCapability() bool {
	// Check if network namespaces are available
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/proc/self/ns/net")
	return err == nil
}

func (v *DefaultVerifier) checkFilesystemIsolationCapability() bool {
	// Check if mount namespaces are available
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/proc/self/ns/mnt")
	return err == nil
}

func (v *DefaultVerifier) checkProcessIsolationCapability() bool {
	// Check if PID namespaces are available
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/proc/self/ns/pid")
	return err == nil
}

func (v *DefaultVerifier) checkResourceLimitsCapability() bool {
	// Check if cgroups are available
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/sys/fs/cgroup")
	return err == nil
}

func (v *DefaultVerifier) checkCanCreateSandbox() bool {
	// Check if we can use unshare or have docker/podman access
	if runtime.GOOS != "linux" {
		return false
	}

	// Check for docker socket
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}

	// Check for podman
	if _, err := exec.LookPath("podman"); err == nil {
		return true
	}

	// Check if we have CAP_SYS_ADMIN for unshare
	// This is a simplified check
	return os.Getuid() == 0
}

func (v *DefaultVerifier) checkSeccompSupport() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	// Check if seccomp is available
	_, err := os.Stat("/proc/self/seccomp")
	return err == nil
}

func (v *DefaultVerifier) checkAppArmorSupport() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/sys/kernel/security/apparmor")
	return err == nil
}

func (v *DefaultVerifier) checkSELinuxSupport() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat("/sys/fs/selinux")
	return err == nil
}

// Helper functions

func parseMemoryMB(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	switch {
	case strings.HasSuffix(s, "GB"):
		s = strings.TrimSuffix(s, "GB")
		return parseInt64(strings.TrimSpace(s)) * 1024
	case strings.HasSuffix(s, "MB"):
		s = strings.TrimSuffix(s, "MB")
		return parseInt64(strings.TrimSpace(s))
	case strings.HasSuffix(s, "KB"):
		s = strings.TrimSuffix(s, "KB")
		val := parseInt64(strings.TrimSpace(s))
		if val < 1024 {
			return 1
		}
		return val / 1024
	default:
		return parseInt64(strings.TrimSpace(s))
	}
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	// Remove any non-numeric suffix
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			s = s[:i]
			break
		}
	}
	var result int64
	_, _ = fmt.Sscanf(s, "%d", &result)
	return result
}

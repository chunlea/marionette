// Package sandbox provides sandbox detection, verification, and resource limit enforcement.
package sandbox

import (
	"context"
	"time"
)

// SandboxMode represents the sandbox configuration mode.
type SandboxMode string

const (
	// ModeRunnerIsSandbox means the runner itself is the sandbox (Docker, E2B).
	ModeRunnerIsSandbox SandboxMode = "runner-is-sandbox"

	// ModeRunnerCreatesSandbox means the runner creates sandboxes for tasks.
	ModeRunnerCreatesSandbox SandboxMode = "runner-creates-sandbox"

	// ModeNone means no sandboxing is applied.
	ModeNone SandboxMode = "none"
)

// SandboxType represents a specific sandbox technology.
type SandboxType string

const (
	// TypeDocker is a standard Docker container.
	TypeDocker SandboxType = "docker"

	// TypeGVisor is gVisor (runsc) container runtime.
	TypeGVisor SandboxType = "gvisor"

	// TypeFirecracker is Firecracker microVM.
	TypeFirecracker SandboxType = "firecracker"

	// TypeKataContainers is Kata Containers.
	TypeKataContainers SandboxType = "kata"

	// TypeNsJail is NsJail sandbox.
	TypeNsJail SandboxType = "nsjail"

	// TypeBubblewrap is Bubblewrap (bwrap) sandbox.
	TypeBubblewrap SandboxType = "bubblewrap"

	// TypeNone means no sandbox detected.
	TypeNone SandboxType = "none"
)

// Verifier provides sandbox detection and verification capabilities.
type Verifier interface {
	// Detect identifies the current sandbox environment.
	Detect(ctx context.Context) (*Environment, error)

	// VerifyIsolation runs isolation verification tests.
	VerifyIsolation(ctx context.Context) (*IsolationResult, error)

	// VerifyResourceLimits checks if resource limits are enforced.
	VerifyResourceLimits(ctx context.Context) (*ResourceResult, error)

	// GetCapabilities returns the capabilities of the current sandbox.
	GetCapabilities(ctx context.Context) (*Capabilities, error)
}

// Environment describes the detected sandbox environment.
type Environment struct {
	// Type is the detected sandbox type.
	Type SandboxType

	// Mode is the sandbox mode configuration.
	Mode SandboxMode

	// InContainer indicates if running inside a container.
	InContainer bool

	// InVM indicates if running inside a virtual machine.
	InVM bool

	// ContainerRuntime is the detected container runtime (docker, containerd, etc.).
	ContainerRuntime string

	// ContainerID is the container ID if running in a container.
	ContainerID string

	// Hostname is the current hostname.
	Hostname string

	// Metadata contains additional detection information.
	Metadata map[string]string
}

// IsolationResult contains the results of isolation verification tests.
type IsolationResult struct {
	// Passed indicates if all isolation tests passed.
	Passed bool

	// Tests contains individual test results.
	Tests []IsolationTest

	// Errors contains any errors encountered during testing.
	Errors []error

	// Duration is how long the verification took.
	Duration time.Duration
}

// IsolationTest represents a single isolation test result.
type IsolationTest struct {
	// Name is the test name.
	Name string

	// Category is the test category (filesystem, network, process, etc.).
	Category string

	// Passed indicates if the test passed.
	Passed bool

	// Message provides details about the result.
	Message string

	// Severity indicates the importance of this test (critical, high, medium, low).
	Severity string
}

// ResourceResult contains the results of resource limit verification.
type ResourceResult struct {
	// Passed indicates if all resource limits are properly enforced.
	Passed bool

	// Limits contains individual limit check results.
	Limits []ResourceLimit

	// Errors contains any errors encountered during verification.
	Errors []error

	// Duration is how long the verification took.
	Duration time.Duration
}

// ResourceLimit represents a single resource limit check result.
type ResourceLimit struct {
	// Name is the limit name (memory, cpu, disk, pids, etc.).
	Name string

	// Configured is the configured limit value (if detectable).
	Configured string

	// Enforced indicates if the limit is being enforced.
	Enforced bool

	// Current is the current usage value.
	Current string

	// Message provides details about the check.
	Message string
}

// Capabilities describes what the sandbox can and cannot do.
type Capabilities struct {
	// AvailableSandboxTypes lists sandbox types available on this system.
	AvailableSandboxTypes []SandboxType

	// HasNetworkIsolation indicates if network isolation is available.
	HasNetworkIsolation bool

	// HasFilesystemIsolation indicates if filesystem isolation is available.
	HasFilesystemIsolation bool

	// HasProcessIsolation indicates if process isolation is available.
	HasProcessIsolation bool

	// HasResourceLimits indicates if resource limits can be enforced.
	HasResourceLimits bool

	// CanCreateSandbox indicates if this environment can create child sandboxes.
	CanCreateSandbox bool

	// SupportsSeccomp indicates if seccomp filtering is available.
	SupportsSeccomp bool

	// SupportsAppArmor indicates if AppArmor is available.
	SupportsAppArmor bool

	// SupportsSELinux indicates if SELinux is available.
	SupportsSELinux bool

	// MaxMemoryMB is the maximum memory available (0 = unlimited).
	MaxMemoryMB int64

	// MaxCPUs is the maximum CPU cores available (0 = unlimited).
	MaxCPUs float64

	// MaxDiskMB is the maximum disk space available (0 = unlimited).
	MaxDiskMB int64

	// MaxPids is the maximum number of processes (0 = unlimited).
	MaxPids int64
}

// VerifyConfig contains configuration for verification tests.
type VerifyConfig struct {
	// SkipNetworkTests skips network isolation tests.
	SkipNetworkTests bool

	// SkipFilesystemTests skips filesystem isolation tests.
	SkipFilesystemTests bool

	// SkipProcessTests skips process isolation tests.
	SkipProcessTests bool

	// SkipResourceTests skips resource limit tests.
	SkipResourceTests bool

	// Timeout is the maximum time for verification.
	Timeout time.Duration
}

// DefaultVerifyConfig returns the default verification configuration.
func DefaultVerifyConfig() VerifyConfig {
	return VerifyConfig{
		Timeout: 30 * time.Second,
	}
}

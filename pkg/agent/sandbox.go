package agent

import (
	"os"
	"os/exec"
	"runtime"
)

// SandboxCapabilities describes detected sandbox capabilities on the runner.
type SandboxCapabilities struct {
	// Types is the list of available sandbox types.
	Types []string
}

// DetectSandboxCapabilities detects available sandbox types on the current system.
func DetectSandboxCapabilities() SandboxCapabilities {
	caps := SandboxCapabilities{
		Types: make([]string, 0, 4),
	}

	// Docker: check socket or binary
	if dockerAvailable() {
		caps.Types = append(caps.Types, "docker")
	}

	// gVisor: check runsc binary
	if gvisorAvailable() {
		caps.Types = append(caps.Types, "gvisor")
	}

	// Linux namespaces
	if runtime.GOOS == "linux" && namespaceAvailable() {
		caps.Types = append(caps.Types, "namespace")
	}

	// macOS sandbox-exec
	if runtime.GOOS == "darwin" && sandboxExecAvailable() {
		caps.Types = append(caps.Types, "sandbox-exec")
	}

	// If nothing detected, report "none"
	if len(caps.Types) == 0 {
		caps.Types = append(caps.Types, "none")
	}

	return caps
}

// dockerAvailable checks if Docker is available.
func dockerAvailable() bool {
	// Check Docker socket
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}

	// Check Docker binary in PATH
	_, err := exec.LookPath("docker")
	return err == nil
}

// gvisorAvailable checks if gVisor (runsc) is available.
func gvisorAvailable() bool {
	_, err := exec.LookPath("runsc")
	return err == nil
}

// namespaceAvailable checks if Linux namespace isolation is available.
func namespaceAvailable() bool {
	// Check if unshare is available
	_, err := exec.LookPath("unshare")
	if err != nil {
		return false
	}

	// Check if we have sufficient privileges (simplified check)
	// In practice, this would test actual namespace creation
	return os.Getuid() == 0
}

// sandboxExecAvailable checks if macOS sandbox-exec is available.
func sandboxExecAvailable() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// HasCapability checks if a specific sandbox type is available.
func (c SandboxCapabilities) HasCapability(sandboxType string) bool {
	for _, t := range c.Types {
		if t == sandboxType {
			return true
		}
	}
	return false
}

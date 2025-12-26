package sandbox

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Detector provides sandbox environment detection.
type Detector struct {
	// procPath is the path to /proc (can be overridden for testing).
	procPath string

	// sysPath is the path to /sys (can be overridden for testing).
	sysPath string
}

// NewDetector creates a new sandbox detector.
func NewDetector() *Detector {
	return &Detector{
		procPath: "/proc",
		sysPath:  "/sys",
	}
}

// NewDetectorWithPaths creates a detector with custom paths (for testing).
func NewDetectorWithPaths(procPath, sysPath string) *Detector {
	return &Detector{
		procPath: procPath,
		sysPath:  sysPath,
	}
}

// Detect identifies the current sandbox environment.
func (d *Detector) Detect(_ context.Context) (*Environment, error) {
	env := &Environment{
		Type:     TypeNone,
		Mode:     ModeNone,
		Metadata: make(map[string]string),
	}

	// Get hostname
	hostname, err := os.Hostname()
	if err == nil {
		env.Hostname = hostname
	}

	// Check if running on Linux (most sandbox detection only works on Linux)
	if runtime.GOOS != "linux" {
		env.Metadata["os"] = runtime.GOOS
		return env, nil
	}

	// Detect container environment
	d.detectContainer(env)

	// Detect VM environment
	d.detectVM(env)

	// Detect sandbox type
	d.detectSandboxType(env)

	return env, nil
}

// detectContainer checks if we're running inside a container.
func (d *Detector) detectContainer(env *Environment) {
	// Check for /.dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		env.InContainer = true
		env.ContainerRuntime = "docker"
		env.Metadata["docker_env"] = "true"
	}

	// Check for /run/.containerenv (Podman)
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		env.InContainer = true
		if env.ContainerRuntime == "" {
			env.ContainerRuntime = "podman"
		}
		env.Metadata["containerenv"] = "true"
	}

	// Check cgroup for container ID
	cgroupPath := filepath.Join(d.procPath, "1", "cgroup")
	if data, err := os.ReadFile(cgroupPath); err == nil {
		content := string(data)

		// Look for docker container ID pattern
		if strings.Contains(content, "docker") {
			env.InContainer = true
			if env.ContainerRuntime == "" {
				env.ContainerRuntime = "docker"
			}
			// Try to extract container ID
			env.ContainerID = extractContainerID(content, "docker")
		}

		// Look for containerd pattern
		if strings.Contains(content, "containerd") {
			env.InContainer = true
			if env.ContainerRuntime == "" {
				env.ContainerRuntime = "containerd"
			}
		}

		// Look for kubepods (Kubernetes)
		if strings.Contains(content, "kubepods") {
			env.InContainer = true
			env.Metadata["kubernetes"] = "true"
			if env.ContainerRuntime == "" {
				env.ContainerRuntime = "kubernetes"
			}
		}

		// Look for LXC
		if strings.Contains(content, "lxc") {
			env.InContainer = true
			if env.ContainerRuntime == "" {
				env.ContainerRuntime = "lxc"
			}
		}
	}

	// Check /proc/1/sched for container indicators
	schedPath := filepath.Join(d.procPath, "1", "sched")
	if data, err := os.ReadFile(schedPath); err == nil {
		// In a container, PID 1 process name is usually not "systemd" or "init"
		content := string(data)
		if !strings.Contains(content, "systemd") && !strings.Contains(content, "init") {
			// Might be in a container
			if env.ContainerRuntime == "" {
				env.Metadata["sched_hint"] = "possible_container"
			}
		}
	}
}

// detectVM checks if we're running inside a virtual machine.
func (d *Detector) detectVM(env *Environment) {
	// Check /sys/class/dmi/id/product_name for VM indicators
	productPath := filepath.Join(d.sysPath, "class", "dmi", "id", "product_name")
	if data, err := os.ReadFile(productPath); err == nil {
		product := strings.TrimSpace(strings.ToLower(string(data)))
		env.Metadata["product_name"] = product

		vmIndicators := []string{
			"virtualbox", "vmware", "kvm", "qemu",
			"xen", "hyper-v", "parallels", "bhyve",
			"firecracker", "amazon ec2",
		}

		for _, indicator := range vmIndicators {
			if strings.Contains(product, indicator) {
				env.InVM = true
				env.Metadata["vm_type"] = indicator
				break
			}
		}
	}

	// Check /sys/hypervisor/type
	hypervisorPath := filepath.Join(d.sysPath, "hypervisor", "type")
	if data, err := os.ReadFile(hypervisorPath); err == nil {
		hypervisor := strings.TrimSpace(string(data))
		env.InVM = true
		env.Metadata["hypervisor"] = hypervisor
	}

	// Check /proc/cpuinfo for hypervisor flag
	cpuInfoPath := filepath.Join(d.procPath, "cpuinfo")
	if file, err := os.Open(cpuInfoPath); err == nil {
		defer func() { _ = file.Close() }()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "flags") && strings.Contains(line, "hypervisor") {
				env.InVM = true
				env.Metadata["hypervisor_flag"] = "true"
				break
			}
		}
	}
}

// detectSandboxType determines the specific sandbox technology in use.
func (d *Detector) detectSandboxType(env *Environment) {
	if !env.InContainer {
		return
	}

	// Check for gVisor (runsc)
	if d.isGVisor() {
		env.Type = TypeGVisor
		return
	}

	// Check for Firecracker
	if d.isFirecracker(env) {
		env.Type = TypeFirecracker
		return
	}

	// Check for Kata Containers
	if d.isKataContainers() {
		env.Type = TypeKataContainers
		return
	}

	// Default to Docker if in container
	if env.ContainerRuntime != "" {
		env.Type = TypeDocker
	}
}

// isGVisor checks if running under gVisor.
func (d *Detector) isGVisor() bool {
	// gVisor's /proc/version contains "gVisor"
	versionPath := filepath.Join(d.procPath, "version")
	if data, err := os.ReadFile(versionPath); err == nil {
		if strings.Contains(strings.ToLower(string(data)), "gvisor") {
			return true
		}
	}

	// Check for gVisor-specific /sys/kernel/modprobe
	modprobePath := filepath.Join(d.sysPath, "kernel", "modprobe")
	if data, err := os.ReadFile(modprobePath); err == nil {
		if strings.Contains(string(data), "/gvisor") {
			return true
		}
	}

	return false
}

// isFirecracker checks if running in Firecracker microVM.
func (d *Detector) isFirecracker(env *Environment) bool {
	// Check product_name for Firecracker
	if productName, ok := env.Metadata["product_name"]; ok {
		if strings.Contains(strings.ToLower(productName), "firecracker") {
			return true
		}
	}

	// Note: Could add additional Firecracker-specific device checks here
	// Firecracker VMs have limited device tree

	return false
}

// isKataContainers checks if running in Kata Containers.
func (d *Detector) isKataContainers() bool {
	// Kata Containers uses a VM with agent inside
	// Check for kata-agent process or kata-specific markers

	// Check /proc/cmdline for kata indicators
	cmdlinePath := filepath.Join(d.procPath, "cmdline")
	if data, err := os.ReadFile(cmdlinePath); err == nil {
		if strings.Contains(string(data), "kata") {
			return true
		}
	}

	return false
}

// extractContainerID attempts to extract container ID from cgroup content.
func extractContainerID(cgroupContent, _ string) string {
	lines := strings.Split(cgroupContent, "\n")
	for _, line := range lines {
		// Docker format: /docker/<container_id>
		if strings.Contains(line, "/docker/") {
			parts := strings.Split(line, "/docker/")
			if len(parts) > 1 {
				id := strings.TrimSpace(parts[len(parts)-1])
				// Container IDs are 64 hex chars
				if len(id) >= 12 {
					return id[:12] // Return short ID
				}
			}
		}
	}
	return ""
}

// GetAvailableSandboxTypes returns sandbox types available on this system.
func (d *Detector) GetAvailableSandboxTypes(_ context.Context) ([]SandboxType, error) {
	types := make([]SandboxType, 0)

	// Check for Docker
	if d.isDockerAvailable() {
		types = append(types, TypeDocker)
	}

	// Check for gVisor
	if d.isGVisorAvailable() {
		types = append(types, TypeGVisor)
	}

	// Check for Bubblewrap
	if d.isBubblewrapAvailable() {
		types = append(types, TypeBubblewrap)
	}

	// Check for NsJail
	if d.isNsJailAvailable() {
		types = append(types, TypeNsJail)
	}

	return types, nil
}

// isDockerAvailable checks if Docker is available.
func (d *Detector) isDockerAvailable() bool {
	// Check for docker socket
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}
	return false
}

// isGVisorAvailable checks if gVisor (runsc) is available.
func (d *Detector) isGVisorAvailable() bool {
	// Check for runsc binary in common locations
	paths := []string{"/usr/bin/runsc", "/usr/local/bin/runsc"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// isBubblewrapAvailable checks if Bubblewrap is available.
func (d *Detector) isBubblewrapAvailable() bool {
	paths := []string{"/usr/bin/bwrap", "/usr/local/bin/bwrap"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// isNsJailAvailable checks if NsJail is available.
func (d *Detector) isNsJailAvailable() bool {
	paths := []string{"/usr/bin/nsjail", "/usr/local/bin/nsjail"}
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

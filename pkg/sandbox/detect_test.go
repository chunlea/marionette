package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewDetector(t *testing.T) {
	d := NewDetector()
	if d == nil {
		t.Fatal("expected detector to be created")
	}
	if d.procPath != "/proc" {
		t.Errorf("expected procPath /proc, got %s", d.procPath)
	}
	if d.sysPath != "/sys" {
		t.Errorf("expected sysPath /sys, got %s", d.sysPath)
	}
}

func TestNewDetectorWithPaths(t *testing.T) {
	d := NewDetectorWithPaths("/custom/proc", "/custom/sys")
	if d.procPath != "/custom/proc" {
		t.Errorf("expected procPath /custom/proc, got %s", d.procPath)
	}
	if d.sysPath != "/custom/sys" {
		t.Errorf("expected sysPath /custom/sys, got %s", d.sysPath)
	}
}

func TestDetector_Detect_NonLinux(t *testing.T) {
	// This test verifies behavior when not on Linux
	// On non-Linux, detection should return minimal info

	d := NewDetector()
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if env == nil {
		t.Fatal("expected environment to be returned")
	}

	// Should always have a hostname
	// (might be empty on some systems)
	if env.Metadata == nil {
		t.Error("expected metadata map to be initialized")
	}
}

func TestDetector_Detect_WithMockFilesystem(t *testing.T) {
	// Skip if running inside a container, as we can't mock /.dockerenv
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("Skipping test inside Docker container")
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		t.Skip("Skipping test inside Podman container")
	}

	// Create mock filesystem
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	sysDir := filepath.Join(tmpDir, "sys")

	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}
	if err := os.MkdirAll(sysDir, 0755); err != nil {
		t.Fatalf("failed to create sys dir: %v", err)
	}

	d := NewDetectorWithPaths(procDir, sysDir)
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if env == nil {
		t.Fatal("expected environment to be returned")
	}

	// Without any mock files, should detect as non-container
	if env.InContainer {
		t.Error("should not detect as container without mock files")
	}
}

func TestDetector_Detect_DockerContainer(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Container detection tests only run on Linux")
	}

	// Create mock filesystem simulating Docker container
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	sysDir := filepath.Join(tmpDir, "sys")

	// Create /proc/1/cgroup with Docker content
	proc1Dir := filepath.Join(procDir, "1")
	if err := os.MkdirAll(proc1Dir, 0755); err != nil {
		t.Fatalf("failed to create proc/1 dir: %v", err)
	}

	cgroupContent := `12:memory:/docker/abc123def456
11:cpuset:/docker/abc123def456
10:pids:/docker/abc123def456
`
	if err := os.WriteFile(filepath.Join(proc1Dir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write cgroup file: %v", err)
	}

	if err := os.MkdirAll(sysDir, 0755); err != nil {
		t.Fatalf("failed to create sys dir: %v", err)
	}

	d := NewDetectorWithPaths(procDir, sysDir)
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !env.InContainer {
		t.Error("should detect as container with Docker cgroup")
	}

	if env.ContainerRuntime != "docker" {
		t.Errorf("expected runtime 'docker', got '%s'", env.ContainerRuntime)
	}

	if env.ContainerID != "abc123def456" {
		t.Errorf("expected container ID 'abc123def456', got '%s'", env.ContainerID)
	}
}

func TestDetector_Detect_Kubernetes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Container detection tests only run on Linux")
	}

	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	sysDir := filepath.Join(tmpDir, "sys")

	proc1Dir := filepath.Join(procDir, "1")
	if err := os.MkdirAll(proc1Dir, 0755); err != nil {
		t.Fatalf("failed to create proc/1 dir: %v", err)
	}

	cgroupContent := `12:memory:/kubepods/pod123/container456
11:cpuset:/kubepods/pod123/container456
`
	if err := os.WriteFile(filepath.Join(proc1Dir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write cgroup file: %v", err)
	}

	if err := os.MkdirAll(sysDir, 0755); err != nil {
		t.Fatalf("failed to create sys dir: %v", err)
	}

	d := NewDetectorWithPaths(procDir, sysDir)
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !env.InContainer {
		t.Error("should detect as container with Kubernetes cgroup")
	}

	if env.Metadata["kubernetes"] != "true" {
		t.Error("should have kubernetes metadata flag")
	}
}

func TestDetector_Detect_GVisor(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Container detection tests only run on Linux")
	}

	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	sysDir := filepath.Join(tmpDir, "sys")

	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	// Create /proc/version with gVisor indicator
	versionContent := "Linux version 4.4.0 gVisor"
	if err := os.WriteFile(filepath.Join(procDir, "version"), []byte(versionContent), 0644); err != nil {
		t.Fatalf("failed to write version file: %v", err)
	}

	// Also need cgroup to indicate container
	proc1Dir := filepath.Join(procDir, "1")
	if err := os.MkdirAll(proc1Dir, 0755); err != nil {
		t.Fatalf("failed to create proc/1 dir: %v", err)
	}
	cgroupContent := "12:memory:/docker/abc123\n"
	if err := os.WriteFile(filepath.Join(proc1Dir, "cgroup"), []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write cgroup file: %v", err)
	}

	if err := os.MkdirAll(sysDir, 0755); err != nil {
		t.Fatalf("failed to create sys dir: %v", err)
	}

	d := NewDetectorWithPaths(procDir, sysDir)
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if env.Type != TypeGVisor {
		t.Errorf("expected sandbox type gvisor, got %s", env.Type)
	}
}

func TestDetector_Detect_VM(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VM detection tests only run on Linux")
	}

	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")
	sysDir := filepath.Join(tmpDir, "sys")

	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	// Create DMI product_name
	dmiDir := filepath.Join(sysDir, "class", "dmi", "id")
	if err := os.MkdirAll(dmiDir, 0755); err != nil {
		t.Fatalf("failed to create dmi dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("VMware Virtual Platform"), 0644); err != nil {
		t.Fatalf("failed to write product_name: %v", err)
	}

	d := NewDetectorWithPaths(procDir, sysDir)
	ctx := context.Background()

	env, err := d.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if !env.InVM {
		t.Error("should detect as VM with VMware product name")
	}

	if env.Metadata["vm_type"] != "vmware" {
		t.Errorf("expected vm_type 'vmware', got '%s'", env.Metadata["vm_type"])
	}
}

func TestDetector_GetAvailableSandboxTypes(t *testing.T) {
	d := NewDetector()
	ctx := context.Background()

	types, err := d.GetAvailableSandboxTypes(ctx)
	if err != nil {
		t.Fatalf("GetAvailableSandboxTypes failed: %v", err)
	}

	// Should return a non-nil slice (may be empty)
	if types == nil {
		t.Error("expected non-nil slice")
	}

	// The actual contents depend on the system
	t.Logf("Available sandbox types: %v", types)
}

func TestExtractContainerID(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		runtime  string
		expected string
	}{
		{
			name:     "docker container ID",
			content:  "12:memory:/docker/abc123def456789012345678901234567890123456789012345678901234",
			runtime:  "docker",
			expected: "abc123def456",
		},
		{
			name:     "short container ID",
			content:  "12:memory:/docker/abc123def456",
			runtime:  "docker",
			expected: "abc123def456",
		},
		{
			name:     "no docker path",
			content:  "12:memory:/user.slice/user-1000.slice",
			runtime:  "docker",
			expected: "",
		},
		{
			name:     "empty content",
			content:  "",
			runtime:  "docker",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContainerID(tt.content, tt.runtime)
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestSandboxType_String(t *testing.T) {
	tests := []struct {
		st       SandboxType
		expected string
	}{
		{TypeDocker, "docker"},
		{TypeGVisor, "gvisor"},
		{TypeFirecracker, "firecracker"},
		{TypeKataContainers, "kata"},
		{TypeNsJail, "nsjail"},
		{TypeBubblewrap, "bubblewrap"},
		{TypeNone, "none"},
	}

	for _, tt := range tests {
		t.Run(string(tt.st), func(t *testing.T) {
			if string(tt.st) != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, string(tt.st))
			}
		})
	}
}

func TestSandboxMode_String(t *testing.T) {
	tests := []struct {
		sm       SandboxMode
		expected string
	}{
		{ModeRunnerIsSandbox, "runner-is-sandbox"},
		{ModeRunnerCreatesSandbox, "runner-creates-sandbox"},
		{ModeNone, "none"},
	}

	for _, tt := range tests {
		t.Run(string(tt.sm), func(t *testing.T) {
			if string(tt.sm) != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, string(tt.sm))
			}
		})
	}
}

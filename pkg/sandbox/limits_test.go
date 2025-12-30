package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewLimitChecker(t *testing.T) {
	lc := NewLimitChecker()
	if lc == nil {
		t.Fatal("expected limit checker to be created")
	}
	if lc.cgroupPath != "/sys/fs/cgroup" {
		t.Errorf("expected cgroupPath /sys/fs/cgroup, got %s", lc.cgroupPath)
	}
	if lc.procPath != "/proc" {
		t.Errorf("expected procPath /proc, got %s", lc.procPath)
	}
}

func TestNewLimitCheckerWithPaths(t *testing.T) {
	lc := NewLimitCheckerWithPaths("/custom/cgroup", "/custom/proc")
	if lc.cgroupPath != "/custom/cgroup" {
		t.Errorf("expected cgroupPath /custom/cgroup, got %s", lc.cgroupPath)
	}
	if lc.procPath != "/custom/proc" {
		t.Errorf("expected procPath /custom/proc, got %s", lc.procPath)
	}
}

func TestLimitChecker_VerifyResourceLimits_MockCgroupV2(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Cgroup tests only run on Linux")
	}

	tmpDir := t.TempDir()
	cgroupDir := tmpDir
	procDir := filepath.Join(tmpDir, "proc")

	// Create cgroup v2 indicator file
	if err := os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("memory cpu"), 0644); err != nil {
		t.Fatalf("failed to write cgroup.controllers: %v", err)
	}

	// Create memory limits
	if err := os.WriteFile(filepath.Join(cgroupDir, "memory.max"), []byte("1073741824"), 0644); err != nil {
		t.Fatalf("failed to write memory.max: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "memory.current"), []byte("524288000"), 0644); err != nil {
		t.Fatalf("failed to write memory.current: %v", err)
	}

	// Create CPU limits
	if err := os.WriteFile(filepath.Join(cgroupDir, "cpu.max"), []byte("200000 100000"), 0644); err != nil {
		t.Fatalf("failed to write cpu.max: %v", err)
	}

	// Create PID limits
	if err := os.WriteFile(filepath.Join(cgroupDir, "pids.max"), []byte("1000"), 0644); err != nil {
		t.Fatalf("failed to write pids.max: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "pids.current"), []byte("42"), 0644); err != nil {
		t.Fatalf("failed to write pids.current: %v", err)
	}

	// Create proc directory structure
	selfDir := filepath.Join(procDir, "self")
	if err := os.MkdirAll(selfDir, 0755); err != nil {
		t.Fatalf("failed to create proc/self dir: %v", err)
	}

	// Create limits file
	limitsContent := `Limit                     Soft Limit           Hard Limit           Units
Max open files            1024                 4096                 files
Max processes             2048                 4096                 processes
`
	if err := os.WriteFile(filepath.Join(selfDir, "limits"), []byte(limitsContent), 0644); err != nil {
		t.Fatalf("failed to write limits: %v", err)
	}

	// Create mounts file (empty for this test)
	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to write mounts: %v", err)
	}

	lc := NewLimitCheckerWithPaths(cgroupDir, procDir)
	ctx := context.Background()

	result, err := lc.VerifyResourceLimits(ctx)
	if err != nil {
		t.Fatalf("VerifyResourceLimits failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	// Check that we found memory limit
	var memoryLimit *ResourceLimit
	var cpuLimit *ResourceLimit
	var pidLimit *ResourceLimit
	var fileLimit *ResourceLimit

	for i := range result.Limits {
		switch result.Limits[i].Name {
		case "memory":
			memoryLimit = &result.Limits[i]
		case "cpu":
			cpuLimit = &result.Limits[i]
		case "pids":
			pidLimit = &result.Limits[i]
		case "open_files":
			fileLimit = &result.Limits[i]
		}
	}

	if memoryLimit == nil {
		t.Error("expected memory limit check")
	} else if !memoryLimit.Enforced {
		t.Error("expected memory limit to be enforced")
	} else if memoryLimit.Configured != "1.00 GB" {
		t.Errorf("expected memory limit '1.00 GB', got '%s'", memoryLimit.Configured)
	}

	if cpuLimit == nil {
		t.Error("expected CPU limit check")
	} else if !cpuLimit.Enforced {
		t.Error("expected CPU limit to be enforced")
	} else if cpuLimit.Configured != "2.00 cores" {
		t.Errorf("expected CPU limit '2.00 cores', got '%s'", cpuLimit.Configured)
	}

	if pidLimit == nil {
		t.Error("expected PID limit check")
	} else if !pidLimit.Enforced {
		t.Error("expected PID limit to be enforced")
	} else if pidLimit.Configured != "1000" {
		t.Errorf("expected PID limit '1000', got '%s'", pidLimit.Configured)
	}

	if fileLimit == nil {
		t.Error("expected open file limit check")
	} else if !fileLimit.Enforced {
		t.Error("expected file limit to be enforced")
	}
}

func TestLimitChecker_VerifyResourceLimits_MockCgroupV1(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Cgroup tests only run on Linux")
	}

	tmpDir := t.TempDir()
	cgroupDir := tmpDir
	procDir := filepath.Join(tmpDir, "proc")

	// Create cgroup v1 structure (no cgroup.controllers file)
	memoryDir := filepath.Join(cgroupDir, "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("failed to create memory cgroup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "memory.limit_in_bytes"), []byte("2147483648"), 0644); err != nil {
		t.Fatalf("failed to write memory.limit_in_bytes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "memory.usage_in_bytes"), []byte("1073741824"), 0644); err != nil {
		t.Fatalf("failed to write memory.usage_in_bytes: %v", err)
	}

	cpuDir := filepath.Join(cgroupDir, "cpu")
	if err := os.MkdirAll(cpuDir, 0755); err != nil {
		t.Fatalf("failed to create cpu cgroup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_quota_us"), []byte("100000"), 0644); err != nil {
		t.Fatalf("failed to write cpu.cfs_quota_us: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cpuDir, "cpu.cfs_period_us"), []byte("100000"), 0644); err != nil {
		t.Fatalf("failed to write cpu.cfs_period_us: %v", err)
	}

	pidsDir := filepath.Join(cgroupDir, "pids")
	if err := os.MkdirAll(pidsDir, 0755); err != nil {
		t.Fatalf("failed to create pids cgroup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidsDir, "pids.max"), []byte("500"), 0644); err != nil {
		t.Fatalf("failed to write pids.max: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pidsDir, "pids.current"), []byte("10"), 0644); err != nil {
		t.Fatalf("failed to write pids.current: %v", err)
	}

	// Create proc directory structure
	selfDir := filepath.Join(procDir, "self")
	if err := os.MkdirAll(selfDir, 0755); err != nil {
		t.Fatalf("failed to create proc/self dir: %v", err)
	}

	limitsContent := `Limit                     Soft Limit           Hard Limit           Units
Max open files            65536                65536                files
`
	if err := os.WriteFile(filepath.Join(selfDir, "limits"), []byte(limitsContent), 0644); err != nil {
		t.Fatalf("failed to write limits: %v", err)
	}

	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to write mounts: %v", err)
	}

	lc := NewLimitCheckerWithPaths(cgroupDir, procDir)
	ctx := context.Background()

	result, err := lc.VerifyResourceLimits(ctx)
	if err != nil {
		t.Fatalf("VerifyResourceLimits failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	// Verify we got results
	if len(result.Limits) == 0 {
		t.Error("expected at least one limit check")
	}

	t.Logf("Duration: %v", result.Duration)
}

func TestLimitChecker_VerifyResourceLimits_NoLimits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Cgroup tests only run on Linux")
	}

	tmpDir := t.TempDir()
	cgroupDir := tmpDir
	procDir := filepath.Join(tmpDir, "proc")

	// Create minimal structure with no limits
	selfDir := filepath.Join(procDir, "self")
	if err := os.MkdirAll(selfDir, 0755); err != nil {
		t.Fatalf("failed to create proc/self dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to write mounts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(selfDir, "limits"), []byte(""), 0644); err != nil {
		t.Fatalf("failed to write limits: %v", err)
	}

	lc := NewLimitCheckerWithPaths(cgroupDir, procDir)
	ctx := context.Background()

	result, err := lc.VerifyResourceLimits(ctx)
	if err != nil {
		t.Fatalf("VerifyResourceLimits failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	// Should still return limit objects, just not enforced
	if len(result.Limits) == 0 {
		t.Error("expected limit checks even when no limits configured")
	}
}

func TestLimitChecker_GetResourceUsage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Cgroup tests only run on Linux")
	}

	tmpDir := t.TempDir()
	cgroupDir := tmpDir
	procDir := filepath.Join(tmpDir, "proc")

	// Create cgroup v2 files
	if err := os.WriteFile(filepath.Join(cgroupDir, "cgroup.controllers"), []byte("memory cpu"), 0644); err != nil {
		t.Fatalf("failed to write cgroup.controllers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "memory.current"), []byte("1073741824"), 0644); err != nil {
		t.Fatalf("failed to write memory.current: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "pids.current"), []byte("50"), 0644); err != nil {
		t.Fatalf("failed to write pids.current: %v", err)
	}

	cpuStatContent := `usage_usec 12345678
user_usec 10000000
system_usec 2345678
`
	if err := os.WriteFile(filepath.Join(cgroupDir, "cpu.stat"), []byte(cpuStatContent), 0644); err != nil {
		t.Fatalf("failed to write cpu.stat: %v", err)
	}

	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	lc := NewLimitCheckerWithPaths(cgroupDir, procDir)
	ctx := context.Background()

	usage, err := lc.GetResourceUsage(ctx)
	if err != nil {
		t.Fatalf("GetResourceUsage failed: %v", err)
	}

	if usage == nil {
		t.Fatal("expected usage map to be returned")
	}

	if usage["memory_bytes"] != "1073741824" {
		t.Errorf("expected memory_bytes '1073741824', got '%s'", usage["memory_bytes"])
	}

	if usage["memory_human"] != "1.00 GB" {
		t.Errorf("expected memory_human '1.00 GB', got '%s'", usage["memory_human"])
	}

	if usage["pids_current"] != "50" {
		t.Errorf("expected pids_current '50', got '%s'", usage["pids_current"])
	}

	if usage["cpu_usage_usec"] != "12345678" {
		t.Errorf("expected cpu_usage_usec '12345678', got '%s'", usage["cpu_usage_usec"])
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
		{1073741824, "1.00 GB"},
		{1610612736, "1.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatBytes(%d) = '%s', expected '%s'", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestReadIntFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Test valid integer
	validPath := filepath.Join(tmpDir, "valid")
	if err := os.WriteFile(validPath, []byte("12345\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	val, err := readIntFromFile(validPath)
	if err != nil {
		t.Fatalf("readIntFromFile failed: %v", err)
	}
	if val != 12345 {
		t.Errorf("expected 12345, got %d", val)
	}

	// Test non-existent file
	_, err = readIntFromFile(filepath.Join(tmpDir, "nonexistent"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}

	// Test invalid content
	invalidPath := filepath.Join(tmpDir, "invalid")
	if err := os.WriteFile(invalidPath, []byte("not a number"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
	_, err = readIntFromFile(invalidPath)
	if err == nil {
		t.Error("expected error for invalid content")
	}
}

func TestLimitChecker_DetectCgroupVersion(t *testing.T) {
	// Test cgroup v2
	tmpDir := t.TempDir()
	cgroupV2Dir := filepath.Join(tmpDir, "v2")
	if err := os.MkdirAll(cgroupV2Dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cgroupV2Dir, "cgroup.controllers"), []byte("memory cpu"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	lc := NewLimitCheckerWithPaths(cgroupV2Dir, "/proc")
	version := lc.detectCgroupVersion()
	if version != 2 {
		t.Errorf("expected cgroup version 2, got %d", version)
	}

	// Test cgroup v1
	cgroupV1Dir := filepath.Join(tmpDir, "v1")
	if err := os.MkdirAll(cgroupV1Dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	lc = NewLimitCheckerWithPaths(cgroupV1Dir, "/proc")
	version = lc.detectCgroupVersion()
	if version != 1 {
		t.Errorf("expected cgroup version 1, got %d", version)
	}
}

func TestLimitChecker_CheckDiskQuota(t *testing.T) {
	tmpDir := t.TempDir()
	procDir := filepath.Join(tmpDir, "proc")

	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatalf("failed to create proc dir: %v", err)
	}

	// Test with quota-enabled mount
	mountsWithQuota := `/dev/sda1 / ext4 rw,usrquota,grpquota 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
`
	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(mountsWithQuota), 0644); err != nil {
		t.Fatalf("failed to write mounts: %v", err)
	}

	lc := NewLimitCheckerWithPaths(tmpDir, procDir)
	limit := lc.checkDiskQuota()

	if !limit.Enforced {
		t.Error("expected disk quota to be detected as enforced")
	}

	// Test without quota
	mountsNoQuota := `/dev/sda1 / ext4 rw,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
`
	if err := os.WriteFile(filepath.Join(procDir, "mounts"), []byte(mountsNoQuota), 0644); err != nil {
		t.Fatalf("failed to write mounts: %v", err)
	}

	limit = lc.checkDiskQuota()
	if limit.Enforced {
		t.Error("expected disk quota to not be detected as enforced")
	}
}

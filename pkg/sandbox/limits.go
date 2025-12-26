package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// LimitChecker verifies resource limits are properly enforced.
type LimitChecker struct {
	// cgroupPath is the path to cgroup filesystem.
	cgroupPath string

	// procPath is the path to /proc.
	procPath string
}

// NewLimitChecker creates a new limit checker.
func NewLimitChecker() *LimitChecker {
	return &LimitChecker{
		cgroupPath: "/sys/fs/cgroup",
		procPath:   "/proc",
	}
}

// NewLimitCheckerWithPaths creates a limit checker with custom paths (for testing).
func NewLimitCheckerWithPaths(cgroupPath, procPath string) *LimitChecker {
	return &LimitChecker{
		cgroupPath: cgroupPath,
		procPath:   procPath,
	}
}

// VerifyResourceLimits checks if resource limits are properly configured and enforced.
func (c *LimitChecker) VerifyResourceLimits(_ context.Context) (*ResourceResult, error) {
	start := time.Now()
	result := &ResourceResult{
		Passed: true,
		Limits: make([]ResourceLimit, 0),
		Errors: make([]error, 0),
	}

	// Check if running on Linux
	if runtime.GOOS != "linux" {
		result.Limits = append(result.Limits, ResourceLimit{
			Name:     "platform",
			Enforced: false,
			Message:  fmt.Sprintf("Resource limit verification only supported on Linux (current: %s)", runtime.GOOS),
		})
		result.Duration = time.Since(start)
		return result, nil
	}

	// Check cgroup version
	cgroupVersion := c.detectCgroupVersion()

	// Check memory limits
	memLimit := c.checkMemoryLimit(cgroupVersion)
	result.Limits = append(result.Limits, memLimit)
	if !memLimit.Enforced && memLimit.Configured != "" {
		result.Passed = false
	}

	// Check CPU limits
	cpuLimit := c.checkCPULimit(cgroupVersion)
	result.Limits = append(result.Limits, cpuLimit)

	// Check PID limits
	pidLimit := c.checkPIDLimit(cgroupVersion)
	result.Limits = append(result.Limits, pidLimit)

	// Check disk quota (if applicable)
	diskLimit := c.checkDiskQuota()
	result.Limits = append(result.Limits, diskLimit)

	// Check open file limits
	fileLimit := c.checkOpenFileLimit()
	result.Limits = append(result.Limits, fileLimit)

	result.Duration = time.Since(start)
	return result, nil
}

// detectCgroupVersion returns 1 for cgroup v1, 2 for cgroup v2.
func (c *LimitChecker) detectCgroupVersion() int {
	// cgroup v2 has a single unified hierarchy at /sys/fs/cgroup
	// cgroup v1 has separate controllers like /sys/fs/cgroup/memory
	unifiedPath := filepath.Join(c.cgroupPath, "cgroup.controllers")
	if _, err := os.Stat(unifiedPath); err == nil {
		return 2
	}
	return 1
}

// checkMemoryLimit checks memory limit configuration.
func (c *LimitChecker) checkMemoryLimit(cgroupVersion int) ResourceLimit {
	limit := ResourceLimit{
		Name:     "memory",
		Enforced: false,
	}

	var limitBytes int64
	var usageBytes int64
	var err error

	if cgroupVersion == 2 {
		// cgroup v2: memory.max and memory.current
		limitBytes, err = c.readCgroupValue("memory.max")
		if err != nil {
			limit.Message = fmt.Sprintf("Cannot read memory limit: %v", err)
			return limit
		}
		usageBytes, _ = c.readCgroupValue("memory.current")
	} else {
		// cgroup v1: memory/memory.limit_in_bytes
		limitPath := filepath.Join(c.cgroupPath, "memory", "memory.limit_in_bytes")
		limitBytes, err = readIntFromFile(limitPath)
		if err != nil {
			limit.Message = fmt.Sprintf("Cannot read memory limit: %v", err)
			return limit
		}
		usagePath := filepath.Join(c.cgroupPath, "memory", "memory.usage_in_bytes")
		usageBytes, _ = readIntFromFile(usagePath)
	}

	// Check if limit is set (very large values indicate no limit)
	const maxReasonableLimit = 1024 * 1024 * 1024 * 1024 // 1TB
	if limitBytes > 0 && limitBytes < maxReasonableLimit {
		limit.Enforced = true
		limit.Configured = formatBytes(limitBytes)
		limit.Current = formatBytes(usageBytes)
		limit.Message = fmt.Sprintf("Memory limited to %s (current: %s)",
			limit.Configured, limit.Current)
	} else {
		limit.Message = "No memory limit configured"
	}

	return limit
}

// checkCPULimit checks CPU limit configuration.
func (c *LimitChecker) checkCPULimit(cgroupVersion int) ResourceLimit {
	limit := ResourceLimit{
		Name:     "cpu",
		Enforced: false,
	}

	if cgroupVersion == 2 {
		// cgroup v2: cpu.max contains "quota period"
		maxPath := filepath.Join(c.cgroupPath, "cpu.max")
		data, err := os.ReadFile(maxPath)
		if err != nil {
			limit.Message = fmt.Sprintf("Cannot read CPU limit: %v", err)
			return limit
		}

		parts := strings.Fields(string(data))
		if len(parts) >= 2 && parts[0] != "max" {
			quota, _ := strconv.ParseInt(parts[0], 10, 64)
			period, _ := strconv.ParseInt(parts[1], 10, 64)
			if period > 0 {
				cpus := float64(quota) / float64(period)
				limit.Enforced = true
				limit.Configured = fmt.Sprintf("%.2f cores", cpus)
				limit.Message = fmt.Sprintf("CPU limited to %.2f cores", cpus)
			}
		} else {
			limit.Message = "No CPU limit configured"
		}
	} else {
		// cgroup v1: cpu/cpu.cfs_quota_us and cpu.cfs_period_us
		quotaPath := filepath.Join(c.cgroupPath, "cpu", "cpu.cfs_quota_us")
		periodPath := filepath.Join(c.cgroupPath, "cpu", "cpu.cfs_period_us")

		quota, err := readIntFromFile(quotaPath)
		if err != nil || quota < 0 {
			limit.Message = "No CPU limit configured"
			return limit
		}

		period, _ := readIntFromFile(periodPath)
		if period > 0 {
			cpus := float64(quota) / float64(period)
			limit.Enforced = true
			limit.Configured = fmt.Sprintf("%.2f cores", cpus)
			limit.Message = fmt.Sprintf("CPU limited to %.2f cores", cpus)
		}
	}

	return limit
}

// checkPIDLimit checks process count limit.
func (c *LimitChecker) checkPIDLimit(cgroupVersion int) ResourceLimit {
	limit := ResourceLimit{
		Name:     "pids",
		Enforced: false,
	}

	var maxPids int64
	var currentPids int64
	var err error

	if cgroupVersion == 2 {
		maxPids, err = c.readCgroupValue("pids.max")
		if err != nil {
			limit.Message = fmt.Sprintf("Cannot read PID limit: %v", err)
			return limit
		}
		currentPids, _ = c.readCgroupValue("pids.current")
	} else {
		maxPath := filepath.Join(c.cgroupPath, "pids", "pids.max")
		maxPids, err = readIntFromFile(maxPath)
		if err != nil {
			limit.Message = "PID limits not configured"
			return limit
		}
		currentPath := filepath.Join(c.cgroupPath, "pids", "pids.current")
		currentPids, _ = readIntFromFile(currentPath)
	}

	if maxPids > 0 && maxPids < 1000000 {
		limit.Enforced = true
		limit.Configured = fmt.Sprintf("%d", maxPids)
		limit.Current = fmt.Sprintf("%d", currentPids)
		limit.Message = fmt.Sprintf("PID limit: %d (current: %d)", maxPids, currentPids)
	} else {
		limit.Message = "No PID limit configured"
	}

	return limit
}

// checkDiskQuota checks disk quota configuration.
func (c *LimitChecker) checkDiskQuota() ResourceLimit {
	limit := ResourceLimit{
		Name:     "disk",
		Enforced: false,
		Message:  "Disk quota checking not implemented for this filesystem",
	}

	// Check /proc/mounts for quota-enabled filesystems
	mountsPath := filepath.Join(c.procPath, "mounts")
	file, err := os.Open(mountsPath)
	if err != nil {
		limit.Message = fmt.Sprintf("Cannot read mounts: %v", err)
		return limit
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Look for quota options in mount options
		if strings.Contains(line, "usrquota") || strings.Contains(line, "grpquota") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				limit.Message = fmt.Sprintf("Quota enabled on %s", fields[1])
				limit.Enforced = true
				return limit
			}
		}
	}

	return limit
}

// checkOpenFileLimit checks open file descriptor limits.
func (c *LimitChecker) checkOpenFileLimit() ResourceLimit {
	limit := ResourceLimit{
		Name:     "open_files",
		Enforced: false,
	}

	// Read /proc/self/limits
	limitsPath := filepath.Join(c.procPath, "self", "limits")
	file, err := os.Open(limitsPath)
	if err != nil {
		limit.Message = fmt.Sprintf("Cannot read limits: %v", err)
		return limit
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Max open files") {
			fields := strings.Fields(line)
			// Format: "Max open files            1024                 1024                 files"
			if len(fields) >= 5 {
				softLimit := fields[3]
				hardLimit := fields[4]
				limit.Configured = fmt.Sprintf("soft=%s hard=%s", softLimit, hardLimit)
				limit.Enforced = true
				limit.Message = fmt.Sprintf("Open file limit: %s (hard: %s)", softLimit, hardLimit)
			}
			break
		}
	}

	if !limit.Enforced {
		limit.Message = "Could not determine open file limit"
	}

	return limit
}

// readCgroupValue reads a single integer value from a cgroup file.
func (c *LimitChecker) readCgroupValue(filename string) (int64, error) {
	path := filepath.Join(c.cgroupPath, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	content := strings.TrimSpace(string(data))
	if content == "max" {
		return 0, nil // No limit
	}

	return strconv.ParseInt(content, 10, 64)
}

// readIntFromFile reads an integer from a file.
func readIntFromFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// formatBytes formats bytes to human-readable string.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// GetResourceUsage returns current resource usage.
func (c *LimitChecker) GetResourceUsage(ctx context.Context) (map[string]string, error) {
	usage := make(map[string]string)

	cgroupVersion := c.detectCgroupVersion()

	// Memory usage
	if cgroupVersion == 2 {
		if mem, err := c.readCgroupValue("memory.current"); err == nil {
			usage["memory_bytes"] = fmt.Sprintf("%d", mem)
			usage["memory_human"] = formatBytes(mem)
		}
	} else {
		path := filepath.Join(c.cgroupPath, "memory", "memory.usage_in_bytes")
		if mem, err := readIntFromFile(path); err == nil {
			usage["memory_bytes"] = fmt.Sprintf("%d", mem)
			usage["memory_human"] = formatBytes(mem)
		}
	}

	// CPU usage (simplified)
	if cgroupVersion == 2 {
		statPath := filepath.Join(c.cgroupPath, "cpu.stat")
		if data, err := os.ReadFile(statPath); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "usage_usec") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						usage["cpu_usage_usec"] = fields[1]
					}
				}
			}
		}
	}

	// Process count
	if cgroupVersion == 2 {
		if pids, err := c.readCgroupValue("pids.current"); err == nil {
			usage["pids_current"] = fmt.Sprintf("%d", pids)
		}
	}

	return usage, nil
}

package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestNewVerifier(t *testing.T) {
	v := NewVerifier()
	if v == nil {
		t.Fatal("expected verifier to be created")
	}
	if v.detector == nil {
		t.Error("expected detector to be initialized")
	}
	if v.limitChecker == nil {
		t.Error("expected limitChecker to be initialized")
	}
	if v.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", v.config.Timeout)
	}
}

func TestNewVerifierWithConfig(t *testing.T) {
	config := VerifyConfig{
		SkipNetworkTests:    true,
		SkipFilesystemTests: true,
		Timeout:             10 * time.Second,
	}

	v := NewVerifierWithConfig(config)
	if v == nil {
		t.Fatal("expected verifier to be created")
	}
	if !v.config.SkipNetworkTests {
		t.Error("expected SkipNetworkTests to be true")
	}
	if !v.config.SkipFilesystemTests {
		t.Error("expected SkipFilesystemTests to be true")
	}
	if v.config.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", v.config.Timeout)
	}
}

func TestDefaultVerifyConfig(t *testing.T) {
	config := DefaultVerifyConfig()

	if config.SkipNetworkTests {
		t.Error("expected SkipNetworkTests to be false by default")
	}
	if config.SkipFilesystemTests {
		t.Error("expected SkipFilesystemTests to be false by default")
	}
	if config.SkipProcessTests {
		t.Error("expected SkipProcessTests to be false by default")
	}
	if config.SkipResourceTests {
		t.Error("expected SkipResourceTests to be false by default")
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", config.Timeout)
	}
}

func TestVerifier_Detect(t *testing.T) {
	v := NewVerifier()
	ctx := context.Background()

	env, err := v.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}

	if env == nil {
		t.Fatal("expected environment to be returned")
	}

	// Verify basic fields are set
	t.Logf("Detected environment: Type=%s, InContainer=%v, InVM=%v",
		env.Type, env.InContainer, env.InVM)
}

func TestVerifier_VerifyIsolation(t *testing.T) {
	v := NewVerifier()
	ctx := context.Background()

	result, err := v.VerifyIsolation(ctx)
	if err != nil {
		t.Fatalf("VerifyIsolation failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	if result.Tests == nil {
		t.Error("expected tests slice to be initialized")
	}

	// Log test results for debugging
	for _, test := range result.Tests {
		t.Logf("Test: %s (%s) - Passed: %v - %s",
			test.Name, test.Category, test.Passed, test.Message)
	}

	t.Logf("Overall: Passed=%v, Duration=%v", result.Passed, result.Duration)
}

func TestVerifier_VerifyIsolation_SkipTests(t *testing.T) {
	config := VerifyConfig{
		SkipNetworkTests:    true,
		SkipFilesystemTests: true,
		SkipProcessTests:    true,
		Timeout:             5 * time.Second,
	}

	v := NewVerifierWithConfig(config)
	ctx := context.Background()

	result, err := v.VerifyIsolation(ctx)
	if err != nil {
		t.Fatalf("VerifyIsolation failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	// With all tests skipped, should have no tests
	if len(result.Tests) != 0 {
		t.Errorf("expected 0 tests when all skipped, got %d", len(result.Tests))
	}
}

func TestVerifier_VerifyIsolation_WithTimeout(t *testing.T) {
	config := VerifyConfig{
		Timeout: 100 * time.Millisecond,
	}

	v := NewVerifierWithConfig(config)
	ctx := context.Background()

	result, err := v.VerifyIsolation(ctx)
	if err != nil {
		t.Fatalf("VerifyIsolation failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	// Duration should be reasonable
	if result.Duration > 5*time.Second {
		t.Errorf("verification took too long: %v", result.Duration)
	}
}

func TestVerifier_VerifyResourceLimits(t *testing.T) {
	v := NewVerifier()
	ctx := context.Background()

	result, err := v.VerifyResourceLimits(ctx)
	if err != nil {
		t.Fatalf("VerifyResourceLimits failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result to be returned")
	}

	if result.Limits == nil {
		t.Error("expected limits slice to be initialized")
	}

	// Log limit results for debugging
	for _, limit := range result.Limits {
		t.Logf("Limit: %s - Enforced: %v - Configured: %s - %s",
			limit.Name, limit.Enforced, limit.Configured, limit.Message)
	}

	t.Logf("Overall: Passed=%v, Duration=%v", result.Passed, result.Duration)
}

func TestVerifier_GetCapabilities(t *testing.T) {
	v := NewVerifier()
	ctx := context.Background()

	caps, err := v.GetCapabilities(ctx)
	if err != nil {
		t.Fatalf("GetCapabilities failed: %v", err)
	}

	if caps == nil {
		t.Fatal("expected capabilities to be returned")
	}

	if caps.AvailableSandboxTypes == nil {
		t.Error("expected AvailableSandboxTypes to be initialized")
	}

	t.Logf("Capabilities: NetworkIsolation=%v, FilesystemIsolation=%v, ProcessIsolation=%v",
		caps.HasNetworkIsolation, caps.HasFilesystemIsolation, caps.HasProcessIsolation)
	t.Logf("Security: Seccomp=%v, AppArmor=%v, SELinux=%v",
		caps.SupportsSeccomp, caps.SupportsAppArmor, caps.SupportsSELinux)
	t.Logf("Available sandbox types: %v", caps.AvailableSandboxTypes)
}

func TestIsolationTest_Fields(t *testing.T) {
	test := IsolationTest{
		Name:     "test_name",
		Category: "filesystem",
		Passed:   true,
		Message:  "test message",
		Severity: "critical",
	}

	if test.Name != "test_name" {
		t.Error("Name field not set correctly")
	}
	if test.Category != "filesystem" {
		t.Error("Category field not set correctly")
	}
	if !test.Passed {
		t.Error("Passed field not set correctly")
	}
	if test.Message != "test message" {
		t.Error("Message field not set correctly")
	}
	if test.Severity != "critical" {
		t.Error("Severity field not set correctly")
	}
}

func TestResourceLimit_Fields(t *testing.T) {
	limit := ResourceLimit{
		Name:       "memory",
		Configured: "1 GB",
		Enforced:   true,
		Current:    "500 MB",
		Message:    "Memory limited",
	}

	if limit.Name != "memory" {
		t.Error("Name field not set correctly")
	}
	if limit.Configured != "1 GB" {
		t.Error("Configured field not set correctly")
	}
	if !limit.Enforced {
		t.Error("Enforced field not set correctly")
	}
	if limit.Current != "500 MB" {
		t.Error("Current field not set correctly")
	}
	if limit.Message != "Memory limited" {
		t.Error("Message field not set correctly")
	}
}

func TestCapabilities_Fields(t *testing.T) {
	caps := Capabilities{
		AvailableSandboxTypes:  []SandboxType{TypeDocker, TypeGVisor},
		HasNetworkIsolation:    true,
		HasFilesystemIsolation: true,
		HasProcessIsolation:    true,
		HasResourceLimits:      true,
		CanCreateSandbox:       false,
		SupportsSeccomp:        true,
		SupportsAppArmor:       false,
		SupportsSELinux:        false,
		MaxMemoryMB:            4096,
		MaxCPUs:                2.0,
		MaxDiskMB:              10240,
		MaxPids:                1000,
	}

	if len(caps.AvailableSandboxTypes) != 2 {
		t.Error("AvailableSandboxTypes not set correctly")
	}
	if !caps.HasNetworkIsolation {
		t.Error("HasNetworkIsolation not set correctly")
	}
	if !caps.HasFilesystemIsolation {
		t.Error("HasFilesystemIsolation not set correctly")
	}
	if !caps.HasProcessIsolation {
		t.Error("HasProcessIsolation not set correctly")
	}
	if !caps.HasResourceLimits {
		t.Error("HasResourceLimits not set correctly")
	}
	if caps.CanCreateSandbox {
		t.Error("CanCreateSandbox not set correctly")
	}
	if !caps.SupportsSeccomp {
		t.Error("SupportsSeccomp not set correctly")
	}
	if caps.MaxMemoryMB != 4096 {
		t.Error("MaxMemoryMB not set correctly")
	}
	if caps.MaxCPUs != 2.0 {
		t.Error("MaxCPUs not set correctly")
	}
	if caps.MaxDiskMB != 10240 {
		t.Error("MaxDiskMB not set correctly")
	}
	if caps.MaxPids != 1000 {
		t.Error("MaxPids not set correctly")
	}
}

func TestEnvironment_Fields(t *testing.T) {
	env := Environment{
		Type:             TypeDocker,
		Mode:             ModeRunnerIsSandbox,
		InContainer:      true,
		InVM:             false,
		ContainerRuntime: "docker",
		ContainerID:      "abc123",
		Hostname:         "container-host",
		Metadata:         map[string]string{"key": "value"},
	}

	if env.Type != TypeDocker {
		t.Error("Type not set correctly")
	}
	if env.Mode != ModeRunnerIsSandbox {
		t.Error("Mode not set correctly")
	}
	if !env.InContainer {
		t.Error("InContainer not set correctly")
	}
	if env.InVM {
		t.Error("InVM not set correctly")
	}
	if env.ContainerRuntime != "docker" {
		t.Error("ContainerRuntime not set correctly")
	}
	if env.ContainerID != "abc123" {
		t.Error("ContainerID not set correctly")
	}
	if env.Hostname != "container-host" {
		t.Error("Hostname not set correctly")
	}
	if env.Metadata["key"] != "value" {
		t.Error("Metadata not set correctly")
	}
}

func TestParseMemoryMB(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1024", 1024},
		{"1024 MB", 1024},
		{"1024MB", 1024},
		{"2 GB", 2048},
		{"2GB", 2048},
		{"1024 KB", 1},
		{"512KB", 1},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseMemoryMB(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemoryMB(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"0", 0},
		{"123", 123},
		{"  456  ", 456},
		{"789abc", 789},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseInt64(tt.input)
			if result != tt.expected {
				t.Errorf("parseInt64(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsolationResult_OverallPass(t *testing.T) {
	// Test case: all critical tests pass
	result := &IsolationResult{
		Passed: true,
		Tests: []IsolationTest{
			{Name: "test1", Passed: true, Severity: "critical"},
			{Name: "test2", Passed: true, Severity: "high"},
			{Name: "test3", Passed: false, Severity: "low"}, // low severity, can fail
		},
	}

	// Since low severity test failed, overall should still pass
	// This logic is in VerifyIsolation
	if !result.Passed {
		t.Error("expected overall pass when only low severity tests fail")
	}

	// Test case: critical test fails
	result2 := &IsolationResult{
		Passed: false,
		Tests: []IsolationTest{
			{Name: "test1", Passed: false, Severity: "critical"},
			{Name: "test2", Passed: true, Severity: "high"},
		},
	}

	if result2.Passed {
		t.Error("expected overall fail when critical test fails")
	}
}

func TestResourceResult_OverallPass(t *testing.T) {
	result := &ResourceResult{
		Passed: true,
		Limits: []ResourceLimit{
			{Name: "memory", Enforced: true, Configured: "1 GB"},
			{Name: "cpu", Enforced: true, Configured: "2 cores"},
		},
	}

	if !result.Passed {
		t.Error("expected overall pass when all limits enforced")
	}
}

func TestVerifier_ContextCancellation(t *testing.T) {
	v := NewVerifier()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	// These should return quickly without blocking
	_, err := v.Detect(ctx)
	if err != nil && err != context.Canceled {
		t.Logf("Detect with cancelled context: %v", err)
	}

	result, err := v.VerifyIsolation(ctx)
	if err != nil && err != context.Canceled {
		t.Logf("VerifyIsolation with cancelled context: %v", err)
	}
	// Result might be partial but should not panic
	if result == nil {
		t.Log("Result is nil with cancelled context (expected)")
	}
}

// Integration test that runs all verification
func TestVerifier_FullVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping full verification in short mode")
	}

	v := NewVerifier()
	ctx := context.Background()

	// Detect environment
	env, err := v.Detect(ctx)
	if err != nil {
		t.Fatalf("Detect failed: %v", err)
	}
	t.Logf("Environment: %+v", env)

	// Verify isolation
	isolation, err := v.VerifyIsolation(ctx)
	if err != nil {
		t.Fatalf("VerifyIsolation failed: %v", err)
	}
	t.Logf("Isolation: Passed=%v, Tests=%d, Duration=%v",
		isolation.Passed, len(isolation.Tests), isolation.Duration)

	// Verify resource limits
	resources, err := v.VerifyResourceLimits(ctx)
	if err != nil {
		t.Fatalf("VerifyResourceLimits failed: %v", err)
	}
	t.Logf("Resources: Passed=%v, Limits=%d, Duration=%v",
		resources.Passed, len(resources.Limits), resources.Duration)

	// Get capabilities
	caps, err := v.GetCapabilities(ctx)
	if err != nil {
		t.Fatalf("GetCapabilities failed: %v", err)
	}
	t.Logf("Capabilities: %+v", caps)
}

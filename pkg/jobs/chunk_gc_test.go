package jobs

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/storage/cas"
)

// mockGC implements cas.GarbageCollector for testing.
type mockGC struct {
	mu          sync.Mutex
	markCalls   int
	sweepCalls  int
	runGCCalls  int
	markResult  int
	sweepResult int
	sweepBytes  int64
	lastResult  *cas.GCResult
	runErr      error
}

func newMockGC() *mockGC {
	return &mockGC{
		markResult:  5,
		sweepResult: 3,
		sweepBytes:  1000,
	}
}

func (m *mockGC) Mark(_ context.Context, _ string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markCalls++
	return m.markResult, nil
}

func (m *mockGC) Sweep(_ context.Context, _ string) (int, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepCalls++
	return m.sweepResult, m.sweepBytes, nil
}

func (m *mockGC) Resurrect(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockGC) RunGC(_ context.Context, _ string) (*cas.GCResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runGCCalls++

	if m.runErr != nil {
		return nil, m.runErr
	}

	result := &cas.GCResult{
		ChunksMarked:  m.markResult,
		ChunksDeleted: m.sweepResult,
		BytesFreed:    m.sweepBytes,
		Duration:      100 * time.Millisecond,
	}
	m.lastResult = result
	return result, nil
}

func (m *mockGC) getRunGCCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runGCCalls
}

// TestChunkGCJob_StartStop tests basic job lifecycle.
func TestChunkGCJob_StartStop(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: 100 * time.Millisecond,
		Logger:   slog.Default(),
	})

	ctx := context.Background()

	// Start the job
	if err := job.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !job.IsRunning() {
		t.Error("job should be running")
	}

	// Wait for at least one GC run
	time.Sleep(50 * time.Millisecond)

	// Stop the job
	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	if err := job.Stop(stopCtx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if job.IsRunning() {
		t.Error("job should not be running")
	}

	// Verify GC was called at least once (on start)
	if gc.getRunGCCalls() < 1 {
		t.Errorf("expected at least 1 GC call, got %d", gc.getRunGCCalls())
	}
}

// TestChunkGCJob_DoubleStart tests that starting twice fails.
func TestChunkGCJob_DoubleStart(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: time.Hour,
	})

	ctx := context.Background()

	if err := job.Start(ctx); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	defer func() { _ = job.Stop(ctx) }()

	if err := job.Start(ctx); err == nil {
		t.Error("Second Start should fail")
	}
}

// TestChunkGCJob_RunNow tests manual GC trigger.
func TestChunkGCJob_RunNow(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: time.Hour, // Long interval so periodic doesn't interfere
	})

	ctx := context.Background()

	if err := job.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = job.Stop(ctx) }()

	// Initial call count (from start)
	initialCalls := gc.getRunGCCalls()

	// Trigger manual run
	result, err := job.RunNow(ctx)
	if err != nil {
		t.Fatalf("RunNow failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result")
	}

	if result.ChunksMarked != gc.markResult {
		t.Errorf("expected %d chunks marked, got %d", gc.markResult, result.ChunksMarked)
	}

	// Verify GC was called
	if gc.getRunGCCalls() <= initialCalls {
		t.Error("RunNow should have called GC")
	}
}

// TestChunkGCJob_LastResult tests that last result is tracked.
func TestChunkGCJob_LastResult(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: time.Hour,
	})

	ctx := context.Background()

	if err := job.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() { _ = job.Stop(ctx) }()

	// Wait for initial run to complete
	time.Sleep(50 * time.Millisecond)

	result := job.LastResult()
	if result == nil {
		t.Fatal("expected last result")
	}

	if result.ChunksMarked != gc.markResult {
		t.Errorf("expected %d chunks marked, got %d", gc.markResult, result.ChunksMarked)
	}

	lastRun := job.LastRun()
	if lastRun.IsZero() {
		t.Error("expected last run time to be set")
	}
}

// TestChunkGCJob_PeriodicRuns tests that GC runs periodically.
func TestChunkGCJob_PeriodicRuns(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: 50 * time.Millisecond, // Short interval for testing
	})

	ctx := context.Background()

	if err := job.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for multiple runs
	time.Sleep(200 * time.Millisecond)

	if err := job.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Should have run multiple times (initial + periodic)
	calls := gc.getRunGCCalls()
	if calls < 2 {
		t.Errorf("expected at least 2 GC calls, got %d", calls)
	}
}

// TestChunkGCScheduler_AddRemoveTenant tests scheduler tenant management.
func TestChunkGCScheduler_AddRemoveTenant(t *testing.T) {
	scheduler := NewChunkGCScheduler(
		func(_ string) cas.GarbageCollector { return newMockGC() },
		time.Hour,
		slog.Default(),
	)

	ctx := context.Background()

	// Add tenant
	if err := scheduler.AddTenant(ctx, "tenant1"); err != nil {
		t.Fatalf("AddTenant failed: %v", err)
	}

	tenants := scheduler.ListTenants()
	if len(tenants) != 1 || tenants[0] != "tenant1" {
		t.Errorf("expected [tenant1], got %v", tenants)
	}

	// Adding same tenant again should fail
	if err := scheduler.AddTenant(ctx, "tenant1"); err == nil {
		t.Error("Adding duplicate tenant should fail")
	}

	// Add another tenant
	if err := scheduler.AddTenant(ctx, "tenant2"); err != nil {
		t.Fatalf("AddTenant failed: %v", err)
	}

	tenants = scheduler.ListTenants()
	if len(tenants) != 2 {
		t.Errorf("expected 2 tenants, got %d", len(tenants))
	}

	// Remove tenant
	if err := scheduler.RemoveTenant(ctx, "tenant1"); err != nil {
		t.Fatalf("RemoveTenant failed: %v", err)
	}

	tenants = scheduler.ListTenants()
	if len(tenants) != 1 {
		t.Errorf("expected 1 tenant, got %d", len(tenants))
	}

	// Cleanup
	if err := scheduler.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

// TestChunkGCScheduler_RunGCForTenant tests manual GC trigger per tenant.
func TestChunkGCScheduler_RunGCForTenant(t *testing.T) {
	mockGCs := make(map[string]*mockGC)
	scheduler := NewChunkGCScheduler(
		func(tenantID string) cas.GarbageCollector {
			gc := newMockGC()
			mockGCs[tenantID] = gc
			return gc
		},
		time.Hour,
		slog.Default(),
	)

	ctx := context.Background()
	defer func() { _ = scheduler.Stop(ctx) }()

	if err := scheduler.AddTenant(ctx, "tenant1"); err != nil {
		t.Fatalf("AddTenant failed: %v", err)
	}

	// Wait for initial run
	time.Sleep(50 * time.Millisecond)

	initialCalls := mockGCs["tenant1"].getRunGCCalls()

	result, err := scheduler.RunGCForTenant(ctx, "tenant1")
	if err != nil {
		t.Fatalf("RunGCForTenant failed: %v", err)
	}

	if result == nil {
		t.Fatal("expected result")
	}

	if mockGCs["tenant1"].getRunGCCalls() <= initialCalls {
		t.Error("RunGCForTenant should have called GC")
	}

	// Try non-existent tenant
	_, err = scheduler.RunGCForTenant(ctx, "nonexistent")
	if err == nil {
		t.Error("RunGCForTenant for non-existent tenant should fail")
	}
}

// TestChunkGCScheduler_Stop tests stopping all jobs.
func TestChunkGCScheduler_Stop(t *testing.T) {
	scheduler := NewChunkGCScheduler(
		func(_ string) cas.GarbageCollector { return newMockGC() },
		time.Hour,
		slog.Default(),
	)

	ctx := context.Background()

	// Add multiple tenants
	for i := 0; i < 3; i++ {
		if err := scheduler.AddTenant(ctx, "tenant"+string(rune('0'+i))); err != nil {
			t.Fatalf("AddTenant failed: %v", err)
		}
	}

	if len(scheduler.ListTenants()) != 3 {
		t.Error("expected 3 tenants")
	}

	// Stop all
	if err := scheduler.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if len(scheduler.ListTenants()) != 0 {
		t.Error("expected no tenants after stop")
	}
}

// TestChunkGCJob_DefaultConfig tests that defaults are applied.
func TestChunkGCJob_DefaultConfig(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		// Interval not set - should default
		// Logger not set - should default
	})

	// Should not panic and should use defaults
	if job.interval != 24*time.Hour {
		t.Errorf("expected default interval of 24h, got %v", job.interval)
	}

	if job.logger == nil {
		t.Error("expected default logger")
	}
}

// TestChunkGCJob_ContextCancellation tests job respects context.
func TestChunkGCJob_ContextCancellation(t *testing.T) {
	gc := newMockGC()
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "tenant1",
		Interval: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())

	if err := job.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel the context
	cancel()

	// Job should stop
	time.Sleep(100 * time.Millisecond)

	if job.IsRunning() {
		t.Error("job should have stopped on context cancellation")
	}
}

package jobs

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/chunlea/marionette/pkg/storage/cas"
)

// tenantRecordingGC records which tenants collection was asked to run for, and
// can be told to fail for some of them.
type tenantRecordingGC struct {
	mu      sync.Mutex
	seen    []string
	failFor map[string]error
	perRun  cas.GCResult
}

func newTenantRecordingGC() *tenantRecordingGC {
	return &tenantRecordingGC{
		failFor: make(map[string]error),
		perRun:  cas.GCResult{ChunksMarked: 2, ChunksDeleted: 1, BytesFreed: 100},
	}
}

func (g *tenantRecordingGC) Mark(context.Context, string) (int, error) { return 0, nil }

func (g *tenantRecordingGC) Sweep(context.Context, string) (int, int64, error) { return 0, 0, nil }

func (g *tenantRecordingGC) Resurrect(context.Context, string, string) error { return nil }

func (g *tenantRecordingGC) RunGC(_ context.Context, tenantID string) (*cas.GCResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.seen = append(g.seen, tenantID)
	if err, ok := g.failFor[tenantID]; ok {
		return nil, err
	}

	result := g.perRun
	return &result, nil
}

func (g *tenantRecordingGC) tenants() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.seen...)
}

type staticTenantLister struct {
	tenants []string
	err     error
	calls   int
}

func (l *staticTenantLister) ListChunkTenants(context.Context) ([]string, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	return l.tenants, nil
}

func TestChunkGCCollectsEveryTenant(t *testing.T) {
	gc := newTenantRecordingGC()
	lister := &staticTenantLister{tenants: []string{"acme", "globex", "initech"}}

	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		Tenants: lister,
		Logger:  slog.New(slog.DiscardHandler),
	})

	result, err := job.RunNow(context.Background())
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}

	seen := gc.tenants()
	if len(seen) != 3 {
		t.Fatalf("collected %v, want all three tenants", seen)
	}

	// Totals are summed across tenants, not reported per tenant.
	if result.ChunksDeleted != 3 || result.ChunksMarked != 6 || result.BytesFreed != 300 {
		t.Errorf("result = %+v, want the per-tenant runs summed", result)
	}
}

// TestChunkGCWildcardTenantMeansEveryTenant pins the documented "*" spelling.
func TestChunkGCWildcardTenantMeansEveryTenant(t *testing.T) {
	gc := newTenantRecordingGC()
	lister := &staticTenantLister{tenants: []string{"acme", "globex"}}

	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "*",
		Tenants:  lister,
		Logger:   slog.New(slog.DiscardHandler),
	})

	if _, err := job.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}

	seen := gc.tenants()
	if len(seen) != 2 {
		t.Fatalf("collected %v, want both tenants", seen)
	}
	for _, tenant := range seen {
		if tenant == "*" {
			t.Fatal(`"*" was passed through as a tenant id instead of being expanded`)
		}
	}
}

func TestChunkGCSingleTenantDoesNotEnumerate(t *testing.T) {
	gc := newTenantRecordingGC()
	lister := &staticTenantLister{tenants: []string{"acme", "globex"}}

	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: "acme",
		Tenants:  lister,
		Logger:   slog.New(slog.DiscardHandler),
	})

	if _, err := job.RunNow(context.Background()); err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}

	if seen := gc.tenants(); len(seen) != 1 || seen[0] != "acme" {
		t.Errorf("collected %v, want only acme", seen)
	}
	if lister.calls != 0 {
		t.Errorf("the tenant lister was consulted for a single-tenant job")
	}
}

// TestChunkGCOneTenantFailureDoesNotStopTheRest matters because these run
// unattended: a single broken tenant must not stall collection everywhere.
func TestChunkGCOneTenantFailureDoesNotStopTheRest(t *testing.T) {
	boom := errors.New("tenant storage unavailable")

	gc := newTenantRecordingGC()
	gc.failFor["globex"] = boom
	lister := &staticTenantLister{tenants: []string{"acme", "globex", "initech"}}

	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		Tenants: lister,
		Logger:  slog.New(slog.DiscardHandler),
	})

	result, err := job.RunNow(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("RunNow() error = %v, want it to surface %v", err, boom)
	}

	if seen := gc.tenants(); len(seen) != 3 {
		t.Errorf("collected %v, want the run to continue past the failure", seen)
	}
	// The two healthy tenants were still collected.
	if result.ChunksDeleted != 2 {
		t.Errorf("ChunksDeleted = %d, want the healthy tenants counted", result.ChunksDeleted)
	}
}

func TestChunkGCAllTenantsRequiresALister(t *testing.T) {
	job := NewChunkGCJob(newTenantRecordingGC(), ChunkGCJobConfig{
		Logger: slog.New(slog.DiscardHandler),
	})

	if _, err := job.RunNow(context.Background()); err == nil {
		t.Fatal("collecting every tenant with no way to enumerate them should fail loudly")
	}
}

func TestChunkGCTenantListFailurePropagates(t *testing.T) {
	boom := errors.New("database down")
	job := NewChunkGCJob(newTenantRecordingGC(), ChunkGCJobConfig{
		Tenants: &staticTenantLister{err: boom},
		Logger:  slog.New(slog.DiscardHandler),
	})

	_, err := job.RunNow(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("RunNow() error = %v, want %v", err, boom)
	}
}

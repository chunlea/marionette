package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/jobs"
	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// Doubles
// =============================================================================

// memoryBlobs is an in-memory blob backend.
type memoryBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte

	// failUpload aborts the next upload, which is how the tests reach the crash
	// window between "object written" and "row committed".
	failUpload bool
	failDelete bool
}

func newMemoryBlobs() *memoryBlobs {
	return &memoryBlobs{objects: map[string][]byte{}}
}

func (m *memoryBlobs) Name() string { return "memory" }

func (m *memoryBlobs) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpload {
		return errors.New("blob store unavailable")
	}
	m.objects[key] = buf.Bytes()
	return nil
}

func (m *memoryBlobs) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (m *memoryBlobs) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete {
		return errors.New("blob store unavailable")
	}
	delete(m.objects, key)
	return nil
}

func (m *memoryBlobs) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

func (m *memoryBlobs) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fakeArchiveStore is an in-memory stand-in for the archive half of the store.
//
// It models the two behaviours the archiver depends on and nothing else: logs
// ordered by (created_at, sequence, id), and one archive row per session.
type fakeArchiveStore struct {
	mu         sync.Mutex
	candidates []store.LogArchiveCandidate
	logs       []*store.Log
	archives   map[string]*store.LogArchive // keyed by archive id

	failCommit bool
	failDelete bool

	nextID int
}

func newFakeArchiveStore() *fakeArchiveStore {
	return &fakeArchiveStore{archives: map[string]*store.LogArchive{}}
}

func (f *fakeArchiveStore) ListLogArchiveCandidates(
	_ context.Context, opts store.ListLogArchiveCandidatesOptions,
) ([]store.LogArchiveCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]store.LogArchiveCandidate, 0, len(f.candidates))
	for i, c := range f.candidates {
		if opts.Limit > 0 && i >= opts.Limit {
			break
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeArchiveStore) ListSessionLogsAfter(
	_ context.Context, sessionID string, after *store.LogCursor, before time.Time, limit int,
) ([]*store.Log, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	sorted := append([]*store.Log(nil), f.logs...)
	sort.Slice(sorted, func(i, j int) bool { return logLess(sorted[i], sorted[j]) })

	var out []*store.Log
	for _, l := range sorted {
		if l.SessionID != sessionID || !l.CreatedAt.Before(before) {
			continue
		}
		if after != nil && !cursorLess(*after, l) {
			continue
		}
		out = append(out, l)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeArchiveStore) DeleteSessionLogsThrough(
	_ context.Context, sessionID string, through store.LogCursor, _ int,
) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete {
		return 0, errors.New("delete failed")
	}

	var kept []*store.Log
	var deleted int64
	for _, l := range f.logs {
		if l.SessionID == sessionID && !cursorLess(through, l) {
			deleted++
			continue
		}
		kept = append(kept, l)
	}
	f.logs = kept
	return deleted, nil
}

func (f *fakeArchiveStore) CountSessionLogsThrough(
	_ context.Context, sessionID string, through store.LogCursor,
) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for _, l := range f.logs {
		if l.SessionID == sessionID && !cursorLess(through, l) {
			n++
		}
	}
	return n, nil
}

func (f *fakeArchiveStore) GetLogArchiveBySession(_ context.Context, sessionID string) (*store.LogArchive, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.archives {
		if a.SessionID == sessionID {
			clone := *a
			return &clone, nil
		}
	}
	return nil, fmt.Errorf("%w: log_archive for %s", store.ErrNotFound, sessionID)
}

func (f *fakeArchiveStore) CreateLogArchive(_ context.Context, a *store.LogArchive) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCommit {
		return errors.New("commit failed")
	}
	f.nextID++
	a.ID = fmt.Sprintf("arch_%d", f.nextID)
	a.ArchivedAt = time.Now()
	clone := *a
	f.archives[a.ID] = &clone
	return nil
}

func (f *fakeArchiveStore) UpdateLogArchive(_ context.Context, id string, u store.LogArchiveUpdates) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCommit {
		return errors.New("commit failed")
	}
	a, ok := f.archives[id]
	if !ok {
		return fmt.Errorf("%w: log_archive %s", store.ErrNotFound, id)
	}
	if u.DeletedAt != nil {
		a.DeletedAt = u.DeletedAt
	}
	if u.StorageKey != nil {
		a.StorageKey = *u.StorageKey
	}
	if u.StorageSizeBytes != nil {
		a.StorageSizeBytes = u.StorageSizeBytes
	}
	if u.LogCount != nil {
		a.LogCount = *u.LogCount
	}
	if u.FirstLogAt != nil {
		a.FirstLogAt = u.FirstLogAt
	}
	if u.LastLogAt != nil {
		a.LastLogAt = u.LastLogAt
	}
	if u.LastLogID != nil {
		a.LastLogID = u.LastLogID
	}
	if u.LastLogSequence != nil {
		a.LastLogSequence = u.LastLogSequence
	}
	if u.ExpiresAt != nil {
		a.ExpiresAt = u.ExpiresAt
	}
	if u.Format != nil {
		a.Format = *u.Format
	}
	if u.Encrypted != nil {
		a.Encrypted = *u.Encrypted
	}
	return nil
}

func (f *fakeArchiveStore) DeleteLogArchive(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.archives, id)
	return nil
}

func (f *fakeArchiveStore) ListExpiredLogArchives(_ context.Context, _ int) ([]*store.LogArchive, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	var out []*store.LogArchive
	for _, a := range f.archives {
		if a.DeletedAt == nil && a.ExpiresAt != nil && !a.ExpiresAt.After(now) {
			clone := *a
			out = append(out, &clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeArchiveStore) ListDeletedLogArchives(_ context.Context, _ int) ([]*store.LogArchive, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.LogArchive
	for _, a := range f.archives {
		if a.DeletedAt != nil {
			clone := *a
			out = append(out, &clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeArchiveStore) archiveFor(sessionID string) *store.LogArchive {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.archives {
		if a.SessionID == sessionID {
			clone := *a
			return &clone
		}
	}
	return nil
}

func (f *fakeArchiveStore) logCount(sessionID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, l := range f.logs {
		if l.SessionID == sessionID {
			n++
		}
	}
	return n
}

func logLess(a, b *store.Log) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if a.Sequence != b.Sequence {
		return a.Sequence < b.Sequence
	}
	return a.ID < b.ID
}

// cursorLess reports whether c is strictly before l.
func cursorLess(c store.LogCursor, l *store.Log) bool {
	if !c.CreatedAt.Equal(l.CreatedAt) {
		return c.CreatedAt.Before(l.CreatedAt)
	}
	if c.Sequence != l.Sequence {
		return c.Sequence < l.Sequence
	}
	return c.ID < l.ID
}

// =============================================================================
// Fixtures
// =============================================================================

var archiveBase = time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

func seedLogs(sessionID string, from, count int) []*store.Log {
	out := make([]*store.Log, 0, count)
	for i := from; i < from+count; i++ {
		out = append(out, &store.Log{
			ID:        fmt.Sprintf("%s_log_%04d", sessionID, i),
			SessionID: sessionID,
			TaskID:    "task_1",
			RunID:     "run_1",
			RunnerID:  "runner_1",
			Stream:    "stdout",
			Level:     "info",
			Content:   fmt.Sprintf("line %d", i),
			Sequence:  int64(i),
			Metadata:  json.RawMessage(`{}`),
			CreatedAt: archiveBase.Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

func newArchiver(t *testing.T, s jobs.LogArchiveStore, blobs *memoryBlobs, cfg jobs.LogArchiverConfig) *jobs.LogArchiver {
	t.Helper()
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	return jobs.NewLogArchiver(s, logarchive.New(blobs, logarchive.WithFrameRecords(4)), cfg)
}

func readArchive(t *testing.T, blobs *memoryBlobs, a *store.LogArchive) []*store.Log {
	t.Helper()
	ctx := context.Background()

	r, err := logarchive.New(blobs).Open(ctx, a)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	var out []*store.Log
	for {
		rec, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, rec)
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestArchiverMovesLogsOutOfPostgres(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 10)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{LogBatchSize: 3})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SessionsArchived)
	assert.Equal(t, int64(10), result.LogsArchived)
	assert.Equal(t, int64(10), result.LogsDeleted)

	archive := fake.archiveFor("sess_a")
	require.NotNil(t, archive)
	assert.Equal(t, int64(10), archive.LogCount)
	assert.Equal(t, logarchive.Format, archive.Format)
	assert.False(t, archive.Encrypted)
	require.NotNil(t, archive.LastLogID)
	assert.Equal(t, "sess_a_log_0009", *archive.LastLogID)
	require.NotNil(t, archive.FirstLogAt)
	assert.Equal(t, archiveBase, *archive.FirstLogAt)

	// The hot rows are gone and the archive holds them all, in order.
	assert.Zero(t, fake.logCount("sess_a"))
	records := readArchive(t, blobs, archive)
	require.Len(t, records, 10)
	for i, rec := range records {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
	}
}

// The lag window is what keeps a row a still-open transaction is about to
// commit from being deleted unarchived.
func TestArchiverLeavesRecentLogsAlone(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 3)
	fake.logs[2].CreatedAt = time.Now() // inside the lag window

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{WriteLag: time.Minute})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.LogsArchived)
	assert.Equal(t, 1, fake.logCount("sess_a"))
}

func TestArchiverSkipsSessionsWithNothingNew(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Zero(t, result.SessionsArchived)
	assert.Empty(t, blobs.keys(), "an empty archive object is a retrieval error waiting to happen")
	assert.Nil(t, fake.archiveFor("sess_a"))
}

// A crash between writing the object and committing the row must lose nothing:
// the rows are still in PostgreSQL, and the next pass writes the same object
// again and commits it.
func TestArchiverConvergesAfterCrashBeforeCommit(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 8)
	fake.failCommit = true

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{LogBatchSize: 3})

	_, err := archiver.RunNow(ctx)
	require.Error(t, err)
	assert.Nil(t, fake.archiveFor("sess_a"))
	assert.Equal(t, 8, fake.logCount("sess_a"), "nothing may be deleted before the row is committed")

	fake.failCommit = false
	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(8), result.LogsArchived)
	assert.Zero(t, fake.logCount("sess_a"))

	archive := fake.archiveFor("sess_a")
	require.NotNil(t, archive)
	records := readArchive(t, blobs, archive)
	require.Len(t, records, 8, "the retry must not double the records")

	// The retry reused the key the abandoned attempt wrote, so no orphan is
	// left behind.
	assert.Equal(t, []string{logarchive.Key("", "sess_a", 0)}, blobs.keys())
}

// A crash between committing the row and deleting the rows leaves duplicates on
// disk. The next pass must finish the delete, not archive them again.
func TestArchiverFinishesAnAbandonedDelete(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 6)
	fake.failDelete = true

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{})

	_, err := archiver.RunNow(ctx)
	require.Error(t, err)
	require.NotNil(t, fake.archiveFor("sess_a"))
	assert.Equal(t, 6, fake.logCount("sess_a"))

	fake.failDelete = false
	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Zero(t, result.LogsArchived, "the rows are already in the object")
	assert.Equal(t, int64(6), result.LogsDeleted)
	assert.Zero(t, fake.logCount("sess_a"))

	records := readArchive(t, blobs, fake.archiveFor("sess_a"))
	assert.Len(t, records, 6)
	assert.Len(t, blobs.keys(), 1)
}

// A session archived while idle can produce more logs. The second pass extends
// the archive rather than starting a new one or losing the old one.
func TestArchiverAppendsToAnExistingArchive(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "suspended"}}
	fake.logs = seedLogs("sess_a", 0, 5)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{IdleAfter: time.Hour, LogBatchSize: 2})

	_, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	first := fake.archiveFor("sess_a")
	require.NotNil(t, first)
	assert.Equal(t, int64(5), first.LogCount)

	fake.logs = append(fake.logs, seedLogs("sess_a", 5, 4)...)

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), result.LogsArchived)

	second := fake.archiveFor("sess_a")
	require.NotNil(t, second)
	assert.Equal(t, int64(9), second.LogCount)
	assert.NotEqual(t, first.StorageKey, second.StorageKey)
	assert.Equal(t, first.FirstLogAt, second.FirstLogAt, "the start of the archive does not move")

	records := readArchive(t, blobs, second)
	require.Len(t, records, 9)
	for i, rec := range records {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
	}

	// The superseded object is gone once the row points at its replacement.
	assert.Equal(t, []string{second.StorageKey}, blobs.keys())
}

func TestArchiverFailsClosedWhenTheObjectStoreIsDown(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 4)

	blobs := newMemoryBlobs()
	blobs.failUpload = true
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{})

	_, err := archiver.RunNow(ctx)
	require.Error(t, err)
	assert.Equal(t, 4, fake.logCount("sess_a"), "no object, no delete")
	assert.Nil(t, fake.archiveFor("sess_a"))
}

// One session's failure must not stall archiving - and therefore retention -
// for every other session in the deployment.
func TestArchiverContinuesPastOneBadSession(t *testing.T) {
	ctx := context.Background()
	fake := &failingSessionStore{fakeArchiveStore: newFakeArchiveStore(), bad: "sess_bad"}
	fake.candidates = []store.LogArchiveCandidate{
		{SessionID: "sess_bad", Status: "terminated"},
		{SessionID: "sess_good", Status: "terminated"},
	}
	fake.logs = append(seedLogs("sess_bad", 0, 3), seedLogs("sess_good", 0, 3)...)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{})

	result, err := archiver.RunNow(ctx)
	require.Error(t, err)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, 1, result.SessionsArchived)
	assert.Zero(t, fake.logCount("sess_good"))
	assert.Equal(t, 3, fake.logCount("sess_bad"))
}

type failingSessionStore struct {
	*fakeArchiveStore
	bad string
}

func (f *failingSessionStore) ListSessionLogsAfter(
	ctx context.Context, sessionID string, after *store.LogCursor, before time.Time, limit int,
) ([]*store.Log, error) {
	if sessionID == f.bad {
		return nil, errors.New("read failed")
	}
	return f.fakeArchiveStore.ListSessionLogsAfter(ctx, sessionID, after, before, limit)
}

func TestArchiverExpiresAndPurgesArchives(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 3)

	blobs := newMemoryBlobs()
	// A retention of one nanosecond means the archive this pass writes is
	// already expired by the time the sweep looks at it.
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{Retention: time.Nanosecond})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SessionsArchived)
	// The row is soft-deleted first and its object removed after, so one pass
	// expires it and the same pass purges it.
	assert.Equal(t, 1, result.ArchivesExpired)
	assert.Equal(t, 1, result.ArchivesPurged)
	assert.Empty(t, blobs.keys())
	assert.Nil(t, fake.archiveFor("sess_a"))
}

// If the object cannot be deleted, the row stays as a tombstone and the next
// sweep tries again. What must never happen is a live row pointing at an object
// that is gone.
func TestArchiverKeepsTombstoneWhenBlobDeleteFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 3)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{Retention: time.Nanosecond})

	blobs.failDelete = true
	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ArchivesExpired)
	assert.Zero(t, result.ArchivesPurged)

	archive := fake.archiveFor("sess_a")
	require.NotNil(t, archive)
	assert.NotNil(t, archive.DeletedAt)
	assert.Len(t, blobs.keys(), 1)

	blobs.failDelete = false
	result, err = archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ArchivesPurged)
	assert.Empty(t, blobs.keys())
}

// A session whose archive was retired starts a new one rather than appending to
// a tombstone whose object is gone.
func TestArchiverStartsFreshAfterAnArchiveIsRetired(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "suspended"}}
	fake.logs = seedLogs("sess_a", 0, 3)

	blobs := newMemoryBlobs()
	blobs.failDelete = true
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{
		IdleAfter: time.Hour,
		Retention: time.Nanosecond,
	})

	_, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	require.NotNil(t, fake.archiveFor("sess_a").DeletedAt)

	fake.logs = append(fake.logs, seedLogs("sess_a", 3, 2)...)
	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), result.LogsArchived, "a retired archive is not extended")
}

func TestArchiverStartStop(t *testing.T) {
	ctx := context.Background()
	fake := newFakeArchiveStore()
	archiver := newArchiver(t, fake, newMemoryBlobs(), jobs.LogArchiverConfig{Interval: time.Hour})

	require.NoError(t, archiver.Start(ctx))
	assert.True(t, archiver.IsRunning())
	require.Error(t, archiver.Start(ctx), "a second Start must not fork a second loop")

	require.Eventually(t, func() bool { return !archiver.LastRun().IsZero() }, time.Second, 5*time.Millisecond)
	require.NotNil(t, archiver.LastResult())

	require.NoError(t, archiver.Stop(ctx))
	assert.False(t, archiver.IsRunning())
	require.NoError(t, archiver.Stop(ctx), "stopping twice is a no-op")
}

func TestArchiverConfigDefaults(t *testing.T) {
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 2)

	blobs := newMemoryBlobs()
	// Negative values are clamped rather than trusted: a negative retention
	// would otherwise mint archives that are already expired.
	archiver := newArchiver(t, fake, blobs, jobs.LogArchiverConfig{
		Retention: -time.Hour,
		IdleAfter: -time.Hour,
	})

	result, err := archiver.RunNow(context.Background())
	require.NoError(t, err)
	assert.Zero(t, result.ArchivesExpired)
	assert.Nil(t, fake.archiveFor("sess_a").ExpiresAt)
}

// errorStore fails one named method so the pass's error handling can be
// exercised without contorting the happy-path double.
type errorStore struct {
	*fakeArchiveStore
	failCandidates   bool
	failExpiredList  bool
	failDeletedList  bool
	failGetArchive   bool
	failCount        bool
	failExpireUpdate bool
	failRowDelete    bool
}

func (e *errorStore) ListLogArchiveCandidates(
	ctx context.Context, opts store.ListLogArchiveCandidatesOptions,
) ([]store.LogArchiveCandidate, error) {
	if e.failCandidates {
		return nil, errors.New("candidate query failed")
	}
	return e.fakeArchiveStore.ListLogArchiveCandidates(ctx, opts)
}

func (e *errorStore) ListExpiredLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error) {
	if e.failExpiredList {
		return nil, errors.New("expired query failed")
	}
	return e.fakeArchiveStore.ListExpiredLogArchives(ctx, limit)
}

func (e *errorStore) ListDeletedLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error) {
	if e.failDeletedList {
		return nil, errors.New("tombstone query failed")
	}
	return e.fakeArchiveStore.ListDeletedLogArchives(ctx, limit)
}

func (e *errorStore) GetLogArchiveBySession(ctx context.Context, sessionID string) (*store.LogArchive, error) {
	if e.failGetArchive {
		return nil, errors.New("archive lookup failed")
	}
	return e.fakeArchiveStore.GetLogArchiveBySession(ctx, sessionID)
}

func (e *errorStore) CountSessionLogsThrough(
	ctx context.Context, sessionID string, through store.LogCursor,
) (int64, error) {
	if e.failCount {
		return 0, errors.New("count failed")
	}
	return e.fakeArchiveStore.CountSessionLogsThrough(ctx, sessionID, through)
}

func (e *errorStore) UpdateLogArchive(ctx context.Context, id string, u store.LogArchiveUpdates) error {
	if e.failExpireUpdate && u.DeletedAt != nil {
		return errors.New("expire failed")
	}
	return e.fakeArchiveStore.UpdateLogArchive(ctx, id, u)
}

func (e *errorStore) DeleteLogArchive(ctx context.Context, id string) error {
	if e.failRowDelete {
		return errors.New("row delete failed")
	}
	return e.fakeArchiveStore.DeleteLogArchive(ctx, id)
}

func TestArchiverReportsACandidateQueryFailure(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore(), failCandidates: true}
	archiver := newArchiver(t, s, newMemoryBlobs(), jobs.LogArchiverConfig{})

	_, err := archiver.RunNow(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing archive candidates")
}

func TestArchiverReportsAnArchiveLookupFailure(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore(), failGetArchive: true}
	s.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	s.logs = seedLogs("sess_a", 0, 2)

	archiver := newArchiver(t, s, newMemoryBlobs(), jobs.LogArchiverConfig{})
	result, err := archiver.RunNow(context.Background())
	require.Error(t, err)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, 2, s.logCount("sess_a"))
}

func TestArchiverReportsACountFailureWhileConverging(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore()}
	s.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	s.logs = seedLogs("sess_a", 0, 2)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, s, blobs, jobs.LogArchiverConfig{})
	_, err := archiver.RunNow(context.Background())
	require.NoError(t, err)

	// Second pass has nothing new, so it takes the convergence path.
	s.failCount = true
	_, err = archiver.RunNow(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "counting archived logs")
}

func TestArchiverReportsSweepQueryFailures(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore(), failExpiredList: true}
	archiver := newArchiver(t, s, newMemoryBlobs(), jobs.LogArchiverConfig{})

	_, err := archiver.RunNow(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing expired log archives")

	s.failExpiredList = false
	s.failDeletedList = true
	_, err = archiver.RunNow(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing deleted log archives")
}

// An archive that cannot be soft-deleted stays live, and its object stays put:
// the sweep never deletes a blob a live row still points at.
func TestArchiverLeavesTheObjectWhenExpiryFails(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore(), failExpireUpdate: true}
	s.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	s.logs = seedLogs("sess_a", 0, 2)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, s, blobs, jobs.LogArchiverConfig{Retention: time.Nanosecond})

	result, err := archiver.RunNow(context.Background())
	require.NoError(t, err)
	assert.Zero(t, result.ArchivesExpired)
	assert.Zero(t, result.ArchivesPurged)
	assert.Len(t, blobs.keys(), 1)
	assert.Nil(t, s.archiveFor("sess_a").DeletedAt)
}

// The blob is gone but the row could not be removed. The tombstone is retried
// next pass; deleting an already-deleted object is a no-op.
func TestArchiverRetriesWhenTheTombstoneRowSurvives(t *testing.T) {
	s := &errorStore{fakeArchiveStore: newFakeArchiveStore(), failRowDelete: true}
	s.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	s.logs = seedLogs("sess_a", 0, 2)

	blobs := newMemoryBlobs()
	archiver := newArchiver(t, s, blobs, jobs.LogArchiverConfig{Retention: time.Nanosecond})

	result, err := archiver.RunNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.ArchivesExpired)
	assert.Zero(t, result.ArchivesPurged)
	assert.Empty(t, blobs.keys())

	s.failRowDelete = false
	result, err = archiver.RunNow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, result.ArchivesPurged)
	assert.Nil(t, s.archiveFor("sess_a"))
}

func TestArchiverStopsOnACancelledContext(t *testing.T) {
	fake := newFakeArchiveStore()
	fake.candidates = []store.LogArchiveCandidate{{SessionID: "sess_a", Status: "terminated"}}
	fake.logs = seedLogs("sess_a", 0, 4)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	archiver := newArchiver(t, fake, newMemoryBlobs(), jobs.LogArchiverConfig{})
	_, err := archiver.RunNow(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 4, fake.logCount("sess_a"))
}

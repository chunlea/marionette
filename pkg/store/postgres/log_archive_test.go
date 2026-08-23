package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/jobs"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// These are the only tests that put the archiver, the object codec, the
// retrieval reader and real SQL together. Everything else in this feature is
// exercised against doubles, and doubles cannot tell you whether the row-tuple
// comparison the delete depends on means what it is supposed to mean, or
// whether a partition drop sees the archives that cover it.

// =============================================================================
// Blob backend
// =============================================================================

type archiveBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    bool
}

// archiveObjectStore is shared by every test in this file.
//
// One database means one set of log_archives rows, and the archiver sweeps all
// of them: a per-test blob store would leave a later test's pass trying to
// extend an earlier test's archive whose object it cannot see. That failure is
// correct behaviour - a row pointing at a missing object is data loss and has
// to be loud - but it is not what these tests are about.
var archiveObjectStore = &archiveBlobs{objects: map[string][]byte{}}

func newArchiveBlobs() *archiveBlobs {
	archiveObjectStore.mu.Lock()
	defer archiveObjectStore.mu.Unlock()
	archiveObjectStore.fail = false
	return archiveObjectStore
}

func (b *archiveBlobs) Name() string { return "memory" }

func (b *archiveBlobs) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail {
		return errors.New("object store unavailable")
	}
	b.objects[key] = buf.Bytes()
	return nil
}

func (b *archiveBlobs) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (b *archiveBlobs) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	return nil
}

func (b *archiveBlobs) Exists(_ context.Context, key string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.objects[key]
	return ok, nil
}

// =============================================================================
// Fixtures
// =============================================================================

// archiveFixture is one session with logs, ready to be archived.
type archiveFixture struct {
	session *store.Session
	task    *store.Task
	run     *store.TaskRun
	runner  *store.Runner
}

func newArchiveFixture(ctx context.Context, t *testing.T, name string) *archiveFixture {
	t.Helper()
	suffix := name + "-" + time.Now().Format("150405.000000")

	workspace := &store.Workspace{
		Name:        "arch-ws-" + suffix,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, workspace))

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	require.NoError(t, testStore.CreateSession(ctx, session))

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "archive me",
		Status:         "completed",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	require.NoError(t, testStore.CreateTask(ctx, task))

	runner := &store.Runner{
		Name:         "arch-runner-" + suffix,
		Hostname:     "localhost",
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	require.NoError(t, testStore.CreateRunner(ctx, runner))

	run := &store.TaskRun{
		TaskID:   task.ID,
		Attempt:  1,
		Status:   "completed",
		RunnerID: &runner.ID,
	}
	require.NoError(t, testStore.CreateTaskRun(ctx, run))

	return &archiveFixture{session: session, task: task, run: run, runner: runner}
}

// writeLogs inserts logs at explicit timestamps, which is the only way to put a
// row in yesterday's partition.
func (f *archiveFixture) writeLogs(ctx context.Context, t *testing.T, from, count int, at time.Time, step time.Duration) {
	t.Helper()

	for i := from; i < from+count; i++ {
		_, err := testStore.Pool().Exec(ctx, `
			INSERT INTO logs (id, session_id, task_id, run_id, runner_id, stream, level,
				content, sequence, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, 'stdout', $6, $7, $8, '{}'::jsonb, $9)`,
			fmt.Sprintf("log_%s_%05d", f.session.ID[len(f.session.ID)-6:], i),
			f.session.ID, f.task.ID, f.run.ID, f.runner.ID,
			logLevelFor(i), fmt.Sprintf("line %d", i), int64(i),
			at.Add(time.Duration(i)*step))
		require.NoError(t, err)
	}
}

func logLevelFor(i int) string {
	if i%3 == 0 {
		return "error"
	}
	return "info"
}

// terminate backdates the session so it clears the archiver's grace window.
func (f *archiveFixture) terminate(ctx context.Context, t *testing.T, ago time.Duration) {
	t.Helper()
	_, err := testStore.Pool().Exec(ctx, `
		UPDATE sessions SET status = 'terminated', updated_at = now() - $2::interval WHERE id = $1`,
		f.session.ID, fmt.Sprintf("%d seconds", int(ago.Seconds())))
	require.NoError(t, err)
}

func (f *archiveFixture) hotLogCount(ctx context.Context, t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, testStore.Pool().QueryRow(ctx,
		"SELECT count(*) FROM logs WHERE session_id = $1", f.session.ID).Scan(&n))
	return n
}

func newTestArchiver(blobs *archiveBlobs, cfg jobs.LogArchiverConfig) *jobs.LogArchiver {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	if cfg.WriteLag == 0 {
		// The fixtures write logs in the past on purpose; a lag window measured
		// from now would be pure noise here.
		cfg.WriteLag = time.Nanosecond
	}
	return jobs.NewLogArchiver(testStore, logarchive.New(blobs, logarchive.WithFrameRecords(7)), cfg)
}

func readArchiveObject(ctx context.Context, t *testing.T, blobs *archiveBlobs, a *store.LogArchive) []*store.Log {
	t.Helper()

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

// The whole loop, against real SQL: archive, delete, read it back.
func TestLogArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "roundtrip")
	f.writeLogs(ctx, t, 0, 25, time.Now().Add(-2*time.Hour), time.Millisecond)
	f.terminate(ctx, t, time.Hour)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{LogBatchSize: 4})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, result.LogsArchived, int64(25))

	archive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(25), archive.LogCount)
	assert.Equal(t, logarchive.Format, archive.Format)
	require.NotNil(t, archive.LastLogID)
	require.NotNil(t, archive.LastLogSequence)
	require.NotNil(t, archive.StorageSizeBytes)
	assert.Positive(t, *archive.StorageSizeBytes)

	assert.Zero(t, f.hotLogCount(ctx, t), "archived rows must be gone from PostgreSQL")

	records := readArchiveObject(ctx, t, blobs, archive)
	require.Len(t, records, 25)
	for i, rec := range records {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
		assert.Equal(t, f.session.ID, rec.SessionID)
		assert.Equal(t, f.task.ID, rec.TaskID)
		assert.Equal(t, int64(i), rec.Sequence)
		assert.Equal(t, logLevelFor(i), rec.Level)
	}

	// And the same bytes come back through the retrieval path a client uses.
	reader := api.NewArchivedLogReader(testStore, logarchive.New(blobs))
	served := readEveryPage(ctx, t, reader, f.session.ID, 6)
	require.Len(t, served, 25)
	for i, rec := range served {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
	}
}

// The crash window between "object written" and "row committed", and the one
// between "row committed" and "rows deleted". Neither may lose a line, and
// neither may serve one twice.
func TestLogArchiveConvergesAfterACrash(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "crash")
	f.writeLogs(ctx, t, 0, 18, time.Now().Add(-2*time.Hour), time.Millisecond)
	f.terminate(ctx, t, time.Hour)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{LogBatchSize: 5})

	// Crash one: the object never lands.
	blobs.fail = true
	_, err := archiver.RunNow(ctx)
	require.Error(t, err)
	assert.Equal(t, 18, f.hotLogCount(ctx, t), "nothing may be deleted without a durable object")
	_, err = testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	// The retry writes the same key over the abandoned object and commits,
	// and a further pass over an already-archived session is a no-op. The
	// other crash window - row committed, rows not deleted - has its own test.
	blobs.fail = false
	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Zero(t, f.hotLogCount(ctx, t))

	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)

	archive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(18), archive.LogCount)
	assert.Len(t, readArchiveObject(ctx, t, blobs, archive), 18, "the retry must not double the records")
}

// The interrupted delete, reconstructed exactly: the archive row is committed
// and the rows are still there. Retrieval must not show them twice, and the
// next pass must finish the delete.
func TestLogArchiveFinishesAnInterruptedDelete(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "interrupted")
	f.writeLogs(ctx, t, 0, 12, time.Now().Add(-2*time.Hour), time.Millisecond)
	f.terminate(ctx, t, time.Hour)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{})

	// Capture the rows so they can be put back after the archive commits,
	// which is what a crash between the two leaves behind. They go back at
	// their original timestamps: CreateLogs stamps NOW(), which would place
	// them after the boundary and make them genuinely new rows rather than
	// the undeleted copies this test is about.
	rows, err := testStore.ListSessionLogsAfter(ctx, f.session.ID, nil, time.Now(), 100)
	require.NoError(t, err)
	require.Len(t, rows, 12)

	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)
	require.Zero(t, f.hotLogCount(ctx, t))

	for _, l := range rows {
		_, err := testStore.Pool().Exec(ctx, `
			INSERT INTO logs (id, session_id, task_id, run_id, runner_id, stream, level,
				content, sequence, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '{}'::jsonb, $10)`,
			l.ID, l.SessionID, l.TaskID, l.RunID, l.RunnerID, l.Stream, l.Level,
			l.Content, l.Sequence, l.CreatedAt)
		require.NoError(t, err)
	}
	require.Equal(t, 12, f.hotLogCount(ctx, t), "the crash state: archived and still present")

	// Retrieval sees each line once, not twice.
	reader := api.NewArchivedLogReader(testStore, logarchive.New(blobs))
	served := readEveryPage(ctx, t, reader, f.session.ID, 5)
	require.Len(t, served, 12)
	for i, rec := range served {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
	}

	// And the next pass converges by finishing the delete rather than
	// archiving the rows a second time.
	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, result.LogsDeleted, int64(12))
	assert.Zero(t, f.hotLogCount(ctx, t))

	archive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(12), archive.LogCount,
		"the rows were already in the object; a second pass must not add them again")
}

// A session archived while idle produces more logs. The archive is extended,
// and retrieval reads across the join.
func TestLogArchiveExtendsAnIdleSession(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "extend")
	base := time.Now().Add(-3 * time.Hour)
	f.writeLogs(ctx, t, 0, 8, base, time.Millisecond)

	_, err := testStore.Pool().Exec(ctx,
		"UPDATE sessions SET last_activity_at = now() - interval '2 hours' WHERE id = $1", f.session.ID)
	require.NoError(t, err)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{IdleAfter: time.Hour})

	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)
	firstArchive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(8), firstArchive.LogCount)

	f.writeLogs(ctx, t, 8, 6, base, time.Millisecond)

	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)

	archive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(14), archive.LogCount)
	assert.NotEqual(t, firstArchive.StorageKey, archive.StorageKey)
	assert.Equal(t, firstArchive.FirstLogAt, archive.FirstLogAt)

	records := readArchiveObject(ctx, t, blobs, archive)
	require.Len(t, records, 14)
	for i, rec := range records {
		assert.Equal(t, fmt.Sprintf("line %d", i), rec.Content)
	}
}

// The boundary is a triple, and this is the case that proves it has to be: a
// row written on the same timestamp as the last archived one, ordered after it.
func TestLogArchiveLeavesRowsTiedWithTheBoundary(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "tie")

	// Every row shares one timestamp, so only sequence and id separate them.
	at := time.Now().Add(-2 * time.Hour)
	f.writeLogs(ctx, t, 0, 5, at, 0)
	f.terminate(ctx, t, time.Hour)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{})

	_, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	require.Zero(t, f.hotLogCount(ctx, t))

	archive, err := testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(5), archive.LogCount)

	// A late row on the same timestamp, ordered after the boundary.
	f.writeLogs(ctx, t, 5, 1, at, 0)
	assert.Equal(t, 1, f.hotLogCount(ctx, t))

	// It is served after the archived ones and never deleted unarchived.
	reader := api.NewArchivedLogReader(testStore, logarchive.New(blobs))
	served := readEveryPage(ctx, t, reader, f.session.ID, 3)
	require.Len(t, served, 6)
	assert.Equal(t, "line 5", served[5].Content)

	// And the next pass archives it rather than dropping it.
	_, err = archiver.RunNow(ctx)
	require.NoError(t, err)
	assert.Zero(t, f.hotLogCount(ctx, t))

	archive, err = testStore.GetLogArchiveBySession(ctx, f.session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(6), archive.LogCount, "the tied row was archived, not dropped")
}

// Retention may only take a day the archives cover. This is the check that let
// RetentionDays stop being pinned at zero.
func TestDropArchivedLogPartitionsOnlyTakesCoveredDays(t *testing.T) {
	ctx := context.Background()
	pool := testStore.Pool()

	// Two old days, each with its own session.
	covered := time.Now().UTC().AddDate(0, 0, -120).Truncate(24 * time.Hour)
	uncovered := time.Now().UTC().AddDate(0, 0, -121).Truncate(24 * time.Hour)
	for _, day := range []time.Time{covered, uncovered} {
		_, err := pool.Exec(ctx, "SELECT create_log_partition($1::date)", day)
		require.NoError(t, err)
	}
	coveredName := "logs_" + covered.Format("20060102")
	uncoveredName := "logs_" + uncovered.Format("20060102")
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, fmt.Sprintf("DROP TABLE IF EXISTS %s", coveredName))
		_, _ = pool.Exec(bg, fmt.Sprintf("DROP TABLE IF EXISTS %s", uncoveredName))
	})

	archived := newArchiveFixture(ctx, t, "covered")
	archived.writeLogs(ctx, t, 0, 6, covered.Add(6*time.Hour), time.Second)
	archived.terminate(ctx, t, time.Hour)

	live := newArchiveFixture(ctx, t, "uncovered")
	live.writeLogs(ctx, t, 0, 4, uncovered.Add(6*time.Hour), time.Second)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{})
	_, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	require.Zero(t, archived.hotLogCount(ctx, t))
	require.Equal(t, 4, live.hotLogCount(ctx, t), "a live session must not be archived")

	result, err := testStore.DropArchivedLogPartitions(ctx, 30)
	require.NoError(t, err)

	assert.Contains(t, result.Dropped, coveredName, "an archived day must be droppable")
	assert.Contains(t, result.Retained, uncoveredName,
		"a day holding logs no archive covers must be kept, not dropped")
	assert.False(t, partitionExists(ctx, t, coveredName))
	assert.True(t, partitionExists(ctx, t, uncoveredName))
	assert.Equal(t, 4, live.hotLogCount(ctx, t), "retention must not have deleted the live rows")
}

// The expiry sweep: the row is soft-deleted first, then the object, then the
// row goes. The reverse order leaves a live row pointing at nothing.
func TestLogArchiveExpirySweep(t *testing.T) {
	ctx := context.Background()
	f := newArchiveFixture(ctx, t, "expiry")
	f.writeLogs(ctx, t, 0, 5, time.Now().Add(-2*time.Hour), time.Millisecond)
	f.terminate(ctx, t, time.Hour)

	blobs := newArchiveBlobs()
	archiver := newTestArchiver(blobs, jobs.LogArchiverConfig{Retention: time.Nanosecond})

	result, err := archiver.RunNow(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.SessionsArchived)
	assert.GreaterOrEqual(t, result.ArchivesExpired, 1)
	assert.GreaterOrEqual(t, result.ArchivesPurged, 1)

	_, err = testStore.GetLogArchiveBySession(ctx, f.session.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// The candidate query is where "ready to archive" is decided, and getting it
// wrong archives a running session's logs out from under it.
func TestListLogArchiveCandidates(t *testing.T) {
	ctx := context.Background()

	fresh := newArchiveFixture(ctx, t, "fresh")
	fresh.writeLogs(ctx, t, 0, 2, time.Now().Add(-time.Hour), time.Millisecond)
	fresh.terminate(ctx, t, time.Minute) // inside the grace window

	old := newArchiveFixture(ctx, t, "old")
	old.writeLogs(ctx, t, 0, 2, time.Now().Add(-time.Hour), time.Millisecond)
	old.terminate(ctx, t, 2*time.Hour)

	empty := newArchiveFixture(ctx, t, "empty")
	empty.terminate(ctx, t, 2*time.Hour)

	candidates, err := testStore.ListLogArchiveCandidates(ctx, store.ListLogArchiveCandidatesOptions{
		TerminatedAfter: time.Hour,
		Limit:           100,
	})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, c := range candidates {
		ids[c.SessionID] = true
	}
	assert.True(t, ids[old.session.ID], "a session terminated past the grace window is a candidate")
	assert.False(t, ids[fresh.session.ID], "the grace window keeps a just-terminated session out")
	assert.False(t, ids[empty.session.ID], "a session with no logs must not get an empty archive")
}

// readEveryPage walks the retrieval reader to the end, failing on a repeat.
func readEveryPage(
	ctx context.Context,
	t *testing.T,
	reader *api.ArchivedLogReader,
	sessionID string,
	limit int,
) []*store.Log {
	t.Helper()

	var out []*store.Log
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 200; i++ {
		page, err := reader.ReadSession(ctx, sessionID, api.GetLogsOptions{Limit: limit, Cursor: cursor})
		require.NoError(t, err)
		for _, l := range page.Items {
			require.False(t, seen[l.ID], "log %s was served twice", l.ID)
			seen[l.ID] = true
			out = append(out, l)
		}
		if !page.HasMore || page.NextCursor == "" {
			return out
		}
		cursor = page.NextCursor
	}
	t.Fatal("pagination did not terminate")
	return nil
}

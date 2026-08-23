package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// Doubles
// =============================================================================

type archiveTestBlobs struct{ objects map[string][]byte }

func newArchiveTestBlobs() *archiveTestBlobs {
	return &archiveTestBlobs{objects: map[string][]byte{}}
}

func (b *archiveTestBlobs) Name() string { return "memory" }

func (b *archiveTestBlobs) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	b.objects[key] = buf.Bytes()
	return nil
}

func (b *archiveTestBlobs) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	data, ok := b.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (b *archiveTestBlobs) Delete(_ context.Context, key string) error {
	delete(b.objects, key)
	return nil
}

func (b *archiveTestBlobs) Exists(_ context.Context, key string) (bool, error) {
	_, ok := b.objects[key]
	return ok, nil
}

// logTestStore serves logs and archive rows the way the real store does: keyset
// pagination over (created_at, sequence, id), and one archive row per session.
type logTestStore struct {
	logs     []*store.Log
	archives map[string]*store.LogArchive

	archiveErr error
}

func newLogTestStore() *logTestStore {
	return &logTestStore{archives: map[string]*store.LogArchive{}}
}

func (s *logTestStore) GetLogArchiveBySession(_ context.Context, sessionID string) (*store.LogArchive, error) {
	if s.archiveErr != nil {
		return nil, s.archiveErr
	}
	a, ok := s.archives[sessionID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return a, nil
}

func (s *logTestStore) ListLogs(_ context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	sorted := append([]*store.Log(nil), s.logs...)
	sort.Slice(sorted, func(i, j int) bool { return apiLogLess(sorted[i], sorted[j]) })

	var matched []*store.Log
	for _, l := range sorted {
		if opts.SessionID != nil && l.SessionID != *opts.SessionID {
			continue
		}
		if opts.TaskID != nil && l.TaskID != *opts.TaskID {
			continue
		}
		if len(opts.Level) > 0 && !containsString(opts.Level, l.Level) {
			continue
		}
		if len(opts.Stream) > 0 && !containsString(opts.Stream, l.Stream) {
			continue
		}
		if opts.After != nil && !apiCursorLess(*opts.After, l) {
			continue
		}
		matched = append(matched, l)
	}

	total := int64(len(matched))

	// The cursor is the id of the last row of the previous page, which is all
	// this double needs to be faithful about ordering.
	if opts.Cursor != "" {
		for i, l := range matched {
			if l.ID == opts.Cursor {
				matched = matched[i+1:]
				break
			}
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}

	next := ""
	if hasMore && len(matched) > 0 {
		next = matched[len(matched)-1].ID
	}

	return &store.ListResult[store.Log]{
		Items:      matched,
		TotalCount: total,
		HasMore:    hasMore,
		NextCursor: next,
	}, nil
}

func apiLogLess(a, b *store.Log) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if a.Sequence != b.Sequence {
		return a.Sequence < b.Sequence
	}
	return a.ID < b.ID
}

func apiCursorLess(c store.LogCursor, l *store.Log) bool {
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

var logBase = time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

func apiTestLogs(sessionID, taskID string, from, count int) []*store.Log {
	out := make([]*store.Log, 0, count)
	for i := from; i < from+count; i++ {
		level := "info"
		if i%3 == 0 {
			level = "error"
		}
		out = append(out, &store.Log{
			ID:        fmt.Sprintf("log_%04d", i),
			SessionID: sessionID,
			TaskID:    taskID,
			RunID:     "run_1",
			RunnerID:  "runner_1",
			Stream:    "stdout",
			Level:     level,
			Content:   fmt.Sprintf("line %d", i),
			Sequence:  int64(i),
			Metadata:  json.RawMessage(`{}`),
			CreatedAt: logBase.Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

// seedArchive writes records into an object and returns the row that points at
// it, exactly as the archiver would.
func seedArchive(t *testing.T, blobs *archiveTestBlobs, sessionID string, records []*store.Log) *store.LogArchive {
	t.Helper()
	ctx := context.Background()

	objects := logarchive.New(blobs, logarchive.WithFrameRecords(3))
	key := logarchive.Key("", sessionID, 0)
	w, err := objects.NewWriter(ctx, key, "")
	require.NoError(t, err)
	require.NoError(t, w.Append(ctx, records))
	size, err := w.Close(ctx)
	require.NoError(t, err)

	last := records[len(records)-1]
	first := records[0].CreatedAt
	return &store.LogArchive{
		ID:               "arch_1",
		SessionID:        sessionID,
		StorageKey:       key,
		StorageSizeBytes: &size,
		LogCount:         int64(len(records)),
		FirstLogAt:       &first,
		LastLogAt:        &last.CreatedAt,
		LastLogID:        &last.ID,
		LastLogSequence:  &last.Sequence,
		Format:           logarchive.Format,
	}
}

func readAllPages(t *testing.T, r *ArchivedLogReader, q logQuery) []*store.Log {
	t.Helper()
	ctx := context.Background()

	var out []*store.Log
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		page, err := r.Read(ctx, q)
		require.NoError(t, err)
		for _, l := range page.Items {
			require.False(t, seen[l.ID], "log %s was served twice", l.ID)
			seen[l.ID] = true
			out = append(out, l)
		}
		if !page.HasMore || page.NextCursor == "" {
			return out
		}
		q.Cursor = page.NextCursor
	}
	t.Fatal("pagination did not terminate")
	return nil
}

// =============================================================================
// Tests
// =============================================================================

func TestReaderServesHotLogsWhenThereIsNoArchive(t *testing.T) {
	s := newLogTestStore()
	s.logs = apiTestLogs("sess_a", "task_1", 0, 5)

	r := NewArchivedLogReader(s, logarchive.New(newArchiveTestBlobs()))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 2})

	require.Len(t, got, 5)
	assert.Equal(t, "line 0", got[0].Content)
}

func TestReaderServesArchiveWhenTheHotRowsAreGone(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	archived := apiTestLogs("sess_a", "task_1", 0, 7)
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", archived)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 3})

	require.Len(t, got, 7)
	for i, l := range got {
		assert.Equal(t, fmt.Sprintf("line %d", i), l.Content)
	}
}

// The seam is where a merged view goes wrong: the archive's last line and the
// first hot line must both appear, exactly once, in order.
func TestReaderMergesArchiveAndHotLogs(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 6))
	s.logs = apiTestLogs("sess_a", "task_1", 6, 5)

	r := NewArchivedLogReader(s, logarchive.New(blobs))

	for _, limit := range []int{1, 2, 3, 4, 6, 11, 50} {
		got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: limit})
		require.Len(t, got, 11, "limit %d", limit)
		for i, l := range got {
			assert.Equal(t, fmt.Sprintf("line %d", i), l.Content, "limit %d, position %d", limit, i)
		}
	}
}

// A crash between committing the archive row and deleting the rows leaves the
// archived rows in PostgreSQL. They must not be served a second time.
func TestReaderDoesNotServeUndeletedArchivedRowsTwice(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	archived := apiTestLogs("sess_a", "task_1", 0, 6)
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", archived)

	// Everything the archive holds is still in the table, plus newer rows.
	s.logs = append(append([]*store.Log(nil), archived...), apiTestLogs("sess_a", "task_1", 6, 3)...)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 4})

	require.Len(t, got, 9)
	for i, l := range got {
		assert.Equal(t, fmt.Sprintf("line %d", i), l.Content)
	}
}

// A row sharing the last archived row's timestamp but ordered after it is a hot
// row. A boundary that compared timestamps alone would drop it.
func TestReaderKeepsRowsTiedWithTheBoundaryTimestamp(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	archived := apiTestLogs("sess_a", "task_1", 0, 3)
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", archived)

	tied := apiTestLogs("sess_a", "task_1", 3, 1)[0]
	tied.CreatedAt = archived[len(archived)-1].CreatedAt // same microsecond
	s.logs = []*store.Log{tied}

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 10})

	require.Len(t, got, 4)
	assert.Equal(t, "line 3", got[3].Content)
}

func TestReaderArchivedOnlyAndArchivedNever(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 4))
	s.logs = apiTestLogs("sess_a", "task_1", 4, 3)

	r := NewArchivedLogReader(s, logarchive.New(blobs))

	only := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 2, Archived: ArchivedOnly})
	require.Len(t, only, 4)
	assert.Equal(t, "line 3", only[3].Content)

	never := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 2, Archived: ArchivedNever})
	require.Len(t, never, 3)
	assert.Equal(t, "line 4", never[0].Content)
}

func TestReaderArchivedOnlyWithNoArchiveIsEmpty(t *testing.T) {
	s := newLogTestStore()
	s.logs = apiTestLogs("sess_a", "task_1", 0, 3)

	r := NewArchivedLogReader(s, logarchive.New(newArchiveTestBlobs()))
	page, err := r.Read(context.Background(), logQuery{SessionID: "sess_a", Archived: ArchivedOnly})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.False(t, page.HasMore)
}

// The same filters have to hold on both sides of the seam, or a level filter
// would quietly stop working the moment a session was archived.
func TestReaderFiltersBothHalves(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 6))
	s.logs = apiTestLogs("sess_a", "task_1", 6, 6)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 3, Level: []string{"error"}})

	require.NotEmpty(t, got)
	for _, l := range got {
		assert.Equal(t, "error", l.Level)
	}
	// Every third line is an error, on both sides of the boundary.
	assert.Len(t, got, 4)
}

func TestReaderFiltersArchiveByTask(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()

	mixed := append(apiTestLogs("sess_a", "task_1", 0, 4), apiTestLogs("sess_a", "task_2", 4, 4)...)
	sort.Slice(mixed, func(i, j int) bool { return apiLogLess(mixed[i], mixed[j]) })
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", mixed)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", TaskID: "task_2", Limit: 2})

	require.Len(t, got, 4)
	for _, l := range got {
		assert.Equal(t, "task_2", l.TaskID)
	}
}

// total_count has to include the archived lines, or a client paging until it
// has seen total_count records stops before the archive is exhausted.
func TestReaderCountsArchivedRecords(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 5))
	s.logs = apiTestLogs("sess_a", "task_1", 5, 2)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	page, err := r.Read(context.Background(), logQuery{SessionID: "sess_a", Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, int64(7), page.TotalCount)
}

// A soft-deleted archive is a retired one; its object is gone or going, so the
// reader must not try to open it.
func TestReaderIgnoresRetiredArchives(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	archive := seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 3))
	deletedAt := time.Now()
	archive.DeletedAt = &deletedAt
	s.archives["sess_a"] = archive
	s.logs = apiTestLogs("sess_a", "task_1", 3, 2)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 10})

	require.Len(t, got, 2)
	assert.Equal(t, "line 3", got[0].Content)
}

// An archive lookup that fails must degrade to the hot rows, not fail the read:
// the logs still in the table are still logs.
func TestReaderFallsBackWhenTheArchiveLookupFails(t *testing.T) {
	s := newLogTestStore()
	s.archiveErr = errors.New("database unavailable")
	s.logs = apiTestLogs("sess_a", "task_1", 0, 3)

	r := NewArchivedLogReader(s, logarchive.New(newArchiveTestBlobs()))
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 10})
	assert.Len(t, got, 3)
}

func TestReaderWithoutObjectStoreServesHotOnly(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 3))
	s.logs = apiTestLogs("sess_a", "task_1", 3, 2)

	r := NewArchivedLogReader(s, nil)
	got := readAllPages(t, r, logQuery{SessionID: "sess_a", Limit: 10})
	assert.Len(t, got, 2)
}

// A cursor into an object that has since been replaced by a shorter one must
// land on the hot rows rather than error: the archive is rewritten whenever a
// session is extended.
func TestReaderSurvivesACursorPastTheEndOfTheObject(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 3))
	s.logs = apiTestLogs("sess_a", "task_1", 3, 2)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	page, err := r.Read(context.Background(), logQuery{
		SessionID: "sess_a", Limit: 10, Cursor: archiveCursorPrefix + "999",
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.Equal(t, "line 3", page.Items[0].Content)
}

func TestReaderRejectsAMalformedArchiveCursor(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	s.archives["sess_a"] = seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 3))

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	_, err := r.Read(context.Background(), logQuery{SessionID: "sess_a", Cursor: archiveCursorPrefix + "nope"})
	require.Error(t, err)
}

func TestReaderReportsAnUnreadableObject(t *testing.T) {
	blobs := newArchiveTestBlobs()
	s := newLogTestStore()
	archive := seedArchive(t, blobs, "sess_a", apiTestLogs("sess_a", "task_1", 0, 3))
	s.archives["sess_a"] = archive
	delete(blobs.objects, archive.StorageKey)

	r := NewArchivedLogReader(s, logarchive.New(blobs))
	_, err := r.Read(context.Background(), logQuery{SessionID: "sess_a"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening log archive")
}

func TestParseArchivedFilter(t *testing.T) {
	for raw, want := range map[string]ArchivedFilter{
		"":      ArchivedAuto,
		"true":  ArchivedOnly,
		"TRUE":  ArchivedOnly,
		"only":  ArchivedOnly,
		"false": ArchivedNever,
	} {
		got, err := ParseArchivedFilter(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}

	_, err := ParseArchivedFilter("1")
	require.Error(t, err, "a value that does nothing must not pass silently")
}

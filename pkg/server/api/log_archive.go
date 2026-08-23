package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// ArchivedFilter selects which copy of a session's logs a request wants.
type ArchivedFilter string

const (
	// ArchivedAuto is the default: the archive first, then the rows still in
	// PostgreSQL. A caller that does not care where a log line is stored gets
	// one ordered stream and never has to find out.
	ArchivedAuto ArchivedFilter = ""

	// ArchivedOnly serves only what the archive holds.
	ArchivedOnly ArchivedFilter = "true"

	// ArchivedNever serves only the rows still in PostgreSQL. It is what the
	// endpoint did before archiving existed.
	ArchivedNever ArchivedFilter = "false"
)

// ParseArchivedFilter reads the `archived` query parameter.
//
// An unrecognised value is an error rather than a silent default: "archived=1"
// quietly serving the merged view would hide from the caller that its filter
// did nothing.
func ParseArchivedFilter(raw string) (ArchivedFilter, error) {
	switch strings.ToLower(raw) {
	case "":
		return ArchivedAuto, nil
	case "true", "only":
		return ArchivedOnly, nil
	case "false":
		return ArchivedNever, nil
	default:
		return "", fmt.Errorf("archived must be true or false, got %q", raw)
	}
}

// archiveCursorPrefix marks a cursor that points into an archive object rather
// than into the logs table.
//
// The two halves of a merged read paginate differently - the archive by record
// offset, PostgreSQL by its own keyset cursor - and the prefix is what lets one
// opaque cursor carry both without the caller knowing there are two.
const archiveCursorPrefix = "archive:"

// maxArchiveScanPerPage bounds how much of an object one page decompresses.
//
// Without it, a level filter that matches nothing would walk a whole archive
// before answering. The page comes back short instead, with a cursor to
// continue - which is what has_more is for.
const maxArchiveScanPerPage = 10000

// logReadStore is the part of the store the reader uses.
type logReadStore interface {
	ListLogs(ctx context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error)
	GetLogArchiveBySession(ctx context.Context, sessionID string) (*store.LogArchive, error)
}

// ArchivedLogReader serves a session's logs from wherever they currently live.
//
// Archiving moves log rows out of PostgreSQL and into an object, and the point
// of this type is that no caller has to care. A session's logs are one ordered
// stream: everything in the archive, then everything written after the boundary
// the archive stops at. The boundary is the full (created_at, sequence, id) of
// the last archived row, so the seam neither repeats a line nor drops one -
// which also means a crash that left archived rows in PostgreSQL shows up as
// duplicated rows on disk, never as duplicated lines on the wire.
type ArchivedLogReader struct {
	store   logReadStore
	objects *logarchive.Store
}

// NewArchivedLogReader returns a reader over the store and the archive objects.
// A nil objects store leaves the reader serving hot rows only, which is what a
// deployment with archiving switched off wants.
func NewArchivedLogReader(s logReadStore, objects *logarchive.Store) *ArchivedLogReader {
	return &ArchivedLogReader{store: s, objects: objects}
}

// logQuery is one page request against a session's logs.
type logQuery struct {
	SessionID string
	TaskID    string
	Limit     int
	Cursor    string
	Level     []string
	Stream    []string
	Archived  ArchivedFilter
}

// ReadSession returns one page of a session's logs, archive and hot rows alike.
//
// It is the exported entry point; the task path narrows the same read to one
// task, which it can only do once it knows which session the task belongs to.
func (r *ArchivedLogReader) ReadSession(
	ctx context.Context,
	sessionID string,
	opts GetLogsOptions,
) (*store.ListResult[store.Log], error) {
	return r.Read(ctx, logQuery{
		SessionID: sessionID,
		Limit:     opts.Limit,
		Cursor:    opts.Cursor,
		Level:     opts.Level,
		Stream:    opts.Stream,
		Archived:  opts.Archived,
	})
}

// Read returns one page of a session's logs.
func (r *ArchivedLogReader) Read(ctx context.Context, q logQuery) (*store.ListResult[store.Log], error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}

	archive := r.archiveFor(ctx, q)
	if archive == nil {
		if q.Archived == ArchivedOnly {
			// Asked for the archive, and there is none. An empty page is the
			// honest answer; a 404 would conflate "no archive" with "no session".
			return &store.ListResult[store.Log]{Items: nil}, nil
		}
		return r.readHot(ctx, q, nil, 0)
	}

	// A cursor into PostgreSQL means the archive half is already behind us.
	if q.Cursor != "" && !strings.HasPrefix(q.Cursor, archiveCursorPrefix) {
		return r.readHot(ctx, q, archive.Boundary(), archive.LogCount)
	}

	offset, err := parseArchiveCursor(q.Cursor)
	if err != nil {
		return nil, err
	}

	items, nextOffset, exhausted, err := r.readArchive(ctx, archive, q, offset)
	if err != nil {
		return nil, err
	}

	if !exhausted {
		return &store.ListResult[store.Log]{
			Items:      items,
			TotalCount: archive.LogCount,
			HasMore:    true,
			NextCursor: archiveCursorPrefix + strconv.Itoa(nextOffset),
		}, nil
	}

	if q.Archived == ArchivedOnly {
		return &store.ListResult[store.Log]{Items: items, TotalCount: archive.LogCount}, nil
	}

	// The archive is spent; the rest of the page comes from the rows written
	// after it.
	rest := q
	rest.Cursor = ""
	rest.Limit = q.Limit - len(items)
	if rest.Limit <= 0 {
		// The archive filled the page exactly. The next cursor still points at
		// its end so the following page picks up the hot rows.
		return &store.ListResult[store.Log]{
			Items:      items,
			TotalCount: archive.LogCount,
			HasMore:    true,
			NextCursor: archiveCursorPrefix + strconv.Itoa(nextOffset),
		}, nil
	}

	hot, err := r.readHot(ctx, rest, archive.Boundary(), archive.LogCount)
	if err != nil {
		return nil, err
	}
	hot.Items = append(items, hot.Items...)
	return hot, nil
}

// archiveFor resolves the session's archive, or nil when there is nothing to
// read from one.
//
// A lookup failure is not fatal: the hot rows are still there, and an archive
// the reader cannot see is a degraded read rather than a broken endpoint.
func (r *ArchivedLogReader) archiveFor(ctx context.Context, q logQuery) *store.LogArchive {
	if r.objects == nil || q.Archived == ArchivedNever || q.SessionID == "" {
		return nil
	}

	archive, err := r.store.GetLogArchiveBySession(ctx, q.SessionID)
	if err != nil || archive == nil || archive.DeletedAt != nil {
		return nil
	}
	return archive
}

// readHot serves the rows still in PostgreSQL, skipping anything the archive
// already holds.
func (r *ArchivedLogReader) readHot(
	ctx context.Context,
	q logQuery,
	after *store.LogCursor,
	archivedCount int64,
) (*store.ListResult[store.Log], error) {
	opts := store.ListLogsOptions{
		BaseListOptions: store.BaseListOptions{Limit: q.Limit, Cursor: q.Cursor},
		Level:           q.Level,
		Stream:          q.Stream,
		After:           after,
	}
	if q.SessionID != "" {
		opts.SessionID = &q.SessionID
	}
	if q.TaskID != "" {
		opts.TaskID = &q.TaskID
	}

	result, err := r.store.ListLogs(ctx, opts)
	if err != nil {
		return nil, err
	}
	// total_count is what the caller sees as "how many lines does this have",
	// and the archived ones are lines. The archived half ignores the level and
	// stream filters, because counting them would mean decompressing the whole
	// object to answer a number.
	result.TotalCount += archivedCount
	return result, nil
}

// readArchive walks the object from a record offset, collecting matches.
//
// The offset counts records scanned rather than records returned, so a cursor
// resumes at the same place whatever filter the next page asks for.
func (r *ArchivedLogReader) readArchive(
	ctx context.Context,
	archive *store.LogArchive,
	q logQuery,
	offset int,
) (items []*store.Log, nextOffset int, exhausted bool, err error) {
	reader, err := r.objects.Open(ctx, archive)
	if err != nil {
		return nil, 0, false, fmt.Errorf("opening log archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	scanned := 0
	for scanned < offset {
		if _, err := reader.Next(ctx); err != nil {
			if errors.Is(err, io.EOF) {
				// The object is shorter than the cursor claims, which means it
				// was replaced under the caller. Report the end rather than an
				// error: the hot rows still complete the stream.
				return nil, scanned, true, nil
			}
			return nil, 0, false, fmt.Errorf("reading log archive: %w", err)
		}
		scanned++
	}

	budget := maxArchiveScanPerPage
	for len(items) < q.Limit && budget > 0 {
		rec, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return items, scanned, true, nil
			}
			return nil, 0, false, fmt.Errorf("reading log archive: %w", err)
		}
		scanned++
		budget--

		if matchesLogFilter(rec, q) {
			items = append(items, rec)
		}
	}

	return items, scanned, false, nil
}

// matchesLogFilter applies the same filters the SQL half applies.
func matchesLogFilter(l *store.Log, q logQuery) bool {
	if q.TaskID != "" && l.TaskID != q.TaskID {
		return false
	}
	if len(q.Level) > 0 && !containsString(q.Level, l.Level) {
		return false
	}
	if len(q.Stream) > 0 && !containsString(q.Stream, l.Stream) {
		return false
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func parseArchiveCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, archiveCursorPrefix))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid cursor")
	}
	return offset, nil
}

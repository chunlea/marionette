package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// LogArchiveStore is the database half of archiving. *postgres.Store implements
// it.
//
// It is a narrow interface rather than store.Store for the same reason
// LogPartitioner is: archiving is background maintenance, its queries are
// PostgreSQL-specific, and a job that named the whole store interface would be
// untestable without one.
type LogArchiveStore interface {
	ListLogArchiveCandidates(ctx context.Context, opts store.ListLogArchiveCandidatesOptions) ([]store.LogArchiveCandidate, error)
	ListSessionLogsAfter(ctx context.Context, sessionID string, after *store.LogCursor, before time.Time, limit int) ([]*store.Log, error)
	DeleteSessionLogsThrough(ctx context.Context, sessionID string, through store.LogCursor, batchSize int) (int64, error)
	CountSessionLogsThrough(ctx context.Context, sessionID string, through store.LogCursor) (int64, error)

	GetLogArchiveBySession(ctx context.Context, sessionID string) (*store.LogArchive, error)
	CreateLogArchive(ctx context.Context, archive *store.LogArchive) error
	UpdateLogArchive(ctx context.Context, archiveID string, updates store.LogArchiveUpdates) error
	DeleteLogArchive(ctx context.Context, archiveID string) error
	ListExpiredLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error)
	ListDeletedLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error)
}

// LogArchiverConfig tunes the archiver. Zero values take the defaults below.
type LogArchiverConfig struct {
	// Interval is how often a pass runs. Default: 1 hour.
	Interval time.Duration

	// TerminatedAfter is how long a session must have been terminated before
	// its logs are archived. It is a grace window, not a retention policy.
	// Default: 15 minutes.
	TerminatedAfter time.Duration

	// IdleAfter also archives sessions that are still alive but have been quiet
	// this long. Zero - the default - leaves live sessions alone, which is the
	// conservative setting: a live session can produce more logs, and each
	// further pass has to extend its archive.
	IdleAfter time.Duration

	// Retention is how long an archive lives before the expiry sweep takes it.
	// Zero keeps archives forever. Default: 90 days.
	Retention time.Duration

	// SessionsPerRun bounds one pass. Default: 50.
	SessionsPerRun int

	// LogBatchSize is how many log rows are read at a time. Default: 1000.
	LogBatchSize int

	// DeleteBatchSize is how many rows one DELETE covers. Default: 5000.
	DeleteBatchSize int

	// WriteLag holds the archiver back from the present. Rows younger than this
	// are left alone, so a log written by a transaction that opened before the
	// scan cannot land underneath the boundary and be deleted unarchived.
	// Default: 5 minutes.
	WriteLag time.Duration

	// Logger is the structured logger to use.
	Logger *zap.Logger
}

func (c *LogArchiverConfig) applyDefaults() {
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.TerminatedAfter <= 0 {
		c.TerminatedAfter = 15 * time.Minute
	}
	if c.IdleAfter < 0 {
		c.IdleAfter = 0
	}
	if c.Retention < 0 {
		c.Retention = 0
	}
	if c.SessionsPerRun <= 0 {
		c.SessionsPerRun = 50
	}
	if c.LogBatchSize <= 0 {
		c.LogBatchSize = 1000
	}
	if c.DeleteBatchSize <= 0 {
		c.DeleteBatchSize = 5000
	}
	if c.WriteLag <= 0 {
		c.WriteLag = 5 * time.Minute
	}
	if c.Logger == nil {
		c.Logger = zap.NewNop()
	}
}

// LogArchiveResult is what one pass did.
type LogArchiveResult struct {
	SessionsArchived int
	LogsArchived     int64
	LogsDeleted      int64
	ArchivesExpired  int
	ArchivesPurged   int
	Duration         time.Duration
	Errors           []error
}

// LogArchiver moves finished sessions' logs out of PostgreSQL and into the blob
// store, and expires the archives it made.
//
// It exists to close the loop round 1 deliberately left open. The partition
// maintainer shipped with retention pinned at zero, because dropping a daily
// partition deletes the only copy of the logs in it. This job makes a second
// copy; DropArchivedLogPartitions then checks, per partition, that the copy
// covers what is about to be dropped.
//
// The ordering is archive-then-delete, and it is the whole design:
//
//	write the object -> commit the log_archives row -> delete the rows
//
// A crash anywhere in that sequence leaves rows that are in both places, never
// rows that are in neither. The retrieval path knows the boundary the row
// records and serves hot rows only from beyond it, so duplicates on disk are
// not duplicates on the wire, and the next pass finishes the delete.
type LogArchiver struct {
	store   LogArchiveStore
	objects *logarchive.Store
	cfg     LogArchiverConfig
	logger  *zap.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	lastRun    time.Time
	lastResult *LogArchiveResult
}

// NewLogArchiver creates the archiver.
func NewLogArchiver(s LogArchiveStore, objects *logarchive.Store, cfg LogArchiverConfig) *LogArchiver {
	cfg.applyDefaults()
	return &LogArchiver{
		store:   s,
		objects: objects,
		cfg:     cfg,
		logger:  cfg.Logger,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start begins the periodic archiver. A pass runs immediately: a server that
// boots with a backlog should not wait an interval to start on it.
func (j *LogArchiver) Start(ctx context.Context) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("log archiver already running")
	}
	j.running = true
	j.stopCh = make(chan struct{})
	j.doneCh = make(chan struct{})
	j.mu.Unlock()

	go j.run(ctx)
	j.logger.Info("log archiver started",
		zap.Duration("interval", j.cfg.Interval),
		zap.Duration("terminated_after", j.cfg.TerminatedAfter),
		zap.Duration("idle_after", j.cfg.IdleAfter),
		zap.Duration("retention", j.cfg.Retention),
		zap.Bool("encrypted", j.objects.Encrypts()),
	)
	return nil
}

// Stop stops the archiver gracefully.
func (j *LogArchiver) Stop(ctx context.Context) error {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return nil
	}
	close(j.stopCh)
	doneCh := j.doneCh
	j.mu.Unlock()

	select {
	case <-doneCh:
		j.logger.Info("log archiver stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning reports whether the periodic loop is live.
func (j *LogArchiver) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// LastRun returns when the last pass finished.
func (j *LogArchiver) LastRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRun
}

// LastResult returns what the last pass did.
func (j *LogArchiver) LastResult() *LogArchiveResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastResult
}

func (j *LogArchiver) run(ctx context.Context) {
	defer func() {
		j.mu.Lock()
		j.running = false
		close(j.doneCh)
		j.mu.Unlock()
	}()

	if _, err := j.RunNow(ctx); err != nil {
		j.logger.Error("initial log archive pass failed", zap.Error(err))
	}

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-j.stopCh:
			return
		case <-ticker.C:
			if _, err := j.RunNow(ctx); err != nil {
				j.logger.Error("log archive pass failed", zap.Error(err))
			}
		}
	}
}

// RunNow archives every eligible session and sweeps expired archives.
//
// One session's failure never stops the pass: a session whose object store
// rejects a write would otherwise stall archiving - and therefore retention -
// for the whole deployment.
func (j *LogArchiver) RunNow(ctx context.Context) (*LogArchiveResult, error) {
	start := time.Now()
	result := &LogArchiveResult{}

	candidates, err := j.store.ListLogArchiveCandidates(ctx, store.ListLogArchiveCandidatesOptions{
		TerminatedAfter: j.cfg.TerminatedAfter,
		IdleAfter:       j.cfg.IdleAfter,
		Limit:           j.cfg.SessionsPerRun,
	})
	if err != nil {
		return result, fmt.Errorf("listing archive candidates: %w", err)
	}

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return j.finish(result, start), err
		}

		archived, deleted, err := j.archiveSession(ctx, c)
		if err != nil {
			j.logger.Error("archiving session logs failed",
				zap.String("session_id", c.SessionID),
				zap.Error(err))
			result.Errors = append(result.Errors, fmt.Errorf("session %s: %w", c.SessionID, err))
			continue
		}

		result.LogsArchived += archived
		result.LogsDeleted += deleted
		if archived > 0 {
			result.SessionsArchived++
		}
	}

	if err := j.sweepExpired(ctx, result); err != nil {
		result.Errors = append(result.Errors, err)
	}

	j.finish(result, start)

	j.logger.Info("log archive pass completed",
		zap.Int("candidates", len(candidates)),
		zap.Int("sessions_archived", result.SessionsArchived),
		zap.Int64("logs_archived", result.LogsArchived),
		zap.Int64("logs_deleted", result.LogsDeleted),
		zap.Int("archives_expired", result.ArchivesExpired),
		zap.Int("archives_purged", result.ArchivesPurged),
		zap.Int("errors", len(result.Errors)),
		zap.Duration("duration", result.Duration),
	)

	if len(result.Errors) > 0 {
		return result, fmt.Errorf("log archive pass completed with %d error(s): %w",
			len(result.Errors), errors.Join(result.Errors...))
	}
	return result, nil
}

func (j *LogArchiver) finish(result *LogArchiveResult, start time.Time) *LogArchiveResult {
	result.Duration = time.Since(start)
	j.mu.Lock()
	j.lastRun = time.Now()
	j.lastResult = result
	j.mu.Unlock()
	return result
}

// archiveSession moves one session's logs into its archive.
func (j *LogArchiver) archiveSession(
	ctx context.Context,
	c store.LogArchiveCandidate,
) (archived, deleted int64, err error) {
	existing, err := j.existingArchive(ctx, c.SessionID)
	if err != nil {
		return 0, 0, err
	}
	boundary := archiveBoundary(existing)

	// Rows younger than the lag window are out of scope for this pass. They are
	// the ones a still-open transaction could be about to write underneath.
	cutoff := time.Now().Add(-j.cfg.WriteLag)

	first, err := j.store.ListSessionLogsAfter(ctx, c.SessionID, boundary, cutoff, j.cfg.LogBatchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("reading session logs: %w", err)
	}

	if len(first) == 0 {
		// Nothing new. There may still be rows a previous pass archived and
		// then died before deleting; finishing that is the convergence step.
		deleted, err = j.finishAbandonedDelete(ctx, c.SessionID, boundary)
		return 0, deleted, err
	}

	tenantID := ""
	if c.TenantID != nil {
		tenantID = *c.TenantID
	}

	var alreadyArchived int64
	if existing != nil {
		alreadyArchived = existing.LogCount
	}
	key := logarchive.Key(tenantID, c.SessionID, alreadyArchived)

	written, last, err := j.writeObject(ctx, c, key, tenantID, existing, first, cutoff)
	if err != nil {
		return 0, 0, err
	}

	// The object is durable. Committing the row is what makes the delete safe:
	// until this returns, the rows in PostgreSQL are the only copy anything
	// knows how to find.
	if err := j.commitArchive(ctx, c, existing, key, written.size, written.count, written.firstAt, last); err != nil {
		return 0, 0, err
	}

	deleted, err = j.store.DeleteSessionLogsThrough(ctx, c.SessionID, *last, j.cfg.DeleteBatchSize)
	if err != nil {
		// The archive is committed, so this is a retry, not a loss: the next
		// pass finds the rows still there and deletes them.
		return written.count, deleted, fmt.Errorf("deleting archived logs: %w", err)
	}

	// The superseded object is only unreferenced once the row points at the new
	// one, which it now does. A failure here leaks a blob; it cannot lose a log.
	if existing != nil && existing.StorageKey != key {
		if err := j.objects.Delete(ctx, existing.StorageKey); err != nil {
			j.logger.Warn("could not delete superseded log archive object",
				zap.String("session_id", c.SessionID),
				zap.String("storage_key", existing.StorageKey),
				zap.Error(err))
		}
	}

	j.logger.Debug("archived session logs",
		zap.String("session_id", c.SessionID),
		zap.String("storage_key", key),
		zap.Int64("logs_archived", written.count),
		zap.Int64("logs_deleted", deleted),
	)

	return written.count, deleted, nil
}

// writtenObject is what one object write produced.
type writtenObject struct {
	count   int64
	size    int64
	firstAt *time.Time
}

// writeObject streams the session's new logs into a new object, carrying the
// existing archive's frames across first.
func (j *LogArchiver) writeObject(
	ctx context.Context,
	c store.LogArchiveCandidate,
	key, tenantID string,
	existing *store.LogArchive,
	first []*store.Log,
	cutoff time.Time,
) (writtenObject, *store.LogCursor, error) {
	w, err := j.objects.NewWriter(ctx, key, tenantID)
	if err != nil {
		return writtenObject{}, nil, fmt.Errorf("opening log archive writer: %w", err)
	}

	out, last, err := j.streamInto(ctx, w, c, existing, first, cutoff)
	size, closeErr := w.Close(ctx)
	if err != nil {
		return writtenObject{}, nil, err
	}
	if closeErr != nil {
		return writtenObject{}, nil, fmt.Errorf("writing log archive: %w", closeErr)
	}
	out.size = size
	return out, last, nil
}

func (j *LogArchiver) streamInto(
	ctx context.Context,
	w *logarchive.Writer,
	c store.LogArchiveCandidate,
	existing *store.LogArchive,
	first []*store.Log,
	cutoff time.Time,
) (writtenObject, *store.LogCursor, error) {
	var out writtenObject

	if existing != nil {
		src, err := j.objects.Open(ctx, existing)
		if err != nil {
			return out, nil, fmt.Errorf("opening existing log archive: %w", err)
		}
		copyErr := w.CopyFrames(ctx, src)
		closeErr := src.Close()
		if copyErr != nil {
			return out, nil, fmt.Errorf("copying existing log archive: %w", copyErr)
		}
		if closeErr != nil {
			return out, nil, fmt.Errorf("closing existing log archive: %w", closeErr)
		}
		out.firstAt = existing.FirstLogAt
	}

	batch := first
	var last *store.LogCursor
	for len(batch) > 0 {
		if err := ctx.Err(); err != nil {
			return out, nil, err
		}
		if err := w.Append(ctx, batch); err != nil {
			return out, nil, fmt.Errorf("appending to log archive: %w", err)
		}

		if out.firstAt == nil {
			at := batch[0].CreatedAt
			out.firstAt = &at
		}
		out.count += int64(len(batch))

		tail := batch[len(batch)-1]
		last = &store.LogCursor{CreatedAt: tail.CreatedAt, Sequence: tail.Sequence, ID: tail.ID}

		if len(batch) < j.cfg.LogBatchSize {
			break
		}

		next, err := j.store.ListSessionLogsAfter(ctx, c.SessionID, last, cutoff, j.cfg.LogBatchSize)
		if err != nil {
			return out, nil, fmt.Errorf("reading session logs: %w", err)
		}
		batch = next
	}

	return out, last, nil
}

// commitArchive records the new object, creating the row or moving it forward.
func (j *LogArchiver) commitArchive(
	ctx context.Context,
	c store.LogArchiveCandidate,
	existing *store.LogArchive,
	key string,
	size, count int64,
	firstAt *time.Time,
	last *store.LogCursor,
) error {
	expiresAt := j.expiry()
	format := logarchive.Format
	encrypted := j.objects.Encrypts()

	if existing == nil {
		archive := &store.LogArchive{
			SessionID:        c.SessionID,
			TenantID:         c.TenantID,
			StorageKey:       key,
			StorageSizeBytes: &size,
			LogCount:         count,
			FirstLogAt:       firstAt,
			LastLogAt:        &last.CreatedAt,
			LastLogID:        &last.ID,
			LastLogSequence:  &last.Sequence,
			ExpiresAt:        expiresAt,
			Format:           format,
			Encrypted:        encrypted,
		}
		if err := j.store.CreateLogArchive(ctx, archive); err != nil {
			return fmt.Errorf("creating log archive row: %w", err)
		}
		return nil
	}

	total := existing.LogCount + count
	updates := store.LogArchiveUpdates{
		StorageKey:       &key,
		StorageSizeBytes: &size,
		LogCount:         &total,
		LastLogAt:        &last.CreatedAt,
		LastLogID:        &last.ID,
		LastLogSequence:  &last.Sequence,
		ExpiresAt:        expiresAt,
		Format:           &format,
		Encrypted:        &encrypted,
	}
	if existing.FirstLogAt == nil && firstAt != nil {
		updates.FirstLogAt = firstAt
	}
	if err := j.store.UpdateLogArchive(ctx, existing.ID, updates); err != nil {
		return fmt.Errorf("updating log archive row: %w", err)
	}
	return nil
}

// finishAbandonedDelete removes rows a previous pass archived but did not get
// to delete. It is the reason archive-then-delete is safe to interrupt.
func (j *LogArchiver) finishAbandonedDelete(
	ctx context.Context,
	sessionID string,
	boundary *store.LogCursor,
) (int64, error) {
	if boundary == nil {
		return 0, nil
	}

	remaining, err := j.store.CountSessionLogsThrough(ctx, sessionID, *boundary)
	if err != nil {
		return 0, fmt.Errorf("counting archived logs: %w", err)
	}
	if remaining == 0 {
		return 0, nil
	}

	j.logger.Info("finishing a delete a previous pass left behind",
		zap.String("session_id", sessionID),
		zap.Int64("rows", remaining))

	deleted, err := j.store.DeleteSessionLogsThrough(ctx, sessionID, *boundary, j.cfg.DeleteBatchSize)
	if err != nil {
		return deleted, fmt.Errorf("deleting archived logs: %w", err)
	}
	return deleted, nil
}

// sweepExpired retires archives past their retention.
//
// Two phases, and the order matters: the row is soft-deleted first, then the
// object. The reverse leaves a live row pointing at an object that is gone,
// which reads as data loss rather than as an interrupted delete - the same
// lesson the content-addressed collector learned. An interrupted sweep leaves a
// tombstone whose blob may still exist, and the next pass finishes it.
func (j *LogArchiver) sweepExpired(ctx context.Context, result *LogArchiveResult) error {
	expired, err := j.store.ListExpiredLogArchives(ctx, j.cfg.SessionsPerRun)
	if err != nil {
		return fmt.Errorf("listing expired log archives: %w", err)
	}

	now := time.Now()
	for _, a := range expired {
		if err := j.store.UpdateLogArchive(ctx, a.ID, store.LogArchiveUpdates{DeletedAt: &now}); err != nil {
			j.logger.Error("could not expire log archive",
				zap.String("archive_id", a.ID), zap.Error(err))
			continue
		}
		result.ArchivesExpired++
	}

	tombstones, err := j.store.ListDeletedLogArchives(ctx, j.cfg.SessionsPerRun)
	if err != nil {
		return fmt.Errorf("listing deleted log archives: %w", err)
	}

	for _, a := range tombstones {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := j.objects.Delete(ctx, a.StorageKey); err != nil {
			j.logger.Error("could not delete expired log archive object",
				zap.String("archive_id", a.ID),
				zap.String("storage_key", a.StorageKey),
				zap.Error(err))
			continue
		}
		if err := j.store.DeleteLogArchive(ctx, a.ID); err != nil {
			j.logger.Error("could not delete expired log archive row",
				zap.String("archive_id", a.ID), zap.Error(err))
			continue
		}
		result.ArchivesPurged++
	}

	return nil
}

func (j *LogArchiver) expiry() *time.Time {
	if j.cfg.Retention <= 0 {
		return nil
	}
	at := time.Now().Add(j.cfg.Retention)
	return &at
}

func (j *LogArchiver) existingArchive(ctx context.Context, sessionID string) (*store.LogArchive, error) {
	archive, err := j.store.GetLogArchiveBySession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading log archive: %w", err)
	}
	// A soft-deleted archive is a retired one. Its object is gone or going, so
	// the session starts a fresh archive rather than appending to a tombstone.
	if archive != nil && archive.DeletedAt != nil {
		return nil, nil
	}
	return archive, nil
}

// archiveBoundary is the exact position an archive stopped at.
//
// All three parts or none: a row missing one of them predates the boundary
// columns, and guessing the missing part would either re-archive rows the
// object already holds or delete rows it does not.
func archiveBoundary(a *store.LogArchive) *store.LogCursor {
	if a == nil || a.LastLogAt == nil || a.LastLogID == nil || a.LastLogSequence == nil {
		return nil
	}
	return &store.LogCursor{
		CreatedAt: *a.LastLogAt,
		Sequence:  *a.LastLogSequence,
		ID:        *a.LastLogID,
	}
}

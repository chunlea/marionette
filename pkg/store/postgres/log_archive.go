package postgres

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// DefaultLogArchiveFormat is the encoding CreateLogArchive stamps on rows that
// do not name one. It must match pkg/storage/logarchive's container version.
const DefaultLogArchiveFormat = "ndjson+zstd/frames1"

// defaultArchiveCandidateLimit bounds one archiver pass when the caller does
// not. Archiving is a background sweep; a pass that tried to drain every
// eligible session at once would hold a connection for as long as it took.
const defaultArchiveCandidateLimit = 50

// ListLogArchiveCandidates returns sessions whose logs are ready to archive.
//
// Two populations qualify. Terminated sessions, once they have been terminated
// for TerminatedAfter - a grace window, so the archiver is never reading a
// session something is still writing to. And, when IdleAfter is set, live
// sessions that have been quiet for that long; those may produce more logs
// later, which is why an archive can be appended to.
//
// A session with no logs is skipped rather than given an empty archive: an
// archive row with no object behind it is a retrieval error waiting to happen.
func (s *Store) ListLogArchiveCandidates(
	ctx context.Context,
	opts store.ListLogArchiveCandidatesOptions,
) ([]store.LogArchiveCandidate, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultArchiveCandidateLimit
	}

	// The idle arm is built in rather than passed as a zero interval: with
	// IdleAfter unset the intent is "terminated sessions only", and a zero
	// interval would instead mean "every live session, immediately".
	idleArm := ""
	args := []any{int64(opts.TerminatedAfter.Seconds()), limit}
	if opts.IdleAfter > 0 {
		idleArm = ` OR (s.status <> 'terminated'
		            AND coalesce(s.last_activity_at, s.updated_at)
		                <= now() - ($3::bigint * interval '1 second'))`
		args = []any{int64(opts.TerminatedAfter.Seconds()), limit, int64(opts.IdleAfter.Seconds())}
	}

	query := fmt.Sprintf(`
		SELECT s.id, s.tenant_id, s.status
		FROM sessions s
		WHERE (
			(s.status = 'terminated'
			 AND s.updated_at <= now() - ($1::bigint * interval '1 second'))%s
		)
		AND EXISTS (SELECT 1 FROM logs l WHERE l.session_id = s.id)
		ORDER BY s.updated_at
		LIMIT $2`, idleArm)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing log archive candidates: %w", err)
	}
	defer rows.Close()

	var out []store.LogArchiveCandidate
	for rows.Next() {
		var c store.LogArchiveCandidate
		if err := rows.Scan(&c.SessionID, &c.TenantID, &c.Status); err != nil {
			return nil, fmt.Errorf("scanning log archive candidate: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log archive candidates: %w", err)
	}
	return out, nil
}

// ListSessionLogsAfter returns one page of a session's logs in archive order.
//
// after is the exclusive lower bound - nil for the first page, otherwise the
// last row of the previous page. before is an exclusive upper bound on
// created_at: the archiver sets it a few minutes into the past so a row written
// by a transaction that opened before the scan cannot land underneath it and be
// deleted unarchived.
func (s *Store) ListSessionLogsAfter(
	ctx context.Context,
	sessionID string,
	after *store.LogCursor,
	before time.Time,
	limit int,
) ([]*store.Log, error) {
	if limit <= 0 {
		limit = 1000
	}

	var (
		query string
		args  []any
	)
	if after == nil {
		query = fmt.Sprintf(`
			SELECT %s FROM logs
			WHERE session_id = $1 AND created_at < $2
			ORDER BY created_at, sequence, id
			LIMIT $3`, logColumns)
		args = []any{sessionID, before, limit}
	} else {
		query = fmt.Sprintf(`
			SELECT %s FROM logs
			WHERE session_id = $1 AND created_at < $2
			  AND (created_at, sequence, id) > ($3, $4, $5)
			ORDER BY created_at, sequence, id
			LIMIT $6`, logColumns)
		args = []any{sessionID, before, after.CreatedAt, after.Sequence, after.ID, limit}
	}

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("reading session logs: %w", err)
	}
	defer rows.Close()

	var logs []*store.Log
	for rows.Next() {
		l, err := scanLogFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning log: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating session logs: %w", err)
	}
	return logs, nil
}

// DeleteSessionLogsThrough removes a session's logs up to and including
// through, in batches, and returns how many rows went.
//
// This is the destructive half of archive-then-delete and must only run once
// the object is durable and the log_archives row is committed. The bound is the
// full (created_at, sequence, id) triple of the last archived row, so a row
// sharing the last archived row's timestamp but arriving after it survives to
// be archived by the next pass.
//
// Batching is not an optimisation: one DELETE covering a busy session's whole
// history would hold row locks across every partition it touches for the length
// of the statement.
func (s *Store) DeleteSessionLogsThrough(
	ctx context.Context,
	sessionID string,
	through store.LogCursor,
	batchSize int,
) (int64, error) {
	if batchSize <= 0 {
		batchSize = 5000
	}

	// The partitioned table's primary key is (id, created_at); deleting by that
	// pair lets the planner prune to the partitions the batch actually touches.
	query := `
		DELETE FROM logs
		WHERE (id, created_at) IN (
			SELECT id, created_at FROM logs
			WHERE session_id = $1
			  AND created_at <= $2
			  AND (created_at, sequence, id) <= ($2, $3, $4)
			ORDER BY created_at, sequence, id
			LIMIT $5
		)`

	var total int64
	for {
		tag, err := s.db.Exec(ctx, query, sessionID, through.CreatedAt, through.Sequence, through.ID, batchSize)
		if err != nil {
			return total, fmt.Errorf("deleting archived session logs: %w", err)
		}
		affected := tag.RowsAffected()
		total += affected
		if affected < int64(batchSize) {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

// CountSessionLogsThrough counts a session's logs at or before through.
//
// The archiver uses it to decide whether a crash left rows behind that the
// archive already covers, which is the difference between "nothing to do" and
// "finish the delete this pass abandoned".
func (s *Store) CountSessionLogsThrough(
	ctx context.Context,
	sessionID string,
	through store.LogCursor,
) (int64, error) {
	query := `
		SELECT count(*) FROM logs
		WHERE session_id = $1
		  AND created_at <= $2
		  AND (created_at, sequence, id) <= ($2, $3, $4)`

	var n int64
	err := s.db.QueryRow(ctx, query, sessionID, through.CreatedAt, through.Sequence, through.ID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counting archived session logs: %w", err)
	}
	return n, nil
}

// ListExpiredLogArchives returns live archives whose expires_at has passed.
func (s *Store) ListExpiredLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error) {
	if limit <= 0 {
		limit = defaultArchiveCandidateLimit
	}
	query := fmt.Sprintf(`
		SELECT %s FROM log_archives
		WHERE deleted_at IS NULL AND expires_at IS NOT NULL AND expires_at <= now()
		ORDER BY expires_at
		LIMIT $1`, logArchiveColumns)
	return s.queryLogArchives(ctx, query, limit)
}

// ListDeletedLogArchives returns soft-deleted archives, whose blobs may still
// be there.
//
// The sweep is two-phase - soft-delete the row, then delete the blob - because
// the reverse order leaves a row pointing at an object that is gone, which
// reads as data loss rather than as an interrupted delete. That is the CAS
// lesson: the database is the record of what exists, so it moves first.
func (s *Store) ListDeletedLogArchives(ctx context.Context, limit int) ([]*store.LogArchive, error) {
	if limit <= 0 {
		limit = defaultArchiveCandidateLimit
	}
	query := fmt.Sprintf(`
		SELECT %s FROM log_archives
		WHERE deleted_at IS NOT NULL
		ORDER BY deleted_at
		LIMIT $1`, logArchiveColumns)
	return s.queryLogArchives(ctx, query, limit)
}

func (s *Store) queryLogArchives(ctx context.Context, query string, args ...any) ([]*store.LogArchive, error) {
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying log_archives: %w", err)
	}
	defer rows.Close()

	var out []*store.LogArchive
	for rows.Next() {
		a, err := scanLogArchiveFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning log_archive: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log_archives: %w", err)
	}
	return out, nil
}

// dailyLogPartition matches the names create_log_partition generates. Anything
// else attached to `logs` - the default partition, a hand-made table - is left
// alone: retention drops what it can name a date for, and nothing else.
var dailyLogPartition = regexp.MustCompile(`^logs_(\d{8})$`)

// DropArchivedLogPartitions drops daily partitions older than retentionDays,
// but only those whose remaining rows are all covered by a live archive.
//
// This is the check that makes retention safe to enable. Round 1 added the
// partition maintainer with RetentionDays pinned at zero, because dropping a
// partition deletes the only copy of the logs in it. Archiving gives there to
// be a second copy, and this asks - per partition, not per deployment - whether
// there actually is one.
//
// A row is covered when its session has an archive that is not soft-deleted and
// whose last_log_at is at or after the row's created_at: the archive object
// holds every log of that session up to that boundary. The usual reason a row
// is uncovered is that its session is still running, which is exactly the case
// retention must not touch.
//
// It runs with cross-tenant access because it serves the whole deployment and
// has no request to take a tenant from; without it, row level security would
// hide every archive from the coverage join and no partition would ever qualify.
func (s *Store) DropArchivedLogPartitions(
	ctx context.Context,
	retentionDays int,
) (*store.LogPartitionDropResult, error) {
	if retentionDays < 0 {
		return nil, &store.InvalidInputError{Field: "retention_days", Message: "must be >= 0"}
	}

	partitions, err := s.listDailyLogPartitions(ctx)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -retentionDays)
	result := &store.LogPartitionDropResult{}

	for _, p := range partitions {
		if !p.day.Before(cutoff) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}

		covered, err := s.logPartitionFullyArchived(ctx, p.day)
		if err != nil {
			return result, err
		}
		if !covered {
			result.Retained = append(result.Retained, p.name)
			continue
		}

		if _, err := s.pool.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pgx.Identifier{p.name}.Sanitize())); err != nil {
			return result, fmt.Errorf("dropping log partition %s: %w", p.name, err)
		}
		result.Dropped = append(result.Dropped, p.name)
	}

	return result, nil
}

type logPartition struct {
	name string
	day  time.Time
}

func (s *Store) listDailyLogPartitions(ctx context.Context) ([]logPartition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_inherits i ON c.oid = i.inhrelid
		JOIN pg_class p ON i.inhparent = p.oid
		WHERE p.relname = 'logs'
		ORDER BY c.relname`)
	if err != nil {
		return nil, fmt.Errorf("listing log partitions: %w", err)
	}
	defer rows.Close()

	var out []logPartition
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning log partition: %w", err)
		}
		m := dailyLogPartition.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		day, err := time.ParseInLocation("20060102", m[1], time.UTC)
		if err != nil {
			continue
		}
		out = append(out, logPartition{name: name, day: day})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log partitions: %w", err)
	}
	return out, nil
}

// logPartitionFullyArchived reports whether every row of one day is covered.
//
// It queries the parent table with the day as a range rather than the partition
// directly: daily partitions carry no policy of their own - they inherit the
// parent's, and only when reached through it - so a direct query would read
// them with row level security off.
func (s *Store) logPartitionFullyArchived(ctx context.Context, day time.Time) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("beginning coverage transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, setSystemSQL); err != nil {
		return false, fmt.Errorf("granting system access: %w", err)
	}

	var uncovered bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM logs l
			LEFT JOIN log_archives a
			  ON a.session_id = l.session_id AND a.deleted_at IS NULL
			WHERE l.created_at >= $1 AND l.created_at < $2
			  AND (a.id IS NULL OR a.last_log_at IS NULL OR l.created_at > a.last_log_at)
		)`, day, day.AddDate(0, 0, 1)).Scan(&uncovered)
	if err != nil {
		return false, fmt.Errorf("checking archive coverage for %s: %w", day.Format("2006-01-02"), err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("committing coverage transaction: %w", err)
	}
	return !uncovered, nil
}

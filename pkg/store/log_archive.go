package store

import "time"

// Types for the log archiver (pkg/jobs.LogArchiver) and the retention coverage
// check it unlocks.
//
// They are deliberately not on the Store interface. Archiving is background
// maintenance rather than a domain operation, the queries behind it are
// PostgreSQL-specific (partition bounds, row-tuple comparison), and the job
// depends on narrow interfaces that *postgres.Store satisfies. The same
// reasoning keeps partition maintenance off the interface.

// LogCursor is the exact position of one log row in a session's stream.
//
// A timestamp alone is not a position: log rows share created_at, and the
// archiver's delete has to stop at exactly the last row it wrote or it deletes
// a row it never archived. Ordering is (created_at, sequence, id), which is
// total because id is unique.
type LogCursor struct {
	CreatedAt time.Time
	Sequence  int64
	ID        string
}

// LogArchiveCandidate is a session whose logs are ready to move out of
// PostgreSQL.
type LogArchiveCandidate struct {
	SessionID string
	TenantID  *string
	Status    string
}

// ListLogArchiveCandidatesOptions selects which sessions are ready.
type ListLogArchiveCandidatesOptions struct {
	// TerminatedAfter is how long a terminated session must have been
	// terminated. It is a grace window, not a retention policy: it exists so
	// the archiver never races an in-flight log write.
	TerminatedAfter time.Duration

	// IdleAfter archives sessions that are still alive but have been quiet for
	// this long. Zero disables it, leaving only terminated sessions - which is
	// the conservative setting, because a live session can produce more logs
	// and each further pass has to append to its archive.
	IdleAfter time.Duration

	// Limit bounds one pass. Zero means the package default.
	Limit int
}

// LogPartitionDropResult reports what a retention pass did.
//
// Retained is not an error: a partition still holding rows no archive covers is
// exactly the case retention must not touch, and naming them is how an operator
// discovers that archiving has fallen behind.
type LogPartitionDropResult struct {
	Dropped  []string
	Retained []string
}

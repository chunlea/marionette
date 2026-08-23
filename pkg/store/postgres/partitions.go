package postgres

import (
	"context"
	"fmt"

	"github.com/chunlea/marionette/pkg/store"
)

// Partition maintenance is deliberately kept off the store.Store interface: it
// is PostgreSQL-specific administration, not a domain operation. The recurring
// caller is pkg/jobs.PartitionMaintainer, which depends on a narrow interface
// that *Store satisfies.

// MaintainLogPartitions ensures a daily partition of `logs` exists for today
// and for the next daysAhead days. It is idempotent.
//
// Without this running regularly, inserts into `logs` land in the logs_default
// partition (migration 007) instead of a daily one, which keeps writes working
// but defeats retention.
func (s *Store) MaintainLogPartitions(ctx context.Context, daysAhead int) error {
	if daysAhead < 0 {
		return &store.InvalidInputError{Field: "days_ahead", Message: "must be >= 0"}
	}

	if _, err := s.pool.Exec(ctx, "SELECT maintain_log_partitions($1::int)", daysAhead); err != nil {
		return fmt.Errorf("maintaining log partitions: %w", err)
	}
	return nil
}

// Retention lives in log_archive.go as DropArchivedLogPartitions. There is no
// unconditional "drop everything older than N days" here on purpose: the
// migration's drop_old_log_partitions() function is a date comparison and
// nothing more, and a caller that reached for it would delete the only copy of
// the logs in every partition it matched.

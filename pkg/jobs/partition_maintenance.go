package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// LogPartitioner maintains the daily partitions of the logs table.
// *postgres.Store implements it.
type LogPartitioner interface {
	// MaintainLogPartitions ensures partitions exist for today and the next
	// daysAhead days.
	MaintainLogPartitions(ctx context.Context, daysAhead int) error
}

// ArchiveAwareLogPartitioner drops partitions the log archives cover.
//
// Retention is a separate interface rather than a method on LogPartitioner
// because dropping a partition without that check deletes the only copy of the
// logs in it. A store that cannot answer "is this day archived?" does not get
// asked to drop anything: the maintainer refuses retention rather than falling
// back to a date comparison.
type ArchiveAwareLogPartitioner interface {
	DropArchivedLogPartitions(ctx context.Context, retentionDays int) (*store.LogPartitionDropResult, error)
}

// PartitionMaintainer keeps the `logs` partition set ahead of the clock.
//
// The initial migration created partitions once and nothing renewed them, so
// every deployment eventually reached the point where inserts failed with
// "no partition of relation logs found for row". Migration 007 added a default
// partition so writes survive, and this job keeps the daily partitions coming
// so retention still works.
type PartitionMaintainer struct {
	partitioner   LogPartitioner
	retention     ArchiveAwareLogPartitioner
	interval      time.Duration
	daysAhead     int
	retentionDays int
	logger        *zap.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	// Metrics
	lastRun         time.Time
	totalRuns       int64
	consecutiveErrs int
}

// PartitionMaintainerConfig contains configuration for the partition job.
type PartitionMaintainerConfig struct {
	// Interval is how often to run maintenance. Default: 24 hours.
	// Partitions are created DaysAhead days in advance, so a missed run is not
	// fatal.
	Interval time.Duration

	// DaysAhead is how many days of partitions to keep ahead. Default: 7.
	DaysAhead int

	// RetentionDays drops daily partitions older than this many days.
	//
	// Zero (the default) disables dropping. A non-zero value is safe only
	// because the drop is archive-gated: DropArchivedLogPartitions retains any
	// partition still holding rows no live archive covers, so switching
	// retention on without the archiver running drops nothing rather than
	// deleting the only copy of the logs. Turn it on with archiving, not
	// before - otherwise the partitions accumulate and the setting reads as
	// working when it is not doing anything.
	RetentionDays int

	// Logger is the structured logger to use.
	Logger *zap.Logger
}

// PartitionMaintenanceResult contains the result of a maintenance run.
type PartitionMaintenanceResult struct {
	Duration time.Duration
	Dropped  bool // whether the retention step ran

	// DroppedPartitions and RetainedPartitions name what retention did.
	// Retained is not a failure: a partition holding rows no archive covers is
	// exactly what retention must leave alone, and naming them is how an
	// operator discovers that archiving has fallen behind.
	DroppedPartitions  []string
	RetainedPartitions []string
}

// NewPartitionMaintainer creates a new log partition maintenance job.
func NewPartitionMaintainer(partitioner LogPartitioner, config PartitionMaintainerConfig) *PartitionMaintainer {
	if config.Interval <= 0 {
		config.Interval = 24 * time.Hour
	}
	if config.DaysAhead <= 0 {
		config.DaysAhead = 7
	}
	if config.RetentionDays < 0 {
		config.RetentionDays = 0
	}
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	retention, _ := partitioner.(ArchiveAwareLogPartitioner)
	if config.RetentionDays > 0 && retention == nil {
		config.Logger.Error("log retention is configured but the store cannot check archive coverage; retention disabled",
			zap.Int("retention_days", config.RetentionDays))
		config.RetentionDays = 0
	}

	return &PartitionMaintainer{
		partitioner:   partitioner,
		retention:     retention,
		interval:      config.Interval,
		daysAhead:     config.DaysAhead,
		retentionDays: config.RetentionDays,
		logger:        config.Logger,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the periodic maintenance job.
//
// Maintenance runs once immediately: a server booting into an already-degraded
// partition set must not wait a full interval to repair it.
func (j *PartitionMaintainer) Start(ctx context.Context) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("partition maintainer already running")
	}
	j.running = true
	j.stopCh = make(chan struct{})
	j.doneCh = make(chan struct{})
	j.mu.Unlock()

	go j.run(ctx)
	j.logger.Info("partition maintainer started",
		zap.Duration("interval", j.interval),
		zap.Int("days_ahead", j.daysAhead),
		zap.Int("retention_days", j.retentionDays),
	)
	return nil
}

// Stop stops the maintenance job gracefully.
func (j *PartitionMaintainer) Stop(ctx context.Context) error {
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
		j.logger.Info("partition maintainer stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning returns whether the job is currently running.
func (j *PartitionMaintainer) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// LastRun returns the time of the last maintenance run.
func (j *PartitionMaintainer) LastRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRun
}

// Stats returns maintenance statistics.
func (j *PartitionMaintainer) Stats() (totalRuns int64, consecutiveErrs int) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.totalRuns, j.consecutiveErrs
}

// RunNow triggers an immediate maintenance run.
func (j *PartitionMaintainer) RunNow(ctx context.Context) (*PartitionMaintenanceResult, error) {
	return j.runMaintenance(ctx)
}

func (j *PartitionMaintainer) run(ctx context.Context) {
	defer func() {
		j.mu.Lock()
		j.running = false
		close(j.doneCh)
		j.mu.Unlock()
	}()

	j.runAndRecord(ctx)

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-j.stopCh:
			return
		case <-ticker.C:
			j.runAndRecord(ctx)
		}
	}
}

func (j *PartitionMaintainer) runAndRecord(ctx context.Context) {
	if _, err := j.runMaintenance(ctx); err != nil {
		j.mu.Lock()
		j.consecutiveErrs++
		errs := j.consecutiveErrs
		j.mu.Unlock()

		j.logger.Error("log partition maintenance failed",
			zap.Int("consecutive_errors", errs),
			zap.Error(err),
		)
		return
	}

	j.mu.Lock()
	j.consecutiveErrs = 0
	j.mu.Unlock()
}

func (j *PartitionMaintainer) runMaintenance(ctx context.Context) (*PartitionMaintenanceResult, error) {
	start := time.Now()

	if err := j.partitioner.MaintainLogPartitions(ctx, j.daysAhead); err != nil {
		return nil, fmt.Errorf("creating log partitions: %w", err)
	}

	result := &PartitionMaintenanceResult{}
	if j.retentionDays > 0 {
		dropResult, err := j.retention.DropArchivedLogPartitions(ctx, j.retentionDays)
		if err != nil {
			return nil, fmt.Errorf("dropping old log partitions: %w", err)
		}
		result.Dropped = true
		result.DroppedPartitions = dropResult.Dropped
		result.RetainedPartitions = dropResult.Retained

		if len(dropResult.Retained) > 0 {
			// Loud, because the usual cause is archiving falling behind, and a
			// silent retain looks exactly like a working retention policy.
			j.logger.Warn("log partitions past retention were kept because no archive covers them",
				zap.Strings("partitions", dropResult.Retained),
				zap.Int("retention_days", j.retentionDays),
			)
		}
		if len(dropResult.Dropped) > 0 {
			j.logger.Info("dropped archived log partitions",
				zap.Strings("partitions", dropResult.Dropped),
			)
		}
	}

	result.Duration = time.Since(start)

	j.mu.Lock()
	j.lastRun = time.Now()
	j.totalRuns++
	j.mu.Unlock()

	j.logger.Debug("log partition maintenance completed",
		zap.Int("days_ahead", j.daysAhead),
		zap.Bool("retention_applied", result.Dropped),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

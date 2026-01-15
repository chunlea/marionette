package core

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// ScheduledSessionActivator is a background job that monitors sessions with
// lifecycle_mode='scheduled' and activates them when their next_scheduled_at
// time arrives.
//
// This enables the scheduled session lifecycle mode where:
// - Sessions stay suspended between scheduled runs
// - Auto-resume at scheduled times (based on schedule_cron)
// - Execute their scheduled tasks when active
// - Auto-suspend again after scheduled tasks complete (handled elsewhere)
type ScheduledSessionActivator struct {
	store         store.Store
	sessionMgr    SessionManagerInterface
	checkInterval time.Duration
	batchSize     int
	logger        *zap.Logger

	// Control channels
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
	mu      sync.RWMutex
}

// ScheduledSessionActivatorConfig holds configuration for the activator.
type ScheduledSessionActivatorConfig struct {
	Store         store.Store
	SessionMgr    SessionManagerInterface
	CheckInterval time.Duration
	BatchSize     int
	Logger        *zap.Logger
}

// NewScheduledSessionActivator creates a new ScheduledSessionActivator.
func NewScheduledSessionActivator(cfg ScheduledSessionActivatorConfig) *ScheduledSessionActivator {
	checkInterval := cfg.CheckInterval
	if checkInterval == 0 {
		checkInterval = 30 * time.Second // Default: check every 30 seconds
	}

	batchSize := cfg.BatchSize
	if batchSize == 0 {
		batchSize = 50 // Default: process up to 50 sessions per poll
	}

	return &ScheduledSessionActivator{
		store:         cfg.Store,
		sessionMgr:    cfg.SessionMgr,
		checkInterval: checkInterval,
		batchSize:     batchSize,
		logger:        cfg.Logger.Named("scheduled_session_activator"),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}
}

// Start begins the background job.
func (a *ScheduledSessionActivator) Start() error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = true
	a.stopCh = make(chan struct{})
	a.doneCh = make(chan struct{})
	a.mu.Unlock()

	a.logger.Info("starting scheduled session activator",
		zap.Duration("check_interval", a.checkInterval),
		zap.Int("batch_size", a.batchSize),
	)

	go a.run()
	return nil
}

// Stop gracefully stops the background job.
func (a *ScheduledSessionActivator) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	a.running = false
	close(a.stopCh)
	a.mu.Unlock()

	// Wait for the run goroutine to finish
	<-a.doneCh
	a.logger.Info("scheduled session activator stopped")
}

// run is the main loop that polls for due scheduled sessions.
func (a *ScheduledSessionActivator) run() {
	defer close(a.doneCh)

	ticker := time.NewTicker(a.checkInterval)
	defer ticker.Stop()

	// Run immediately on start
	a.checkAndActivate()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.checkAndActivate()
		}
	}
}

// checkAndActivate checks for due scheduled sessions and activates them.
func (a *ScheduledSessionActivator) checkAndActivate() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	now := time.Now()

	sessions, err := a.store.GetDueScheduledSessions(ctx, now, a.batchSize)
	if err != nil {
		a.logger.Error("failed to get due scheduled sessions", zap.Error(err))
		return
	}

	if len(sessions) == 0 {
		return
	}

	a.logger.Debug("found due scheduled sessions", zap.Int("count", len(sessions)))

	// Process sessions concurrently
	var wg sync.WaitGroup
	for _, session := range sessions {
		wg.Add(1)
		go func(session *store.Session) {
			defer wg.Done()
			a.activateSession(ctx, session)
		}(session)
	}
	wg.Wait()
}

// activateSession resumes a scheduled session and updates its next_scheduled_at.
func (a *ScheduledSessionActivator) activateSession(ctx context.Context, session *store.Session) {
	sessionID := session.ID

	a.logger.Info("activating scheduled session",
		zap.String("session_id", sessionID),
		zap.Timep("next_scheduled_at", session.NextScheduledAt),
	)

	// Resume the session
	if err := a.sessionMgr.Resume(ctx, sessionID); err != nil {
		a.logger.Error("failed to resume scheduled session",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return
	}

	// Calculate and update next_scheduled_at
	nextRunAt, err := a.calculateNextScheduledAt(session)
	if err != nil {
		a.logger.Error("failed to calculate next scheduled time",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		// Session is resumed, but we couldn't calculate next time
		// This might happen if cron expression is invalid
		return
	}

	// Update the session with the next scheduled time
	updates := store.SessionUpdates{
		NextScheduledAt: &nextRunAt,
	}
	if err := a.store.UpdateSession(ctx, sessionID, updates); err != nil {
		a.logger.Error("failed to update next scheduled time",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
	}

	a.logger.Info("scheduled session activated",
		zap.String("session_id", sessionID),
		zap.Time("next_scheduled_at", nextRunAt),
	)
}

// calculateNextScheduledAt calculates the next scheduled time based on the cron expression.
func (a *ScheduledSessionActivator) calculateNextScheduledAt(session *store.Session) (time.Time, error) {
	if session.ScheduleCron == nil || *session.ScheduleCron == "" {
		return time.Time{}, ErrScheduleCronRequired
	}

	cronExpr := *session.ScheduleCron

	// Get timezone
	timezone := "UTC"
	if session.ScheduleTimezone != nil && *session.ScheduleTimezone != "" {
		timezone = *session.ScheduleTimezone
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		a.logger.Warn("invalid timezone, using UTC",
			zap.String("session_id", session.ID),
			zap.String("timezone", timezone),
			zap.Error(err),
		)
		loc = time.UTC
	}

	// Create parser with timezone support
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}

	// Calculate next run time from now in the specified timezone
	now := time.Now().In(loc)
	nextRunAt := schedule.Next(now)

	return nextRunAt, nil
}

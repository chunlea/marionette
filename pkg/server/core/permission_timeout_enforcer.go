package core

import (
	"context"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Default permission timeout enforcement configuration.
const (
	DefaultPermissionTimeoutCheckInterval = 60 * time.Second
)

// PermissionTimeoutEnforcer monitors pending permission requests and
// suspends sessions that exceed their suspend_after_seconds timeout.
type PermissionTimeoutEnforcer struct {
	store         store.Store
	sessionMgr    SessionManagerInterface
	checkInterval time.Duration
	logger        *zap.Logger

	stopCh chan struct{}
	doneCh chan struct{}
}

// PermissionTimeoutEnforcerOption is a functional option for PermissionTimeoutEnforcer.
type PermissionTimeoutEnforcerOption func(*PermissionTimeoutEnforcer)

// WithPermissionTimeoutCheckInterval sets the check interval for the timeout enforcer.
func WithPermissionTimeoutCheckInterval(d time.Duration) PermissionTimeoutEnforcerOption {
	return func(e *PermissionTimeoutEnforcer) {
		e.checkInterval = d
	}
}

// NewPermissionTimeoutEnforcer creates a new PermissionTimeoutEnforcer.
func NewPermissionTimeoutEnforcer(
	store store.Store,
	sessionMgr SessionManagerInterface,
	logger *zap.Logger,
	opts ...PermissionTimeoutEnforcerOption,
) *PermissionTimeoutEnforcer {
	e := &PermissionTimeoutEnforcer{
		store:         store,
		sessionMgr:    sessionMgr,
		checkInterval: DefaultPermissionTimeoutCheckInterval,
		logger:        logger,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start begins the background permission timeout enforcement loop.
func (e *PermissionTimeoutEnforcer) Start(ctx context.Context) {
	e.logger.Info("starting permission timeout enforcer",
		zap.Duration("check_interval", e.checkInterval),
	)

	go e.run(ctx)
}

// Stop stops the permission timeout enforcer.
func (e *PermissionTimeoutEnforcer) Stop() {
	e.logger.Info("stopping permission timeout enforcer")
	close(e.stopCh)
	<-e.doneCh
	e.logger.Info("permission timeout enforcer stopped")
}

// run is the main loop for the timeout enforcer.
func (e *PermissionTimeoutEnforcer) run(ctx context.Context) {
	defer close(e.doneCh)

	ticker := time.NewTicker(e.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.checkTimeouts(ctx); err != nil {
				e.logger.Error("failed to check permission timeouts", zap.Error(err))
			}
		}
	}
}

// checkTimeouts checks all pending permission requests for timeout.
// When a permission request exceeds its suspend_after_seconds, the session
// is suspended (but the permission request stays pending).
func (e *PermissionTimeoutEnforcer) checkTimeouts(ctx context.Context) error {
	// Get all pending permission requests
	perms, err := e.store.ListPermissionRequests(ctx, store.ListPermissionRequestsOptions{
		Status: []string{PermissionStatusPending},
	})
	if err != nil {
		return err
	}

	if len(perms.Items) == 0 {
		return nil
	}

	now := time.Now()
	suspendCount := 0

	for _, perm := range perms.Items {
		// Check if permission has exceeded suspend timeout
		elapsed := now.Sub(perm.CreatedAt)
		suspendAfter := time.Duration(perm.SuspendAfterSeconds) * time.Second

		if elapsed < suspendAfter {
			continue
		}

		// Get session to check if it needs to be suspended
		session, err := e.store.GetSession(ctx, perm.SessionID)
		if err != nil {
			e.logger.Error("failed to get session for permission timeout",
				zap.String("perm_id", perm.ID),
				zap.String("session_id", perm.SessionID),
				zap.Error(err),
			)
			continue
		}

		// Only suspend if session is active
		if session.Status != SessionStatusActive {
			continue
		}

		e.logger.Info("suspending session due to permission timeout",
			zap.String("session_id", session.ID),
			zap.String("perm_id", perm.ID),
			zap.Duration("elapsed", elapsed),
			zap.Duration("suspend_after", suspendAfter),
		)

		// Suspend the session
		if err := e.sessionMgr.Suspend(ctx, session.ID, "permission_timeout"); err != nil {
			e.logger.Error("failed to suspend session after permission timeout",
				zap.String("session_id", session.ID),
				zap.String("perm_id", perm.ID),
				zap.Error(err),
			)
			continue
		}

		suspendCount++
	}

	if suspendCount > 0 {
		e.logger.Info("suspended sessions due to permission timeout",
			zap.Int("count", suspendCount),
		)
	}

	return nil
}

// CheckTimeouts is exported for testing.
func (e *PermissionTimeoutEnforcer) CheckTimeouts(ctx context.Context) error {
	return e.checkTimeouts(ctx)
}

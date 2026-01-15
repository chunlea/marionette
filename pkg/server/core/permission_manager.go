package core

import (
	"context"
	"errors"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Permission status constants.
const (
	PermissionStatusPending  = "pending"
	PermissionStatusApproved = "approved"
	PermissionStatusDenied   = "denied"
	PermissionStatusCanceled = "canceled"
)

// Risk level constants.
const (
	RiskLevelLow      = "low"
	RiskLevelMedium   = "medium"
	RiskLevelHigh     = "high"
	RiskLevelCritical = "critical"
)

// Default permission timeout configuration.
const (
	DefaultSuspendAfterSeconds = 1800 // 30 minutes
)

// Permission-related errors.
var (
	ErrPermissionNotFound         = errors.New("permission request not found")
	ErrPermissionAlreadyResponded = errors.New("permission request already responded")
	ErrPermissionNotPending       = errors.New("permission request is not pending")
)

// PermissionManager handles permission request lifecycle.
type PermissionManager struct {
	store      store.Store
	cmdSender  CommandSender
	sessionMgr SessionManagerInterface
	auditLog   audit.Logger
	logger     *zap.Logger
	webhooks   *WebhookIntegration
}

// NewPermissionManager creates a new PermissionManager.
func NewPermissionManager(
	store store.Store,
	cmdSender CommandSender,
	sessionMgr SessionManagerInterface,
	auditLog audit.Logger,
	logger *zap.Logger,
) *PermissionManager {
	return &PermissionManager{
		store:      store,
		cmdSender:  cmdSender,
		sessionMgr: sessionMgr,
		auditLog:   auditLog,
		logger:     logger,
	}
}

// SetWebhookIntegration sets the webhook integration for dispatching events.
func (m *PermissionManager) SetWebhookIntegration(wi *WebhookIntegration) {
	m.webhooks = wi
}

// Create stores a new permission request from runner.
func (m *PermissionManager) Create(ctx context.Context, req *CreatePermissionRequestInput) (*store.PermissionRequest, error) {
	// Set defaults
	suspendAfter := req.SuspendAfterSeconds
	if suspendAfter <= 0 {
		suspendAfter = DefaultSuspendAfterSeconds
	}

	riskLevel := req.RiskLevel
	if riskLevel == "" {
		riskLevel = RiskLevelMedium
	}

	now := time.Now()
	perm := &store.PermissionRequest{
		ID:                  id.PermissionRequest(),
		OriginalRequestID:   req.OriginalRequestID,
		SessionID:           req.SessionID,
		TaskID:              req.TaskID,
		RunID:               req.RunID,
		Tool:                req.Tool,
		Action:              req.Action,
		RiskLevel:           riskLevel,
		Status:              PermissionStatusPending,
		SuspendAfterSeconds: suspendAfter,
		TenantID:            req.TenantID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	// Set optional context
	if req.Context != "" {
		perm.Context = &req.Context
	}

	if err := m.store.CreatePermissionRequest(ctx, perm); err != nil {
		return nil, err
	}

	m.logger.Info("permission request created",
		zap.String("perm_id", perm.ID),
		zap.String("session_id", perm.SessionID),
		zap.String("tool", perm.Tool),
		zap.String("risk_level", perm.RiskLevel),
	)

	// Dispatch webhook event
	if m.webhooks != nil {
		m.webhooks.DispatchPermissionEvent(ctx, "permission.requested", perm)
	}

	return perm, nil
}

// Respond approves or denies a permission request.
func (m *PermissionManager) Respond(ctx context.Context, permID string, approved bool, reason, respondedBy string) error {
	perm, err := m.store.GetPermissionRequest(ctx, permID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrPermissionNotFound
		}
		return err
	}

	if perm.Status != PermissionStatusPending {
		return ErrPermissionAlreadyResponded
	}

	// Update status
	status := PermissionStatusDenied
	if approved {
		status = PermissionStatusApproved
	}

	now := time.Now()
	updates := store.PermissionRequestUpdates{
		Status:      &status,
		RespondedAt: &now,
	}
	if respondedBy != "" {
		updates.RespondedBy = &respondedBy
	}
	if reason != "" {
		updates.ResponseReason = &reason
	}

	if err := m.store.UpdatePermissionRequest(ctx, permID, updates); err != nil {
		return err
	}

	m.logger.Info("permission request responded",
		zap.String("perm_id", permID),
		zap.Bool("approved", approved),
		zap.String("responded_by", respondedBy),
	)

	// Dispatch webhook event
	if m.webhooks != nil {
		eventType := "permission.denied"
		if approved {
			eventType = "permission.approved"
		}
		// Update perm struct for webhook dispatch
		perm.Status = status
		perm.RespondedAt = &now
		if respondedBy != "" {
			perm.RespondedBy = &respondedBy
		}
		if reason != "" {
			perm.ResponseReason = &reason
		}
		m.webhooks.DispatchPermissionEvent(ctx, eventType, perm)
	}

	// Log audit event
	if m.auditLog != nil {
		action := audit.ActionPermissionDenied
		if approved {
			action = audit.ActionPermissionApproved
		}
		_ = audit.NewEvent(action).
			WithActor(audit.ActorTypeAPIKey, respondedBy, "").
			WithResource(audit.ResourceTypePermissionRequest, permID).
			WithSession(perm.SessionID).
			WithTask(perm.TaskID).
			WithDetails(map[string]any{
				"tool":       perm.Tool,
				"action":     perm.Action,
				"reason":     reason,
				"risk_level": perm.RiskLevel,
			}).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	// Get session to find runner
	session, err := m.store.GetSession(ctx, perm.SessionID)
	if err != nil {
		m.logger.Warn("failed to get session for permission response",
			zap.String("session_id", perm.SessionID),
			zap.Error(err),
		)
		return nil // Permission is updated, session lookup is not fatal
	}

	// If session is suspended, resume it
	if session.Status == SessionStatusSuspended && m.sessionMgr != nil {
		m.logger.Info("resuming suspended session after permission response",
			zap.String("session_id", session.ID),
		)
		if err := m.sessionMgr.Resume(ctx, session.ID); err != nil {
			m.logger.Warn("failed to resume session",
				zap.String("session_id", session.ID),
				zap.Error(err),
			)
			// Not fatal - permission is still updated
		}
	}

	// Send response to runner if connected
	if session.RunnerID != nil && m.cmdSender != nil {
		cmd := &pb.ServerCommand{
			Payload: &pb.ServerCommand_ApprovePermission{
				ApprovePermission: &pb.ApprovePermission{
					RequestId: perm.OriginalRequestID, // Use original request ID from agent
					Approved:  approved,
					Reason:    reason,
				},
			},
		}

		if err := m.cmdSender.SendCommand(*session.RunnerID, cmd); err != nil {
			m.logger.Warn("failed to send permission response to runner",
				zap.String("runner_id", *session.RunnerID),
				zap.String("perm_id", permID),
				zap.Error(err),
			)
			// Not a fatal error - runner will poll on reconnect
		} else {
			m.logger.Debug("permission response sent to runner",
				zap.String("runner_id", *session.RunnerID),
				zap.String("perm_id", permID),
				zap.Bool("approved", approved),
			)
		}
	}

	return nil
}

// Get retrieves a permission request by ID.
func (m *PermissionManager) Get(ctx context.Context, permID string) (*store.PermissionRequest, error) {
	perm, err := m.store.GetPermissionRequest(ctx, permID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrPermissionNotFound
		}
		return nil, err
	}
	return perm, nil
}

// List retrieves permission requests with filters.
func (m *PermissionManager) List(ctx context.Context, opts ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return m.store.ListPermissionRequests(ctx, opts)
}

// Cancel cancels a pending permission request.
func (m *PermissionManager) Cancel(ctx context.Context, permID string) error {
	perm, err := m.store.GetPermissionRequest(ctx, permID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrPermissionNotFound
		}
		return err
	}

	if perm.Status != PermissionStatusPending {
		return ErrPermissionNotPending
	}

	status := PermissionStatusCanceled
	now := time.Now()
	updates := store.PermissionRequestUpdates{
		Status:      &status,
		RespondedAt: &now,
	}

	if err := m.store.UpdatePermissionRequest(ctx, permID, updates); err != nil {
		return err
	}

	m.logger.Info("permission request canceled",
		zap.String("perm_id", permID),
	)

	// Dispatch webhook event
	if m.webhooks != nil {
		perm.Status = status
		perm.RespondedAt = &now
		m.webhooks.DispatchPermissionEvent(ctx, "permission.canceled", perm)
	}

	// Log audit event
	if m.auditLog != nil {
		_ = audit.NewEvent(audit.ActionPermissionCanceled).
			WithSystemActor().
			WithResource(audit.ResourceTypePermissionRequest, permID).
			WithSession(perm.SessionID).
			WithTask(perm.TaskID).
			WithDetails(map[string]any{
				"tool":       perm.Tool,
				"action":     perm.Action,
				"risk_level": perm.RiskLevel,
			}).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	return nil
}

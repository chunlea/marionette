// Package core provides business logic for the Marionette server.
package core

import (
	"context"
	"errors"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// RunnerRegistry handles runner registration and lookup.
type RunnerRegistry struct {
	store    store.Store
	tokenSvc *auth.RunnerTokenService
	logger   *zap.Logger
}

// NewRunnerRegistry creates a new RunnerRegistry.
func NewRunnerRegistry(store store.Store, tokenSvc *auth.RunnerTokenService, logger *zap.Logger) *RunnerRegistry {
	return &RunnerRegistry{
		store:    store,
		tokenSvc: tokenSvc,
		logger:   logger,
	}
}

// RegisterRequest contains the data needed to register a runner.
type RegisterRequest struct {
	Token        string            // Runner token (required)
	Name         string            // Runner name
	Hostname     string            // Runner hostname
	SandboxMode  string            // Sandbox mode
	SandboxTypes []string          // Available sandbox types
	Capabilities []string          // Runner capabilities
	Labels       map[string]string // Key-value labels
}

// RegisterResult contains the result of runner registration.
type RegisterResult struct {
	RunnerID string // The runner ID (new or existing)
	IsNew    bool   // True if a new runner was created
	PoolName string // Pool name from token
}

// ErrTokenRequired is returned when no token is provided.
var ErrTokenRequired = errors.New("runner token is required")

// ErrTokenBoundToOtherRunner is returned when token is bound to a different runner.
var ErrTokenBoundToOtherRunner = errors.New("token is bound to a different runner")

// Register registers a new runner or updates an existing one.
// The registration flow:
// 1. Validate token via tokenSvc.Validate()
// 2. If token is bound to a runner -> check it exists, update it
// 3. If token is not bound -> look up by name or create new runner
// 4. Bind token to runner if not already bound
func (r *RunnerRegistry) Register(ctx context.Context, req *RegisterRequest) (*RegisterResult, error) {
	if req.Token == "" {
		return nil, ErrTokenRequired
	}

	// Validate token
	tokenInfo, err := r.tokenSvc.Validate(ctx, req.Token)
	if err != nil {
		r.logger.Warn("token validation failed", zap.Error(err))
		return nil, err
	}

	r.logger.Debug("token validated",
		zap.String("token_id", tokenInfo.ID),
		zap.String("pool_name", tokenInfo.PoolName),
		zap.Any("runner_id", tokenInfo.RunnerID),
	)

	// Check if token is already bound to a runner
	if tokenInfo.RunnerID != nil && *tokenInfo.RunnerID != "" {
		return r.handleBoundToken(ctx, req, tokenInfo)
	}

	// Token is not bound - look up or create runner
	return r.handleUnboundToken(ctx, req, tokenInfo)
}

// handleBoundToken handles registration when token is already bound to a runner.
func (r *RunnerRegistry) handleBoundToken(ctx context.Context, req *RegisterRequest, tokenInfo *store.RunnerToken) (*RegisterResult, error) {
	runnerID := *tokenInfo.RunnerID

	// Verify runner exists
	runner, err := r.store.GetRunner(ctx, runnerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			r.logger.Warn("token bound to non-existent runner, unbinding",
				zap.String("token_id", tokenInfo.ID),
				zap.String("runner_id", runnerID),
			)
			// Runner was deleted, unbind and create new
			if unbindErr := r.tokenSvc.UnbindRunner(ctx, tokenInfo.ID); unbindErr != nil {
				r.logger.Error("failed to unbind token", zap.Error(unbindErr))
			}
			// Fall through to create new runner
			return r.createNewRunner(ctx, req, tokenInfo)
		}
		return nil, err
	}

	// Runner exists - update it
	updates := store.RunnerUpdates{
		Hostname: &req.Hostname,
	}
	if req.SandboxMode != "" {
		updates.SandboxMode = &req.SandboxMode
	}
	if len(req.SandboxTypes) > 0 {
		updates.SandboxTypes = req.SandboxTypes
	}
	if len(req.Capabilities) > 0 {
		updates.Capabilities = req.Capabilities
	}

	if err := r.store.UpdateRunner(ctx, runner.ID, updates); err != nil {
		r.logger.Error("failed to update runner", zap.Error(err))
		return nil, err
	}

	r.logger.Info("existing runner re-registered",
		zap.String("runner_id", runner.ID),
		zap.String("name", runner.Name),
	)

	return &RegisterResult{
		RunnerID: runner.ID,
		IsNew:    false,
		PoolName: tokenInfo.PoolName,
	}, nil
}

// handleUnboundToken handles registration when token is not yet bound.
func (r *RunnerRegistry) handleUnboundToken(ctx context.Context, req *RegisterRequest, tokenInfo *store.RunnerToken) (*RegisterResult, error) {
	// Try to find existing runner by name
	if req.Name != "" {
		runner, err := r.store.GetRunnerByName(ctx, req.Name)
		if err == nil {
			// Found existing runner with same name
			// Bind token and update runner
			if err := r.tokenSvc.BindRunner(ctx, tokenInfo.ID, runner.ID); err != nil {
				r.logger.Error("failed to bind token to existing runner", zap.Error(err))
				return nil, err
			}

			// Update runner info
			updates := store.RunnerUpdates{
				Hostname: &req.Hostname,
			}
			if req.SandboxMode != "" {
				updates.SandboxMode = &req.SandboxMode
			}
			if err := r.store.UpdateRunner(ctx, runner.ID, updates); err != nil {
				r.logger.Warn("failed to update runner", zap.Error(err))
			}

			r.logger.Info("token bound to existing runner",
				zap.String("token_id", tokenInfo.ID),
				zap.String("runner_id", runner.ID),
				zap.String("name", runner.Name),
			)

			return &RegisterResult{
				RunnerID: runner.ID,
				IsNew:    false,
				PoolName: tokenInfo.PoolName,
			}, nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		// Runner not found by name, fall through to create new
	}

	return r.createNewRunner(ctx, req, tokenInfo)
}

// createNewRunner creates a new runner and binds the token to it.
func (r *RunnerRegistry) createNewRunner(ctx context.Context, req *RegisterRequest, tokenInfo *store.RunnerToken) (*RegisterResult, error) {
	// Ensure slice fields are never nil (database has NOT NULL constraints)
	sandboxTypes := req.SandboxTypes
	if sandboxTypes == nil {
		sandboxTypes = []string{}
	}
	capabilities := req.Capabilities
	if capabilities == nil {
		capabilities = []string{}
	}

	runner := &store.Runner{
		Name:         req.Name,
		Hostname:     req.Hostname,
		Status:       "offline", // Will be set to idle on Connect
		SandboxMode:  req.SandboxMode,
		SandboxTypes: sandboxTypes,
		Capabilities: capabilities,
		PoolName:     &tokenInfo.PoolName,
		TenantID:     tokenInfo.TenantID,
	}

	if runner.SandboxMode == "" {
		runner.SandboxMode = "runner-is-sandbox"
	}

	if err := r.store.CreateRunner(ctx, runner); err != nil {
		r.logger.Error("failed to create runner", zap.Error(err))
		return nil, err
	}

	// Bind token to new runner
	if err := r.tokenSvc.BindRunner(ctx, tokenInfo.ID, runner.ID); err != nil {
		r.logger.Error("failed to bind token to new runner",
			zap.String("token_id", tokenInfo.ID),
			zap.String("runner_id", runner.ID),
			zap.Error(err),
		)
		// Don't fail registration, runner is created
	}

	r.logger.Info("new runner registered",
		zap.String("runner_id", runner.ID),
		zap.String("name", runner.Name),
		zap.String("pool", tokenInfo.PoolName),
	)

	return &RegisterResult{
		RunnerID: runner.ID,
		IsNew:    true,
		PoolName: tokenInfo.PoolName,
	}, nil
}

// Get retrieves a runner by ID.
func (r *RunnerRegistry) Get(ctx context.Context, runnerID string) (*store.Runner, error) {
	return r.store.GetRunner(ctx, runnerID)
}

// GetByName retrieves a runner by name.
func (r *RunnerRegistry) GetByName(ctx context.Context, name string) (*store.Runner, error) {
	return r.store.GetRunnerByName(ctx, name)
}

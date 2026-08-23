package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// WorkspaceStore defines the minimal store interface needed by WorkspaceManager.
type WorkspaceStore interface {
	CreateWorkspace(ctx context.Context, ws *store.Workspace) error
	GetWorkspace(ctx context.Context, id string) (*store.Workspace, error)
	ListWorkspaces(ctx context.Context, opts store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error)
	UpdateWorkspace(ctx context.Context, id string, updates store.WorkspaceUpdates) error
	DeleteWorkspace(ctx context.Context, id string) error
	ListSessions(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error)
}

// Workspace storage type constants.
const (
	WorkspaceStorageTypeVolume = "volume"
)

// Workspace mobility constants.
const (
	WorkspaceMobilityLocal      = "local"
	WorkspaceMobilityShared     = "shared"
	WorkspaceMobilityObjectSync = "object_sync"
)

// Default workspace settings.
const (
	DefaultWorkspaceBaseDir = "/var/marionette/workspaces"
)

// Workspace-related errors.
var (
	ErrWorkspaceNotFound      = errors.New("workspace not found")
	ErrWorkspaceAlreadyExists = errors.New("workspace already exists")
	ErrWorkspaceDeleted       = errors.New("workspace has been deleted")
	ErrWorkspaceInUse         = errors.New("workspace is in use by a session")
	ErrInvalidWorkspaceName   = errors.New("invalid workspace name")
)

// WorkspaceManagerInterface defines the interface for workspace management.
type WorkspaceManagerInterface interface {
	Create(ctx context.Context, opts CreateWorkspaceOptions) (*store.Workspace, error)
	Get(ctx context.Context, workspaceID string) (*store.Workspace, error)
	List(ctx context.Context, opts ListWorkspacesOptions) (*store.ListResult[store.Workspace], error)
	Update(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) (*store.Workspace, error)
	Delete(ctx context.Context, workspaceID string) error
	GetHostPath(ctx context.Context, workspaceID string) (string, error)
	EnsureHostDirectory(ctx context.Context, workspaceID string) (string, error)
	CleanupHostDirectory(ctx context.Context, workspaceID string) error
	IsInUse(ctx context.Context, workspaceID string) (bool, error)
}

// WorkspaceManager handles workspace lifecycle and storage.
type WorkspaceManager struct {
	store  WorkspaceStore
	config config.WorkspaceStorageConfig
	logger *zap.Logger

	// mu serialises host directory creation and removal so two sessions
	// starting on the same workspace cannot race mkdir against RemoveAll.
	mu sync.Mutex
}

// NewWorkspaceManager creates a new WorkspaceManager.
//
// It takes the narrow WorkspaceStore rather than the full store.Store: the
// manager only ever reads and writes workspaces and looks up the sessions using
// them, and the half-applied narrow-interface refactor left the field narrow
// while the constructor still demanded everything.
func NewWorkspaceManager(store WorkspaceStore, cfg config.WorkspaceStorageConfig, logger *zap.Logger) *WorkspaceManager {
	if cfg.BaseDir == "" {
		cfg.BaseDir = DefaultWorkspaceBaseDir
	}
	return &WorkspaceManager{
		store:  store,
		config: cfg,
		logger: logger,
	}
}

// CreateWorkspaceOptions contains options for creating a workspace.
type CreateWorkspaceOptions struct {
	Name        string            // Optional workspace name
	Persist     *bool             // Whether to persist workspace (default: true)
	StorageType string            // Storage type (default: "volume")
	Mobility    string            // Mobility mode (default: "local")
	DiskQuotaMB *int              // Disk quota in MB (nil for default)
	TenantID    *string           // For multi-tenant deployments
	Labels      map[string]string // Optional metadata labels
	Annotations map[string]string // Optional annotations
}

// ListWorkspacesOptions contains options for listing workspaces.
type ListWorkspacesOptions struct {
	Limit    int
	Cursor   string
	TenantID *string
}

// Create creates a new workspace with a corresponding host directory.
func (m *WorkspaceManager) Create(ctx context.Context, opts CreateWorkspaceOptions) (*store.Workspace, error) {
	wsID := id.Workspace()

	// Set defaults
	name := opts.Name
	if name == "" {
		name = "workspace-" + wsID
	}

	persist := true
	if opts.Persist != nil {
		persist = *opts.Persist
	}

	storageType := opts.StorageType
	if storageType == "" {
		storageType = WorkspaceStorageTypeVolume
	}

	mobility := opts.Mobility
	if mobility == "" {
		mobility = WorkspaceMobilityLocal
	}

	// Set disk quota
	var diskQuotaMB *int
	if opts.DiskQuotaMB != nil {
		diskQuotaMB = opts.DiskQuotaMB
	} else if m.config.DefaultQuotaMB > 0 {
		quota := m.config.DefaultQuotaMB
		diskQuotaMB = &quota
	}

	// Prepare labels and annotations
	labels, err := json.Marshal(opts.Labels)
	if err != nil {
		labels = []byte("{}")
	}
	annotations, err := json.Marshal(opts.Annotations)
	if err != nil {
		annotations = []byte("{}")
	}

	ws := &store.Workspace{
		ID:          wsID,
		Name:        name,
		Persist:     persist,
		StorageType: storageType,
		Mobility:    mobility,
		DiskQuotaMB: diskQuotaMB,
		TenantID:    opts.TenantID,
		Labels:      labels,
		Annotations: annotations,
	}

	// Create workspace in database
	if err := m.store.CreateWorkspace(ctx, ws); err != nil {
		return nil, err
	}

	m.logger.Info("workspace created",
		zap.String("workspace_id", wsID),
		zap.String("name", name),
		zap.String("storage_type", storageType),
		zap.String("mobility", mobility),
	)

	// Get the created workspace with all fields populated
	return m.store.GetWorkspace(ctx, wsID)
}

// Get retrieves a workspace by ID.
func (m *WorkspaceManager) Get(ctx context.Context, workspaceID string) (*store.Workspace, error) {
	ws, err := m.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrWorkspaceNotFound
		}
		return nil, err
	}

	if ws.DeletedAt != nil {
		return nil, ErrWorkspaceDeleted
	}

	return ws, nil
}

// List returns workspaces matching the filter options.
func (m *WorkspaceManager) List(ctx context.Context, opts ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	storeOpts := store.ListWorkspacesOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		TenantID: opts.TenantID,
	}
	return m.store.ListWorkspaces(ctx, storeOpts)
}

// Update updates a workspace's mutable fields.
func (m *WorkspaceManager) Update(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
	// Verify workspace exists
	ws, err := m.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	if err := m.store.UpdateWorkspace(ctx, ws.ID, updates); err != nil {
		return nil, err
	}

	return m.store.GetWorkspace(ctx, workspaceID)
}

// Delete soft-deletes a workspace.
// The actual host directory cleanup is handled separately based on configuration.
func (m *WorkspaceManager) Delete(ctx context.Context, workspaceID string) error {
	// Verify workspace exists
	ws, err := m.Get(ctx, workspaceID)
	if err != nil {
		return err
	}

	// Check if workspace is in use
	inUse, err := m.IsInUse(ctx, workspaceID)
	if err != nil {
		return err
	}
	if inUse {
		return ErrWorkspaceInUse
	}

	// Soft delete the workspace
	if err := m.store.DeleteWorkspace(ctx, ws.ID); err != nil {
		return err
	}

	m.logger.Info("workspace deleted",
		zap.String("workspace_id", workspaceID),
	)

	// Optionally cleanup host directory based on configuration
	if m.config.CleanupOnTerminate {
		if err := m.CleanupHostDirectory(ctx, workspaceID); err != nil {
			m.logger.Warn("failed to cleanup workspace host directory",
				zap.String("workspace_id", workspaceID),
				zap.Error(err),
			)
			// Don't return error - the database deletion succeeded
		}
	}

	return nil
}

// GetHostPath returns the host filesystem path for a workspace.
// This path is used for mounting the workspace into containers.
func (m *WorkspaceManager) GetHostPath(_ context.Context, workspaceID string) (string, error) {
	return filepath.Join(m.config.BaseDir, workspaceID), nil
}

// EnsureHostDirectory creates the host directory for a workspace if it doesn't exist.
// Returns the host path that was created or already exists.
func (m *WorkspaceManager) EnsureHostDirectory(ctx context.Context, workspaceID string) (string, error) {
	hostPath, err := m.GetHostPath(ctx, workspaceID)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if directory already exists
	if _, err := os.Stat(hostPath); err == nil {
		return hostPath, nil
	}

	// Create the directory with proper permissions
	// 0755 allows owner full access, group and others read/execute
	if err := os.MkdirAll(hostPath, 0755); err != nil {
		return "", err
	}

	m.logger.Debug("workspace host directory created",
		zap.String("workspace_id", workspaceID),
		zap.String("path", hostPath),
	)

	return hostPath, nil
}

// CleanupHostDirectory removes the host directory for a workspace.
func (m *WorkspaceManager) CleanupHostDirectory(ctx context.Context, workspaceID string) error {
	hostPath, err := m.GetHostPath(ctx, workspaceID)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if directory exists
	if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		return nil
	}

	// Remove the directory and all contents
	if err := os.RemoveAll(hostPath); err != nil {
		return err
	}

	m.logger.Debug("workspace host directory cleaned up",
		zap.String("workspace_id", workspaceID),
		zap.String("path", hostPath),
	)

	return nil
}

// IsInUse checks if a workspace is currently being used by any active session.
func (m *WorkspaceManager) IsInUse(ctx context.Context, workspaceID string) (bool, error) {
	// Filter to live sessions in the query rather than after it. Fetching one
	// row and then discarding it for being terminated reported "not in use"
	// whenever the terminated session happened to come back first - and callers
	// act on that by deleting the directory.
	result, err := m.store.ListSessions(ctx, store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit: 1,
		},
		WorkspaceID: &workspaceID,
		Status: []string{
			SessionStatusPending,
			SessionStatusActive,
			SessionStatusSuspended,
			SessionStatusResuming,
		},
	})
	if err != nil {
		return false, err
	}

	return len(result.Items) > 0, nil
}

// GetBaseDir returns the configured base directory for workspaces.
func (m *WorkspaceManager) GetBaseDir() string {
	return m.config.BaseDir
}

// Ensure WorkspaceManager implements WorkspaceManagerInterface.
var _ WorkspaceManagerInterface = (*WorkspaceManager)(nil)

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chunlea/marionette/pkg/storage/cas"
	"go.uber.org/zap"
)

// Workspace sync moves a runner's workspace into content-addressable storage
// so a session can survive losing its runner, and pulls it back when the
// session resumes somewhere else.
//
// The rule this file exists to enforce: a suspend never fails because a sync
// failed, and a sync is never reported as done unless the manifest is durably
// stored. The previous implementation reported neither honestly - it set
// workspaceSynced = false and returned Success: true regardless, with a TODO
// where the sync should have been.

var (
	// ErrSyncUnavailable is returned when no CAS backend is configured, so
	// there is nowhere to sync to.
	ErrSyncUnavailable = errors.New("workspace sync is not configured")

	// ErrNoWorkspaceIdentity is returned when the server has not told the
	// runner which workspace it is holding, so a sync would have no safe key
	// to store under.
	ErrNoWorkspaceIdentity = errors.New("workspace identity was not supplied by the server")

	// ErrNoManifest is returned when a restore is asked for without a snapshot
	// to restore from.
	ErrNoManifest = errors.New("no manifest id to restore from")
)

// WorkspaceIdentity is the key a workspace is stored under in CAS.
//
// It must identify the workspace, not the session: two sessions on the same
// workspace have to resolve to the same snapshot history. WorkspacePath cannot
// serve as this key - the server sends the literal string "/workspace" for
// every container-mode session, so keying on it would collide every Docker
// session's snapshots with every other one.
type WorkspaceIdentity struct {
	// WorkspaceID is the server's workspace id (ws_xxx).
	WorkspaceID string

	// TenantID scopes the CAS namespace. Empty is valid: single-tenant
	// deployments store everything under the empty tenant.
	TenantID string
}

// Known reports whether the identity is usable as a CAS key.
func (w WorkspaceIdentity) Known() bool { return w.WorkspaceID != "" }

// SyncResult describes what a sync attempt actually achieved. It is what the
// suspend response reports, so it must never claim more than happened.
type SyncResult struct {
	// Synced is true only once the manifest is durably stored.
	Synced bool

	// ManifestID identifies the stored snapshot when Synced is true.
	ManifestID string

	// Reason explains why Synced is false. Empty when Synced is true.
	Reason string
}

// WorkspaceSyncer syncs a workspace directory to CAS and restores it back.
type WorkspaceSyncer struct {
	// newSyncer builds a cas.Syncer scoped to one workspace. It is a factory
	// rather than a single instance because cas.Sync.RestoreFromManifest drops
	// the workspace id (see pinnedManifestStore), so the store has to be told
	// which workspace a restore is for.
	newSyncer func(workspaceID string) cas.Syncer
	logger    *zap.Logger
}

// NewWorkspaceSyncer creates a syncer over a fixed CAS implementation.
// A nil cas.Syncer produces a syncer that honestly reports itself unavailable.
func NewWorkspaceSyncer(syncer cas.Syncer, logger *zap.Logger) *WorkspaceSyncer {
	if syncer == nil {
		return &WorkspaceSyncer{logger: logger.Named("workspace-sync")}
	}
	return &WorkspaceSyncer{
		newSyncer: func(string) cas.Syncer { return syncer },
		logger:    logger.Named("workspace-sync"),
	}
}

// Available reports whether a CAS backend is configured.
func (s *WorkspaceSyncer) Available() bool {
	return s != nil && s.newSyncer != nil
}

// Sync stores the workspace at dir and returns what actually happened.
//
// The error is returned for logging and tests; callers on the suspend path
// must use the SyncResult and let the suspend succeed regardless.
func (s *WorkspaceSyncer) Sync(ctx context.Context, id WorkspaceIdentity, dir string) (SyncResult, error) {
	if !s.Available() {
		return SyncResult{Reason: ErrSyncUnavailable.Error()}, ErrSyncUnavailable
	}
	if !id.Known() {
		return SyncResult{Reason: ErrNoWorkspaceIdentity.Error()}, ErrNoWorkspaceIdentity
	}

	if _, err := os.Stat(dir); err != nil {
		reason := fmt.Sprintf("workspace directory is unreadable: %v", err)
		return SyncResult{Reason: reason}, fmt.Errorf("stat workspace: %w", err)
	}

	manifestID, err := s.newSyncer(id.WorkspaceID).Sync(ctx, id.WorkspaceID, id.TenantID, dir)
	if err != nil {
		return SyncResult{Reason: fmt.Sprintf("sync failed: %v", err)}, err
	}

	s.logger.Info("workspace synced",
		zap.String("workspace_id", id.WorkspaceID),
		zap.String("manifest_id", manifestID),
		zap.String("dir", dir),
	)

	return SyncResult{Synced: true, ManifestID: manifestID}, nil
}

// Restore materializes a specific snapshot into dir.
//
// The manifest id is required. cas.Syncer.Restore, which would look up the
// latest snapshot for a workspace, is unimplemented upstream, so the id has to
// travel with the session - see the lane report's NEEDS for the wire fields.
func (s *WorkspaceSyncer) Restore(ctx context.Context, id WorkspaceIdentity, manifestID, dir string) error {
	if !s.Available() {
		return ErrSyncUnavailable
	}
	if !id.Known() {
		return ErrNoWorkspaceIdentity
	}
	if manifestID == "" {
		return ErrNoManifest
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}

	if err := s.newSyncer(id.WorkspaceID).RestoreFromManifest(ctx, manifestID, id.TenantID, dir); err != nil {
		return fmt.Errorf("restore workspace: %w", err)
	}

	s.logger.Info("workspace restored",
		zap.String("workspace_id", id.WorkspaceID),
		zap.String("manifest_id", manifestID),
		zap.String("dir", dir),
	)
	return nil
}

// IsEmptyDir reports whether dir is missing or contains no entries. A restore
// is only worth attempting when the runner has nothing local to work from;
// overwriting a populated workspace would discard work the runner still holds.
func IsEmptyDir(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

// buildCASSyncer constructs a cas.Syncer from the agent's storage settings.
// It returns nil when no backend is configured, which is the default: a runner
// with nowhere to sync says so rather than pretending.
func buildCASSyncer(cfg StorageConfig, logger *zap.Logger) (func(workspaceID string) cas.Syncer, error) {
	switch cfg.Backend {
	case "", StorageBackendNone:
		return nil, nil

	case StorageBackendLocal:
		if cfg.LocalPath == "" {
			return nil, errors.New("storage.local_path is required for the local backend")
		}
		provider, err := cas.NewLocalProvider(cfg.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("create local CAS provider: %w", err)
		}

		encryptor, err := buildCASEncryptor(cfg, logger)
		if err != nil {
			return nil, err
		}

		casCfg := cas.DefaultConfig
		if casCfg.TempDir == "" {
			casCfg.TempDir = filepath.Join(os.TempDir(), "marionette-cas")
		}

		chunkStore := cas.NewBlobChunkStore(provider, encryptor)
		manifestStore := cas.NewBlobManifestStore(provider, encryptor)

		return func(workspaceID string) cas.Syncer {
			return cas.NewSync(casCfg, chunkStore, pinnedManifestStore{
				ManifestStore: manifestStore,
				workspaceID:   workspaceID,
			})
		}, nil

	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.Backend)
	}
}

// buildCASEncryptor resolves the configured encryption mode.
//
// The mode is deliberately not defaulted. Chunks written unencrypted are a
// decision an operator makes on purpose, not something a missing config value
// decides for them.
func buildCASEncryptor(cfg StorageConfig, logger *zap.Logger) (cas.Encryptor, error) {
	switch cfg.Encryption {
	case "":
		return nil, errors.New(
			"storage.encryption must be set explicitly when a storage backend is configured")

	case StorageEncryptionNone:
		logger.Warn("workspace chunks will be stored UNENCRYPTED",
			zap.String("backend", cfg.Backend),
			zap.String("path", cfg.LocalPath),
		)
		return cas.NewNoOpEncryptor(), nil

	default:
		// Per-tenant encryption needs the tenant's data key, which the runner
		// has no way to obtain today. Refusing beats silently downgrading to
		// no encryption.
		return nil, fmt.Errorf(
			"storage.encryption %q is not supported by the runner: it has no way to obtain a tenant data key",
			cfg.Encryption)
	}
}

// NewWorkspaceSyncerFromConfig builds the syncer described by the agent's
// storage settings. It always returns a usable *WorkspaceSyncer: with no
// backend configured the result reports itself unavailable rather than being
// nil, so callers never have to nil-check before asking.
func NewWorkspaceSyncerFromConfig(cfg StorageConfig, logger *zap.Logger) (*WorkspaceSyncer, error) {
	factory, err := buildCASSyncer(cfg, logger)
	if err != nil {
		return nil, err
	}
	return &WorkspaceSyncer{newSyncer: factory, logger: logger.Named("workspace-sync")}, nil
}

// pinnedManifestStore supplies the workspace id that
// cas.Sync.RestoreFromManifest fails to pass down.
//
// Manifests are stored under manifests/{tenant}/{workspace}/{manifest}, and
// SaveManifest keys them correctly from the manifest itself. RestoreFromManifest
// however calls StreamManifestFiles with an empty workspace id, producing a key
// with a hole in it that matches nothing that was ever written. This wrapper
// fills the hole for reads. It is a workaround in this lane, not a fix: the
// upstream call should pass the workspace id through. See the lane report.
type pinnedManifestStore struct {
	cas.ManifestStore
	workspaceID string
}

func (p pinnedManifestStore) LoadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*cas.Manifest, error) {
	return p.ManifestStore.LoadManifest(ctx, tenantID, p.resolve(workspaceID), manifestID)
}

func (p pinnedManifestStore) StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan cas.ManifestFile, *cas.ManifestHeader, error) {
	return p.ManifestStore.StreamManifestFiles(ctx, tenantID, p.resolve(workspaceID), manifestID)
}

func (p pinnedManifestStore) DeleteManifest(ctx context.Context, tenantID, workspaceID, manifestID string) error {
	return p.ManifestStore.DeleteManifest(ctx, tenantID, p.resolve(workspaceID), manifestID)
}

// resolve prefers the caller's workspace id and falls back to the pinned one.
func (p pinnedManifestStore) resolve(workspaceID string) string {
	if workspaceID != "" {
		return workspaceID
	}
	return p.workspaceID
}

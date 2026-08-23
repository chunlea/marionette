package agent

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/storage/cas"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// newTestSyncer builds a syncer over a local CAS backend in a temp directory,
// which is the same code path the agent uses in production for
// storage.backend=local.
func newTestSyncer(t *testing.T) *WorkspaceSyncer {
	t.Helper()

	logger := zaptest.NewLogger(t)
	syncer, err := NewWorkspaceSyncerFromConfig(StorageConfig{
		Backend:    StorageBackendLocal,
		LocalPath:  t.TempDir(),
		Encryption: StorageEncryptionNone,
	}, logger)
	require.NoError(t, err)
	require.True(t, syncer.Available())

	return syncer
}

// writeTree materializes files into root and returns their contents by path.
func writeTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, content, 0o644))
	}
}

// readTree reads every regular file under root, keyed by relative path.
func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()

	out := map[string][]byte{}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = content
		return nil
	}))
	return out
}

// randomBytes produces incompressible content, so the chunker is exercised
// rather than the compressor swallowing everything.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()

	buf := make([]byte, n)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return buf
}

// TestWorkspaceSyncer_RoundTrip is the property the whole feature rests on: a
// workspace that goes into CAS comes back out byte-identical, on a different
// directory, the way a resumed session lands on a different runner.
func TestWorkspaceSyncer_RoundTrip(t *testing.T) {
	syncer := newTestSyncer(t)
	id := WorkspaceIdentity{WorkspaceID: "ws_roundtrip", TenantID: "tenant_a"}

	source := t.TempDir()
	files := map[string][]byte{
		"README.md":             []byte("# marionette\n"),
		"src/main.go":           []byte("package main\n\nfunc main() {}\n"),
		"src/nested/deep/a.txt": []byte("nested content"),
		"empty.txt":             {},
		"binary.bin":            randomBytes(t, 3*1024*1024),
	}
	writeTree(t, source, files)

	ctx := context.Background()

	result, err := syncer.Sync(ctx, id, source)
	require.NoError(t, err)
	require.True(t, result.Synced, "reason: %s", result.Reason)
	assert.NotEmpty(t, result.ManifestID)
	assert.Empty(t, result.Reason)

	// Restore into a fresh directory, as a different runner would.
	target := filepath.Join(t.TempDir(), "restored")
	require.NoError(t, syncer.Restore(ctx, id, result.ManifestID, target))

	restored := readTree(t, target)

	wantPaths := make([]string, 0, len(files))
	for name := range files {
		wantPaths = append(wantPaths, filepath.FromSlash(name))
	}
	gotPaths := make([]string, 0, len(restored))
	for name := range restored {
		gotPaths = append(gotPaths, name)
	}
	sort.Strings(wantPaths)
	sort.Strings(gotPaths)
	require.Equal(t, wantPaths, gotPaths, "restored file set must match")

	for name, want := range files {
		got := restored[filepath.FromSlash(name)]
		assert.Equalf(t, want, got, "file %s must be byte-identical", name)
	}
}

// TestWorkspaceSyncer_RoundTripAfterChange covers the resume-then-suspend-again
// cycle: a second sync must capture the newer state, not replay the first.
func TestWorkspaceSyncer_RoundTripAfterChange(t *testing.T) {
	syncer := newTestSyncer(t)
	id := WorkspaceIdentity{WorkspaceID: "ws_incremental", TenantID: ""}
	ctx := context.Background()

	source := t.TempDir()
	writeTree(t, source, map[string][]byte{"a.txt": []byte("first")})

	first, err := syncer.Sync(ctx, id, source)
	require.NoError(t, err)
	require.True(t, first.Synced)

	// Change the workspace the way a task would, then sync again.
	writeTree(t, source, map[string][]byte{
		"a.txt": []byte("second"),
		"b.txt": []byte("added"),
	})

	second, err := syncer.Sync(ctx, id, source)
	require.NoError(t, err)
	require.True(t, second.Synced)
	assert.NotEqual(t, first.ManifestID, second.ManifestID, "each sync is its own snapshot")

	target := filepath.Join(t.TempDir(), "restored")
	require.NoError(t, syncer.Restore(ctx, id, second.ManifestID, target))

	restored := readTree(t, target)
	assert.Equal(t, []byte("second"), restored["a.txt"], "restore must return the newer snapshot")
	assert.Equal(t, []byte("added"), restored["b.txt"])
}

// TestWorkspaceSyncer_TenantsAreIsolated pins that the tenant is part of the
// key, so one tenant's workspace can never restore into another's runner.
func TestWorkspaceSyncer_TenantsAreIsolated(t *testing.T) {
	syncer := newTestSyncer(t)
	ctx := context.Background()

	source := t.TempDir()
	writeTree(t, source, map[string][]byte{"secret.txt": []byte("tenant a only")})

	result, err := syncer.Sync(ctx, WorkspaceIdentity{WorkspaceID: "ws_shared", TenantID: "tenant_a"}, source)
	require.NoError(t, err)
	require.True(t, result.Synced)

	target := filepath.Join(t.TempDir(), "restored")
	err = syncer.Restore(ctx, WorkspaceIdentity{WorkspaceID: "ws_shared", TenantID: "tenant_b"},
		result.ManifestID, target)
	require.Error(t, err, "tenant b must not see tenant a's snapshot")

	empty, emptyErr := IsEmptyDir(target)
	require.NoError(t, emptyErr)
	assert.True(t, empty, "a failed restore must not leave partial content behind")
}

// TestWorkspaceSyncer_HonestFailures covers every path that must report
// "not synced" with a reason instead of claiming success.
func TestWorkspaceSyncer_HonestFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("no backend configured", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		syncer, err := NewWorkspaceSyncerFromConfig(StorageConfig{Backend: StorageBackendNone}, logger)
		require.NoError(t, err)
		assert.False(t, syncer.Available())

		result, err := syncer.Sync(ctx, WorkspaceIdentity{WorkspaceID: "ws_x"}, t.TempDir())
		assert.ErrorIs(t, err, ErrSyncUnavailable)
		assert.False(t, result.Synced)
		assert.NotEmpty(t, result.Reason)

		assert.ErrorIs(t, syncer.Restore(ctx, WorkspaceIdentity{WorkspaceID: "ws_x"}, "mfst_x", t.TempDir()),
			ErrSyncUnavailable)
	})

	t.Run("workspace identity unknown", func(t *testing.T) {
		syncer := newTestSyncer(t)

		result, err := syncer.Sync(ctx, WorkspaceIdentity{}, t.TempDir())
		assert.ErrorIs(t, err, ErrNoWorkspaceIdentity)
		assert.False(t, result.Synced)
		assert.Contains(t, result.Reason, "workspace identity")
	})

	t.Run("workspace directory missing", func(t *testing.T) {
		syncer := newTestSyncer(t)

		result, err := syncer.Sync(ctx, WorkspaceIdentity{WorkspaceID: "ws_gone"},
			filepath.Join(t.TempDir(), "does-not-exist"))
		require.Error(t, err)
		assert.False(t, result.Synced)
		assert.Contains(t, result.Reason, "unreadable")
	})

	t.Run("restore without a manifest id", func(t *testing.T) {
		syncer := newTestSyncer(t)

		err := syncer.Restore(ctx, WorkspaceIdentity{WorkspaceID: "ws_never_synced"}, "", t.TempDir())
		assert.ErrorIs(t, err, ErrNoManifest)
	})

	t.Run("restore of a manifest that does not exist", func(t *testing.T) {
		syncer := newTestSyncer(t)

		err := syncer.Restore(ctx, WorkspaceIdentity{WorkspaceID: "ws_never_synced"}, "mfst_nope", t.TempDir())
		assert.Error(t, err)
	})
}

func TestBuildCASSyncer_Config(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		cfg     StorageConfig
		wantErr string
		wantNil bool
	}{
		{name: "default is disabled", cfg: StorageConfig{}, wantNil: true},
		{name: "explicit none", cfg: StorageConfig{Backend: StorageBackendNone}, wantNil: true},
		{
			name:    "local without path",
			cfg:     StorageConfig{Backend: StorageBackendLocal, Encryption: StorageEncryptionNone},
			wantErr: "local_path is required",
		},
		{
			name:    "encryption must be explicit",
			cfg:     StorageConfig{Backend: StorageBackendLocal, LocalPath: t.TempDir()},
			wantErr: "must be set explicitly",
		},
		{
			name: "tenant encryption is refused rather than downgraded",
			cfg: StorageConfig{
				Backend: StorageBackendLocal, LocalPath: t.TempDir(), Encryption: "tenant",
			},
			wantErr: "no way to obtain a tenant data key",
		},
		{
			name:    "unknown backend",
			cfg:     StorageConfig{Backend: "gopher-holes"},
			wantErr: "unknown storage backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syncer, err := buildCASSyncer(tt.cfg, logger)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, syncer)
			} else {
				assert.NotNil(t, syncer)
			}
		})
	}
}

func TestIsEmptyDir(t *testing.T) {
	empty := t.TempDir()
	got, err := IsEmptyDir(empty)
	require.NoError(t, err)
	assert.True(t, got)

	got, err = IsEmptyDir(filepath.Join(empty, "missing"))
	require.NoError(t, err)
	assert.True(t, got, "a missing directory counts as empty")

	writeTree(t, empty, map[string][]byte{"a.txt": []byte("x")})
	got, err = IsEmptyDir(empty)
	require.NoError(t, err)
	assert.False(t, got)
}

// failingSyncer stands in for a CAS backend that is up but erroring.
type failingSyncer struct{ cas.Syncer }

func (failingSyncer) Sync(context.Context, string, string, string) (string, error) {
	return "", errors.New("object store unreachable")
}

func (failingSyncer) RestoreFromManifest(context.Context, string, string, string) error {
	return errors.New("object store unreachable")
}

// TestHandleDetachSession_SyncFailureDoesNotBlockSuspend is the coordinator's
// verdict encoded as a test: a suspend still succeeds when the sync fails, and
// says so rather than reporting a workspace it never saved.
func TestHandleDetachSession_SyncFailureDoesNotBlockSuspend(t *testing.T) {
	logger := zaptest.NewLogger(t)
	wsMgr := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(wsMgr, logger)
	handler.SetWorkspaceSyncer(NewWorkspaceSyncer(failingSyncer{}, logger))

	attachForSync(t, handler, "sess_syncfail", WorkspaceIdentity{WorkspaceID: "ws_syncfail"}, "")

	msg, err := handler.HandleDetachSession(context.Background(), &pb.DetachSession{
		SessionId:   "sess_syncfail",
		SaveContext: true,
		Suspend:     &pb.SuspendConfig{Strategy: "release_to_pool", SyncWorkspace: true},
	})
	require.NoError(t, err)

	suspended := msg.GetSessionSuspended()
	require.NotNil(t, suspended)
	assert.True(t, suspended.Success, "a failed sync must not fail the suspend")
	assert.False(t, suspended.WorkspaceSynced, "a failed sync must not report as synced")
}

// TestHandleDetachSession_ReportsSyncTruthfully walks the suspend path with a
// working backend and asserts the flag is earned, not assumed.
func TestHandleDetachSession_ReportsSyncTruthfully(t *testing.T) {
	logger := zaptest.NewLogger(t)
	base := t.TempDir()
	wsMgr := NewWorkspaceManager(base, logger)
	handler := NewDefaultCommandHandler(wsMgr, logger)
	handler.SetWorkspaceSyncer(newTestSyncer(t))

	attachForSync(t, handler, "sess_ok", WorkspaceIdentity{WorkspaceID: "ws_ok"}, "")
	writeTree(t, filepath.Join(base, "ws_ok"), map[string][]byte{"work.txt": []byte("done")})

	msg, err := handler.HandleDetachSession(context.Background(), &pb.DetachSession{
		SessionId:   "sess_ok",
		SaveContext: true,
		Suspend:     &pb.SuspendConfig{Strategy: "release_to_pool", SyncWorkspace: true},
	})
	require.NoError(t, err)

	suspended := msg.GetSessionSuspended()
	require.NotNil(t, suspended)
	assert.True(t, suspended.Success)
	assert.True(t, suspended.WorkspaceSynced, "a successful sync must be reported as synced")
}

// TestHandleDetachSession_NoSyncerReportsNotSynced pins the default posture: a
// runner with nowhere to sync reports the workspace as unsaved.
func TestHandleDetachSession_NoSyncerReportsNotSynced(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger)

	attachForSync(t, handler, "sess_nosync", WorkspaceIdentity{WorkspaceID: "ws_nosync"}, "")

	msg, err := handler.HandleDetachSession(context.Background(), &pb.DetachSession{
		SessionId: "sess_nosync",
		Suspend:   &pb.SuspendConfig{Strategy: "release_to_pool", SyncWorkspace: true},
	})
	require.NoError(t, err)

	suspended := msg.GetSessionSuspended()
	require.NotNil(t, suspended)
	assert.True(t, suspended.Success)
	assert.False(t, suspended.WorkspaceSynced)
}

// TestHandleAttachSession_RestoresEmptyWorkspace walks suspend then resume
// through the handler, which is the shape the server drives.
func TestHandleAttachSession_RestoresEmptyWorkspace(t *testing.T) {
	logger := zaptest.NewLogger(t)
	syncer := newTestSyncer(t)
	ctx := context.Background()
	id := WorkspaceIdentity{WorkspaceID: "ws_resume"}

	// A previous runner synced this workspace.
	source := t.TempDir()
	writeTree(t, source, map[string][]byte{"kept.txt": []byte("survives the runner")})
	result, err := syncer.Sync(ctx, id, source)
	require.NoError(t, err)
	require.True(t, result.Synced)

	// A fresh runner attaches with nothing local.
	base := t.TempDir()
	handler := NewDefaultCommandHandler(NewWorkspaceManager(base, logger), logger)
	handler.SetWorkspaceSyncer(syncer)

	attachForSync(t, handler, "sess_resume", id, result.ManifestID)

	restored, err := os.ReadFile(filepath.Join(base, "ws_resume", "kept.txt"))
	require.NoError(t, err, "the workspace must have been restored on attach")
	assert.Equal(t, []byte("survives the runner"), restored)
}

// TestHandleAttachSession_KeepsPopulatedWorkspace pins that a re-attach never
// overwrites work the runner still holds with an older snapshot.
func TestHandleAttachSession_KeepsPopulatedWorkspace(t *testing.T) {
	logger := zaptest.NewLogger(t)
	syncer := newTestSyncer(t)
	ctx := context.Background()
	id := WorkspaceIdentity{WorkspaceID: "ws_keep"}

	source := t.TempDir()
	writeTree(t, source, map[string][]byte{"file.txt": []byte("old snapshot")})
	result, err := syncer.Sync(ctx, id, source)
	require.NoError(t, err)

	base := t.TempDir()
	local := filepath.Join(base, "ws_keep")
	writeTree(t, local, map[string][]byte{"file.txt": []byte("newer local work")})

	handler := NewDefaultCommandHandler(NewWorkspaceManager(base, logger), logger)
	handler.SetWorkspaceSyncer(syncer)

	attachForSync(t, handler, "sess_keep", id, result.ManifestID)

	content, err := os.ReadFile(filepath.Join(local, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, []byte("newer local work"), content,
		"restore must not clobber a populated workspace")
}

// attachForSync attaches a session and forces the workspace identity that the
// wire cannot carry yet.
//
// workspaceIdentityFromAttach returns an unknown identity today because
// AttachSession has no workspace_id field; see the lane report's NEEDS. Once it
// does, this helper collapses to a plain HandleAttachSession call.
func attachForSync(t *testing.T, handler *DefaultCommandHandler, sessionID string, id WorkspaceIdentity, manifestID string) {
	t.Helper()

	_, err := handler.HandleAttachSession(context.Background(), &pb.AttachSession{
		SessionId:     sessionID,
		WorkspacePath: id.WorkspaceID,
		AgentConfig:   &pb.AgentConfig{Agent: "claude"},
	})
	require.NoError(t, err)

	handler.sessionsMu.Lock()
	session := handler.sessions[sessionID]
	handler.sessionsMu.Unlock()
	require.NotNil(t, session)

	session.Workspace = id
	session.WorkspaceManifestID = manifestID
	handler.restoreWorkspace(context.Background(), session)
}

// =============================================================================
// Workspace identity comes from the wire, not from the mount path
// =============================================================================

// TestWorkspaceIdentityFromAttach is the fix for a CAS key collision. The
// server sends "/workspace" as workspace_path for every container-mode session,
// so a key built from the path would put every session in the deployment into
// one snapshot history.
func TestWorkspaceIdentityFromAttach(t *testing.T) {
	t.Run("reads identity from the wire", func(t *testing.T) {
		id, manifestID := workspaceIdentityFromAttach(&pb.AttachSession{
			SessionId:           "sess_1",
			WorkspacePath:       "/workspace",
			WorkspaceId:         "ws_abc",
			TenantId:            "tenant_a",
			WorkspaceManifestId: "mfst_1",
		})

		assert.Equal(t, "ws_abc", id.WorkspaceID)
		assert.Equal(t, "tenant_a", id.TenantID)
		assert.Equal(t, "mfst_1", manifestID)
		assert.True(t, id.Known())
	})

	t.Run("a single-tenant deployment has no tenant", func(t *testing.T) {
		id, _ := workspaceIdentityFromAttach(&pb.AttachSession{
			WorkspacePath: "/workspace",
			WorkspaceId:   "ws_abc",
		})
		assert.Equal(t, "ws_abc", id.WorkspaceID)
		assert.Empty(t, id.TenantID)
		assert.True(t, id.Known(), "no tenant is still a usable key")
	})

	// An older server that does not send the field leaves the runner without a
	// safe key, and the syncer refuses rather than inventing one.
	t.Run("no workspace id is not a usable key", func(t *testing.T) {
		id, manifestID := workspaceIdentityFromAttach(&pb.AttachSession{
			WorkspacePath: "/workspace",
		})
		assert.False(t, id.Known())
		assert.Empty(t, manifestID)
	})

	t.Run("nil command", func(t *testing.T) {
		id, manifestID := workspaceIdentityFromAttach(nil)
		assert.False(t, id.Known())
		assert.Empty(t, manifestID)
	})
}

// TestWorkspaceSyncer_MountPathDoesNotCollideSessions is the round-trip test
// extended to the case that motivated the proto change: two sessions on
// different workspaces, both mounted at the same "/workspace", must not see
// each other's snapshots.
func TestWorkspaceSyncer_MountPathDoesNotCollideSessions(t *testing.T) {
	syncer := newTestSyncer(t)
	ctx := context.Background()

	// Two runners, each holding a different workspace, both told the mount
	// point is "/workspace" - which is exactly what the server sends.
	const sharedMountPath = "/workspace"

	firstID, firstManifest := workspaceIdentityFromAttach(&pb.AttachSession{
		SessionId: "sess_1", WorkspacePath: sharedMountPath, WorkspaceId: "ws_one",
	})
	secondID, _ := workspaceIdentityFromAttach(&pb.AttachSession{
		SessionId: "sess_2", WorkspacePath: sharedMountPath, WorkspaceId: "ws_two",
	})
	require.Empty(t, firstManifest, "a workspace that was never synced has nothing to restore")
	require.NotEqual(t, firstID, secondID,
		"identical mount paths must still produce different CAS keys")

	firstSource := t.TempDir()
	writeTree(t, firstSource, map[string][]byte{"a.txt": []byte("workspace one")})
	firstResult, err := syncer.Sync(ctx, firstID, firstSource)
	require.NoError(t, err)
	require.True(t, firstResult.Synced, "reason: %s", firstResult.Reason)

	secondSource := t.TempDir()
	writeTree(t, secondSource, map[string][]byte{"b.txt": []byte("workspace two")})
	secondResult, err := syncer.Sync(ctx, secondID, secondSource)
	require.NoError(t, err)
	require.True(t, secondResult.Synced, "reason: %s", secondResult.Reason)

	// Each workspace restores its own content, not the other's.
	firstTarget := filepath.Join(t.TempDir(), "first")
	require.NoError(t, syncer.Restore(ctx, firstID, firstResult.ManifestID, firstTarget))
	assert.Equal(t,
		map[string][]byte{filepath.FromSlash("a.txt"): []byte("workspace one")},
		readTree(t, firstTarget))

	secondTarget := filepath.Join(t.TempDir(), "second")
	require.NoError(t, syncer.Restore(ctx, secondID, secondResult.ManifestID, secondTarget))
	assert.Equal(t,
		map[string][]byte{filepath.FromSlash("b.txt"): []byte("workspace two")},
		readTree(t, secondTarget))

	// And one workspace's manifest is not resolvable under the other's key.
	crossTarget := filepath.Join(t.TempDir(), "cross")
	err = syncer.Restore(ctx, secondID, firstResult.ManifestID, crossTarget)
	require.Error(t, err,
		"a manifest from another workspace must not restore under this key")
}

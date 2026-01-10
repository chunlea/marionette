package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// Stream Tests
// =============================================================================

// createTestSessionForStream creates a test session with workspace for stream tests.
func createTestSessionForStream(t *testing.T, ctx context.Context) *store.Session {
	// Create a workspace first
	workspace := &store.Workspace{
		Name:        "test-workspace-" + time.Now().Format("150405.000000"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create a session
	session := &store.Session{
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		Status:        "active",
		NetworkPolicy: "allow_list",
		LifecycleMode: "on_demand",
		AllowedHosts:  []string{},
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	return session
}

func TestStreamCRUD(t *testing.T) {
	ctx := context.Background()

	// Create a session for the stream
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create stream
	iceServers, _ := json.Marshal([]map[string]any{
		{"urls": []string{"stun:stun.l.google.com:19302"}},
	})
	metadata, _ := json.Marshal(map[string]string{"key": "value"})

	stream := &store.Stream{
		SessionID:    session.ID,
		Type:         "desktop",
		State:        "pending",
		ICEServers:   iceServers,
		ProviderName: "webrtc-sfu",
		Metadata:     metadata,
		AudioEnabled: false,
		InputEnabled: true,
	}

	err := testStore.CreateStream(ctx, stream)
	require.NoError(t, err)
	assert.NotEmpty(t, stream.ID)
	assert.True(t, stream.ID[:5] == "strm_", "ID should have strm_ prefix")
	assert.NotZero(t, stream.CreatedAt)

	// Get
	got, err := testStore.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, stream.SessionID, got.SessionID)
	assert.Equal(t, stream.Type, got.Type)
	assert.Equal(t, stream.State, got.State)
	assert.Equal(t, stream.ProviderName, got.ProviderName)
	assert.Equal(t, stream.AudioEnabled, got.AudioEnabled)
	assert.Equal(t, stream.InputEnabled, got.InputEnabled)

	// Update
	newState := "active"
	signalingURL := "wss://example.com/signaling"
	width := 1920
	height := 1080
	frameRate := 30
	bitRate := 5000000
	videoCodec := "h264"
	now := time.Now()

	err = testStore.UpdateStream(ctx, stream.ID, store.StreamUpdates{
		State:            &newState,
		SignalingURL:     &signalingURL,
		ResolutionWidth:  &width,
		ResolutionHeight: &height,
		FrameRate:        &frameRate,
		BitRate:          &bitRate,
		VideoCodec:       &videoCodec,
		StartedAt:        &now,
	})
	require.NoError(t, err)

	got, err = testStore.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.State)
	assert.Equal(t, signalingURL, *got.SignalingURL)
	assert.Equal(t, width, *got.ResolutionWidth)
	assert.Equal(t, height, *got.ResolutionHeight)
	assert.Equal(t, frameRate, *got.FrameRate)
	assert.Equal(t, bitRate, *got.BitRate)
	assert.Equal(t, videoCodec, *got.VideoCodec)
	assert.NotNil(t, got.StartedAt)

	// Delete
	err = testStore.DeleteStream(ctx, stream.ID)
	require.NoError(t, err)

	_, err = testStore.GetStream(ctx, stream.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestStreamNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetStream(ctx, "strm_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "stream", notFoundErr.Resource)
}

func TestStreamUpdateNotFound(t *testing.T) {
	ctx := context.Background()

	newState := "active"
	err := testStore.UpdateStream(ctx, "strm_nonexistent12345", store.StreamUpdates{
		State: &newState,
	})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestStreamDeleteNotFound(t *testing.T) {
	ctx := context.Background()

	err := testStore.DeleteStream(ctx, "strm_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestStreamEmptyUpdate(t *testing.T) {
	ctx := context.Background()

	// Create a session for the stream
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create stream
	stream := &store.Stream{
		SessionID:    session.ID,
		Type:         "browser",
		State:        "pending",
		ProviderName: "webrtc-sfu",
	}
	err := testStore.CreateStream(ctx, stream)
	require.NoError(t, err)

	// Empty update should not fail
	err = testStore.UpdateStream(ctx, stream.ID, store.StreamUpdates{})
	require.NoError(t, err)

	// Cleanup
	_ = testStore.DeleteStream(ctx, stream.ID)
}

func TestGetStreamBySessionAndType(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create active desktop stream
	desktopStream := &store.Stream{
		SessionID:    session.ID,
		Type:         "desktop",
		State:        "active",
		ProviderName: "webrtc-sfu",
	}
	err := testStore.CreateStream(ctx, desktopStream)
	require.NoError(t, err)

	// Create stopped browser stream
	browserStream := &store.Stream{
		SessionID:    session.ID,
		Type:         "browser",
		State:        "stopped",
		ProviderName: "webrtc-sfu",
	}
	err = testStore.CreateStream(ctx, browserStream)
	require.NoError(t, err)

	// Get active desktop stream
	got, err := testStore.GetStreamBySessionAndType(ctx, session.ID, "desktop", true)
	require.NoError(t, err)
	assert.Equal(t, desktopStream.ID, got.ID)

	// Get browser stream (activeOnly=true should not find stopped stream)
	_, err = testStore.GetStreamBySessionAndType(ctx, session.ID, "browser", true)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Get browser stream (activeOnly=false should find it)
	got, err = testStore.GetStreamBySessionAndType(ctx, session.ID, "browser", false)
	require.NoError(t, err)
	assert.Equal(t, browserStream.ID, got.ID)

	// Cleanup
	_ = testStore.DeleteStream(ctx, desktopStream.ID)
	_ = testStore.DeleteStream(ctx, browserStream.ID)
}

func TestListStreams(t *testing.T) {
	ctx := context.Background()

	// Create two sessions
	session1 := createTestSessionForStream(t, ctx)
	session2 := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session1.ID)
		_ = testStore.DeleteWorkspace(ctx, session1.WorkspaceID)
		_ = testStore.DeleteSession(ctx, session2.ID)
		_ = testStore.DeleteWorkspace(ctx, session2.WorkspaceID)
	}()

	// Create multiple streams
	streams := []*store.Stream{
		{SessionID: session1.ID, Type: "desktop", State: "active", ProviderName: "sfu"},
		{SessionID: session1.ID, Type: "browser", State: "pending", ProviderName: "sfu"},
		{SessionID: session2.ID, Type: "android", State: "stopped", ProviderName: "sfu"},
		{SessionID: session2.ID, Type: "ios", State: "error", ProviderName: "sfu"},
	}

	for _, s := range streams {
		err := testStore.CreateStream(ctx, s)
		require.NoError(t, err)
	}

	// List all
	list, err := testStore.ListStreams(ctx, store.ListStreamsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 4)

	// List by session
	list, err = testStore.ListStreams(ctx, store.ListStreamsOptions{
		SessionID: &session1.ID,
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 2)
	for _, s := range list.Items {
		assert.Equal(t, session1.ID, s.SessionID)
	}

	// List by type
	list, err = testStore.ListStreams(ctx, store.ListStreamsOptions{
		Type: []string{"desktop", "browser"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 2)

	// List by state
	list, err = testStore.ListStreams(ctx, store.ListStreamsOptions{
		State: []string{"active"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)
	for _, s := range list.Items {
		assert.Equal(t, "active", s.State)
	}

	// List active only
	list, err = testStore.ListStreams(ctx, store.ListStreamsOptions{
		ActiveOnly: true,
	})
	require.NoError(t, err)
	for _, s := range list.Items {
		assert.NotContains(t, []string{"stopped", "error"}, s.State)
	}

	// Test pagination
	list, err = testStore.ListStreams(ctx, store.ListStreamsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:     2,
			OrderDesc: true,
		},
	})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(list.Items), 2)

	// Cleanup
	for _, s := range streams {
		_ = testStore.DeleteStream(ctx, s.ID)
	}
}

func TestCleanupExpiredStreams(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create an expired stream
	pastTime := time.Now().Add(-1 * time.Hour)
	expiredStream := &store.Stream{
		SessionID:    session.ID,
		Type:         "desktop",
		State:        "active",
		ProviderName: "sfu",
		ExpiresAt:    &pastTime,
	}
	err := testStore.CreateStream(ctx, expiredStream)
	require.NoError(t, err)

	// Create a non-expired stream
	futureTime := time.Now().Add(1 * time.Hour)
	activeStream := &store.Stream{
		SessionID:    session.ID,
		Type:         "browser",
		State:        "active",
		ProviderName: "sfu",
		ExpiresAt:    &futureTime,
	}
	err = testStore.CreateStream(ctx, activeStream)
	require.NoError(t, err)

	// Create a stream without expiration
	noExpireStream := &store.Stream{
		SessionID:    session.ID,
		Type:         "android",
		State:        "active",
		ProviderName: "sfu",
	}
	err = testStore.CreateStream(ctx, noExpireStream)
	require.NoError(t, err)

	// Cleanup expired streams
	count, err := testStore.CleanupExpiredStreams(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1)

	// Verify expired stream is now stopped
	got, err := testStore.GetStream(ctx, expiredStream.ID)
	require.NoError(t, err)
	assert.Equal(t, "stopped", got.State)
	assert.NotNil(t, got.StoppedAt)

	// Verify active stream is still active
	got, err = testStore.GetStream(ctx, activeStream.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.State)

	// Verify no-expire stream is still active
	got, err = testStore.GetStream(ctx, noExpireStream.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", got.State)

	// Cleanup
	_ = testStore.DeleteStream(ctx, expiredStream.ID)
	_ = testStore.DeleteStream(ctx, activeStream.ID)
	_ = testStore.DeleteStream(ctx, noExpireStream.ID)
}

func TestStreamWithRunner(t *testing.T) {
	ctx := context.Background()

	// Create a runner
	runner := &store.Runner{
		Name:         "test-runner-stream-" + time.Now().Format("150405.000"),
		Hostname:     "localhost",
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
		_ = testStore.DeleteRunner(ctx, runner.ID)
	}()

	// Create stream with runner
	stream := &store.Stream{
		SessionID:    session.ID,
		RunnerID:     &runner.ID,
		Type:         "desktop",
		State:        "active",
		ProviderName: "sfu",
	}
	err = testStore.CreateStream(ctx, stream)
	require.NoError(t, err)

	// Verify runner is set
	got, err := testStore.GetStream(ctx, stream.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.RunnerID)
	assert.Equal(t, runner.ID, *got.RunnerID)

	// List by runner
	list, err := testStore.ListStreams(ctx, store.ListStreamsOptions{
		RunnerID: &runner.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Cleanup
	_ = testStore.DeleteStream(ctx, stream.ID)
}

func TestStreamTransaction(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	t.Run("commit", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		stream := &store.Stream{
			SessionID:    session.ID,
			Type:         "desktop",
			State:        "pending",
			ProviderName: "sfu",
		}
		err = tx.CreateStream(ctx, stream)
		require.NoError(t, err)

		err = tx.Commit(ctx)
		require.NoError(t, err)

		// Verify stream exists
		got, err := testStore.GetStream(ctx, stream.ID)
		require.NoError(t, err)
		assert.Equal(t, stream.SessionID, got.SessionID)

		// Cleanup
		_ = testStore.DeleteStream(ctx, stream.ID)
	})

	t.Run("rollback", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)

		stream := &store.Stream{
			SessionID:    session.ID,
			Type:         "browser",
			State:        "pending",
			ProviderName: "sfu",
		}
		err = tx.CreateStream(ctx, stream)
		require.NoError(t, err)

		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Verify stream does not exist
		_, err = testStore.GetStream(ctx, stream.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("tx_operations", func(t *testing.T) {
		tx, err := testStore.BeginTx(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()

		// Create stream via tx
		stream := &store.Stream{
			SessionID:    session.ID,
			Type:         "ios",
			State:        "pending",
			ProviderName: "sfu",
		}
		err = tx.CreateStream(ctx, stream)
		require.NoError(t, err)

		// Get stream via tx
		got, err := tx.GetStream(ctx, stream.ID)
		require.NoError(t, err)
		assert.Equal(t, stream.ID, got.ID)

		// GetStreamBySessionAndType via tx
		got, err = tx.GetStreamBySessionAndType(ctx, session.ID, "ios", true)
		require.NoError(t, err)
		assert.Equal(t, stream.ID, got.ID)

		// ListStreams via tx
		list, err := tx.ListStreams(ctx, store.ListStreamsOptions{
			SessionID: &session.ID,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list.Items), 1)

		// UpdateStream via tx
		newState := "active"
		err = tx.UpdateStream(ctx, stream.ID, store.StreamUpdates{
			State: &newState,
		})
		require.NoError(t, err)

		// CleanupExpiredStreams via tx
		_, err = tx.CleanupExpiredStreams(ctx)
		require.NoError(t, err)

		// DeleteStream via tx
		err = tx.DeleteStream(ctx, stream.ID)
		require.NoError(t, err)
	})
}

func TestStreamAllTypes(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Test all stream types
	types := []string{"desktop", "browser", "ios", "android"}
	var createdStreams []*store.Stream

	for _, streamType := range types {
		stream := &store.Stream{
			SessionID:    session.ID,
			Type:         streamType,
			State:        "pending",
			ProviderName: "sfu",
		}
		err := testStore.CreateStream(ctx, stream)
		require.NoError(t, err)
		createdStreams = append(createdStreams, stream)

		got, err := testStore.GetStream(ctx, stream.ID)
		require.NoError(t, err)
		assert.Equal(t, streamType, got.Type)
	}

	// Cleanup
	for _, s := range createdStreams {
		_ = testStore.DeleteStream(ctx, s.ID)
	}
}

func TestStreamAllStates(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create a stream
	stream := &store.Stream{
		SessionID:    session.ID,
		Type:         "desktop",
		State:        "pending",
		ProviderName: "sfu",
	}
	err := testStore.CreateStream(ctx, stream)
	require.NoError(t, err)

	// Test all state transitions
	states := []string{"starting", "active", "paused", "stopping", "stopped", "error"}

	for _, state := range states {
		err = testStore.UpdateStream(ctx, stream.ID, store.StreamUpdates{
			State: &state,
		})
		require.NoError(t, err)

		got, err := testStore.GetStream(ctx, stream.ID)
		require.NoError(t, err)
		assert.Equal(t, state, got.State)
	}

	// Cleanup
	_ = testStore.DeleteStream(ctx, stream.ID)
}

func TestStreamMetadataAndICEServers(t *testing.T) {
	ctx := context.Background()

	// Create a session
	session := createTestSessionForStream(t, ctx)
	defer func() {
		_ = testStore.DeleteSession(ctx, session.ID)
		_ = testStore.DeleteWorkspace(ctx, session.WorkspaceID)
	}()

	// Create stream with complex ICE servers and metadata
	iceServers, _ := json.Marshal([]map[string]any{
		{
			"urls":       []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"},
			"username":   "",
			"credential": "",
		},
		{
			"urls":       []string{"turn:turn.example.com:3478"},
			"username":   "user",
			"credential": "pass",
		},
	})

	metadata, _ := json.Marshal(map[string]any{
		"device":     "MacBook Pro",
		"resolution": "1920x1080",
		"nested": map[string]string{
			"key": "value",
		},
	})

	stream := &store.Stream{
		SessionID:    session.ID,
		Type:         "desktop",
		State:        "pending",
		ICEServers:   iceServers,
		ProviderName: "sfu",
		Metadata:     metadata,
	}
	err := testStore.CreateStream(ctx, stream)
	require.NoError(t, err)

	// Verify
	got, err := testStore.GetStream(ctx, stream.ID)
	require.NoError(t, err)

	var gotICE []map[string]any
	err = json.Unmarshal(got.ICEServers, &gotICE)
	require.NoError(t, err)
	assert.Len(t, gotICE, 2)

	var gotMeta map[string]any
	err = json.Unmarshal(got.Metadata, &gotMeta)
	require.NoError(t, err)
	assert.Equal(t, "MacBook Pro", gotMeta["device"])

	// Update metadata
	newMetadata, _ := json.Marshal(map[string]string{"updated": "true"})
	err = testStore.UpdateStream(ctx, stream.ID, store.StreamUpdates{
		Metadata: newMetadata,
	})
	require.NoError(t, err)

	got, err = testStore.GetStream(ctx, stream.ID)
	require.NoError(t, err)

	err = json.Unmarshal(got.Metadata, &gotMeta)
	require.NoError(t, err)
	assert.Equal(t, "true", gotMeta["updated"])

	// Cleanup
	_ = testStore.DeleteStream(ctx, stream.ID)
}

package browser

import (
	"context"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestBrowserStreamProvider_Name(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	assert.Equal(t, ProviderName, provider.Name())
}

func TestBrowserStreamProvider_SupportedTypes(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	types := provider.SupportedTypes()
	require.Len(t, types, 1)
	assert.Equal(t, streaming.StreamTypeBrowser, types[0])
}

func TestBrowserStreamProvider_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
		Logger:  logger,
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		TenantID:  "tenant_test",
		Type:      streaming.StreamTypeBrowser,
		Resolution: streaming.Resolution{
			Width:  1920,
			Height: 1080,
		},
		FrameRate: 30,
	}

	info, err := provider.Start(ctx, opts)
	require.NoError(t, err)
	require.NotNil(t, info)

	// Verify stream info
	assert.NotEmpty(t, info.ID)
	assert.Contains(t, info.SignalingURL, "/api/v1/streams/")
	assert.Contains(t, info.SignalingURL, "/ws")
	assert.Equal(t, 30, info.FrameRate)
	assert.Equal(t, "jpeg", info.VideoCodec)

	// Verify stream was created
	streams := provider.ListStreams()
	assert.Len(t, streams, 1)
	assert.Contains(t, streams, info.ID)
}

func TestBrowserStreamProvider_Start_DefaultFrameRate(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		Type:      streaming.StreamTypeBrowser,
		// No frame rate specified
	}

	info, err := provider.Start(ctx, opts)
	require.NoError(t, err)

	// Should use default frame rate
	assert.Equal(t, DefaultMaxFPS, info.FrameRate)
}

func TestBrowserStreamProvider_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
		Logger:  logger,
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		Type:      streaming.StreamTypeBrowser,
	}

	// Start a stream
	info, err := provider.Start(ctx, opts)
	require.NoError(t, err)

	// Verify stream exists
	assert.Len(t, provider.ListStreams(), 1)

	// Stop the stream
	err = provider.Stop(ctx, info.ID)
	require.NoError(t, err)

	// Verify stream was removed
	assert.Len(t, provider.ListStreams(), 0)
}

func TestBrowserStreamProvider_Stop_NotFound(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()
	err := provider.Stop(ctx, "nonexistent")
	assert.ErrorIs(t, err, streaming.ErrStreamNotFound)
}

func TestBrowserStreamProvider_GetInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
		Logger:  logger,
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		Type:      streaming.StreamTypeBrowser,
		Resolution: streaming.Resolution{
			Width:  1280,
			Height: 720,
		},
		FrameRate: 25,
	}

	// Start a stream
	startInfo, err := provider.Start(ctx, opts)
	require.NoError(t, err)

	// Get info
	info, err := provider.GetInfo(ctx, startInfo.ID)
	require.NoError(t, err)

	assert.Equal(t, startInfo.ID, info.ID)
	assert.Contains(t, info.SignalingURL, startInfo.ID)
	assert.Equal(t, 25, info.FrameRate)
}

func TestBrowserStreamProvider_GetInfo_NotFound(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()
	_, err := provider.GetInfo(ctx, "nonexistent")
	assert.ErrorIs(t, err, streaming.ErrStreamNotFound)
}

func TestBrowserStreamProvider_SetStreamState(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		Type:      streaming.StreamTypeBrowser,
	}

	// Start a stream
	info, err := provider.Start(ctx, opts)
	require.NoError(t, err)

	// Update state
	err = provider.SetStreamState(info.ID, streaming.StreamStateActive, "")
	require.NoError(t, err)

	// Verify state
	state, err := provider.GetStreamState(info.ID)
	require.NoError(t, err)
	assert.Equal(t, streaming.StreamStateActive, state)

	// Update to error state
	err = provider.SetStreamState(info.ID, streaming.StreamStateError, "connection lost")
	require.NoError(t, err)

	state, err = provider.GetStreamState(info.ID)
	require.NoError(t, err)
	assert.Equal(t, streaming.StreamStateError, state)
}

func TestBrowserStreamProvider_SetStreamState_NotFound(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	err := provider.SetStreamState("nonexistent", streaming.StreamStateActive, "")
	assert.ErrorIs(t, err, streaming.ErrStreamNotFound)
}

func TestBrowserStreamProvider_ValidateStream(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()
	opts := streaming.StreamOptions{
		SessionID: "sess_test123",
		RunnerID:  "run_test456",
		Type:      streaming.StreamTypeBrowser,
	}

	// Start a stream
	info, err := provider.Start(ctx, opts)
	require.NoError(t, err)

	// Valid validation
	err = provider.ValidateStream(info.ID, "run_test456", "sess_test123")
	require.NoError(t, err)

	// Invalid runner ID
	err = provider.ValidateStream(info.ID, "run_wrong", "sess_test123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "runner ID mismatch")

	// Invalid session ID
	err = provider.ValidateStream(info.ID, "run_test456", "sess_wrong")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session ID mismatch")
}

func TestBrowserStreamProvider_ValidateStream_NotFound(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	err := provider.ValidateStream("nonexistent", "run_test", "sess_test")
	assert.ErrorIs(t, err, streaming.ErrStreamNotFound)
}

func TestBrowserStreamProvider_FrameHub(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	hub := provider.FrameHub()
	require.NotNil(t, hub)

	// Verify it's the same instance
	assert.Same(t, hub, provider.FrameHub())
}

func TestBrowserStreamProvider_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
		Logger:  logger,
	})

	ctx := context.Background()

	// Start multiple streams
	for i := 0; i < 3; i++ {
		opts := streaming.StreamOptions{
			SessionID: "sess_test123",
			RunnerID:  "run_test456",
			Type:      streaming.StreamTypeBrowser,
		}
		_, err := provider.Start(ctx, opts)
		require.NoError(t, err)
	}

	assert.Len(t, provider.ListStreams(), 3)

	// Close provider
	err := provider.Close()
	require.NoError(t, err)

	// Verify all streams are cleared
	assert.Len(t, provider.ListStreams(), 0)
}

func TestBrowserStreamProvider_MultipleStreams(t *testing.T) {
	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	ctx := context.Background()

	var streamIDs []string

	// Start multiple streams for different sessions
	for i := 0; i < 5; i++ {
		opts := streaming.StreamOptions{
			SessionID: "sess_test" + string(rune('A'+i)),
			RunnerID:  "run_test" + string(rune('0'+i)),
			Type:      streaming.StreamTypeBrowser,
		}
		info, err := provider.Start(ctx, opts)
		require.NoError(t, err)
		streamIDs = append(streamIDs, info.ID)
	}

	// Verify all streams exist
	assert.Len(t, provider.ListStreams(), 5)

	// Stop some streams
	require.NoError(t, provider.Stop(ctx, streamIDs[0]))
	require.NoError(t, provider.Stop(ctx, streamIDs[2]))

	// Verify remaining streams
	assert.Len(t, provider.ListStreams(), 3)
}

func TestBrowserStreamProvider_ImplementsInterface(t *testing.T) {
	// Compile-time check that BrowserStreamProvider implements streaming.StreamProvider
	var _ streaming.StreamProvider = (*BrowserStreamProvider)(nil)

	provider := NewBrowserStreamProvider(BrowserStreamProviderConfig{
		BaseURL: "ws://localhost:8080",
	})

	// Runtime check
	_, ok := interface{}(provider).(streaming.StreamProvider)
	assert.True(t, ok)
}

func TestFrameHub_RegisterUnregister(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewFrameHub(logger)

	// Register a stream
	inputCh, err := hub.RegisterStream("stream1", "runner1", "session1", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, inputCh)

	// Verify stream exists
	conn := hub.GetStream("stream1")
	require.NotNil(t, conn)
	assert.Equal(t, "stream1", conn.StreamID)
	assert.Equal(t, "runner1", conn.RunnerID)
	assert.True(t, conn.Connected)

	// Unregister
	hub.UnregisterStream("stream1")

	// Verify stream is gone
	conn = hub.GetStream("stream1")
	assert.Nil(t, conn)
}

func TestFrameHub_Subscribe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewFrameHub(logger)

	// Register a stream
	_, err := hub.RegisterStream("stream1", "runner1", "session1", nil, nil)
	require.NoError(t, err)

	// Subscribe
	sub := &FrameSubscriber{
		ID:        "sub1",
		StreamID:  "stream1",
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}
	hub.Subscribe(sub)

	// Verify subscriber count
	assert.Equal(t, 1, hub.GetSubscriberCount("stream1"))

	// Add another subscriber
	sub2 := &FrameSubscriber{
		ID:        "sub2",
		StreamID:  "stream1",
		FrameCh:   make(chan *pb.BrowserFrame, 10),
		Done:      make(chan struct{}),
		CreatedAt: time.Now(),
	}
	hub.Subscribe(sub2)

	assert.Equal(t, 2, hub.GetSubscriberCount("stream1"))

	// Unsubscribe
	hub.Unsubscribe(sub)
	assert.Equal(t, 1, hub.GetSubscriberCount("stream1"))

	hub.Unsubscribe(sub2)
	assert.Equal(t, 0, hub.GetSubscriberCount("stream1"))
}

func TestFrameHub_ListActiveStreams(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewFrameHub(logger)

	// Register multiple streams
	_, _ = hub.RegisterStream("stream1", "runner1", "session1", nil, nil)
	_, _ = hub.RegisterStream("stream2", "runner2", "session2", nil, nil)
	_, _ = hub.RegisterStream("stream3", "runner3", "session3", nil, nil)

	streams := hub.ListActiveStreams()
	assert.Len(t, streams, 3)
	assert.Contains(t, streams, "stream1")
	assert.Contains(t, streams, "stream2")
	assert.Contains(t, streams, "stream3")

	// Unregister one
	hub.UnregisterStream("stream2")

	streams = hub.ListActiveStreams()
	assert.Len(t, streams, 2)
	assert.NotContains(t, streams, "stream2")
}

func TestFrameHub_GetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewFrameHub(logger)

	// Register a stream
	_, _ = hub.RegisterStream("stream1", "runner1", "session1", nil, nil)

	stats := hub.GetStats("stream1")
	require.NotNil(t, stats)
	assert.Equal(t, "stream1", stats.StreamID)
	assert.True(t, stats.StreamConnected)
	assert.Equal(t, uint64(0), stats.FramesReceived)

	// Stats for non-existent stream
	stats = hub.GetStats("nonexistent")
	assert.Equal(t, "nonexistent", stats.StreamID)
	assert.False(t, stats.StreamConnected)
}

func TestFrameHub_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	hub := NewFrameHub(logger)

	// Register streams
	_, _ = hub.RegisterStream("stream1", "runner1", "session1", nil, nil)
	_, _ = hub.RegisterStream("stream2", "runner2", "session2", nil, nil)

	// Close
	hub.Close()

	// Verify all streams are cleared
	streams := hub.ListActiveStreams()
	assert.Len(t, streams, 0)
}

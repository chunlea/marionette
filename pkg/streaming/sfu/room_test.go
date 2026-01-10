package sfu

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// createTestSFU creates an SFU for testing.
func createTestSFU(t *testing.T) *SFU {
	t.Helper()

	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		sfu.Close(context.Background())
	})

	return sfu
}

// createTestRoom creates a Room for testing.
func createTestRoom(t *testing.T) *Room {
	t.Helper()

	sfu := createTestSFU(t)
	room, err := sfu.CreateRoom("test-stream")
	require.NoError(t, err)

	return room
}

func TestNewRoom(t *testing.T) {
	sfu := createTestSFU(t)
	logger := zaptest.NewLogger(t)

	room := newRoom("test-stream", sfu, logger)

	assert.NotNil(t, room)
	assert.Equal(t, "test-stream", room.StreamID())
	assert.False(t, room.IsClosed())
	assert.False(t, room.HasPublisher())
	assert.Equal(t, 0, room.SubscriberCount())
}

func TestRoom_StreamID(t *testing.T) {
	room := createTestRoom(t)

	assert.Equal(t, "test-stream", room.StreamID())
}

func TestRoom_HasPublisher_False(t *testing.T) {
	room := createTestRoom(t)

	assert.False(t, room.HasPublisher())
}

func TestRoom_GetPublisher_Nil(t *testing.T) {
	room := createTestRoom(t)

	pub := room.GetPublisher()
	assert.Nil(t, pub)
}

func TestRoom_SubscriberCount_Empty(t *testing.T) {
	room := createTestRoom(t)

	assert.Equal(t, 0, room.SubscriberCount())
}

func TestRoom_SubscriberIDs_Empty(t *testing.T) {
	room := createTestRoom(t)

	ids := room.SubscriberIDs()
	assert.Empty(t, ids)
}

func TestRoom_GetSubscriber_NotFound(t *testing.T) {
	room := createTestRoom(t)

	peer, ok := room.GetSubscriber("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, peer)
}

func TestRoom_IsClosed_False(t *testing.T) {
	room := createTestRoom(t)

	assert.False(t, room.IsClosed())
}

func TestRoom_Close(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	err := room.Close(ctx)
	require.NoError(t, err)

	assert.True(t, room.IsClosed())
}

func TestRoom_Close_Idempotent(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	// Close multiple times should not error
	err := room.Close(ctx)
	require.NoError(t, err)

	err = room.Close(ctx)
	require.NoError(t, err)

	err = room.Close(ctx)
	require.NoError(t, err)

	assert.True(t, room.IsClosed())
}

func TestRoom_Stats_Empty(t *testing.T) {
	room := createTestRoom(t)

	stats := room.Stats()

	assert.Equal(t, "test-stream", stats.StreamID)
	assert.False(t, stats.HasPublisher)
	assert.Equal(t, 0, stats.SubscriberCount)
	assert.Equal(t, 0, stats.TrackCount)
}

func TestRoomStats_Struct(t *testing.T) {
	stats := RoomStats{
		StreamID:        "stream-123",
		HasPublisher:    true,
		SubscriberCount: 5,
		TrackCount:      2,
	}

	assert.Equal(t, "stream-123", stats.StreamID)
	assert.True(t, stats.HasPublisher)
	assert.Equal(t, 5, stats.SubscriberCount)
	assert.Equal(t, 2, stats.TrackCount)
}

func TestRoom_GetTrackInfo_Empty(t *testing.T) {
	room := createTestRoom(t)

	infos := room.GetTrackInfo()
	assert.Empty(t, infos)
}

func TestRoom_SetPublisher(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	peer, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)
	require.NotNil(t, peer)

	assert.Equal(t, "publisher-1", peer.ID)
	assert.Equal(t, PeerRolePublisher, peer.Role)
	assert.True(t, room.HasPublisher())
}

func TestRoom_SetPublisher_AlreadyExists(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)

	// Try to set another publisher
	_, err = room.SetPublisher(ctx, "publisher-2")
	assert.Error(t, err)
}

func TestRoom_SetPublisher_WhenClosed(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	err := room.Close(ctx)
	require.NoError(t, err)

	_, err = room.SetPublisher(ctx, "publisher-1")
	assert.Error(t, err)
}

func TestRoom_GetPublisher_AfterSet(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	peer, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)

	gotPeer := room.GetPublisher()
	assert.Equal(t, peer, gotPeer)
}

func TestRoom_AddSubscriber(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	peer, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)
	require.NotNil(t, peer)

	assert.Equal(t, "subscriber-1", peer.ID)
	assert.Equal(t, PeerRoleSubscriber, peer.Role)
	assert.Equal(t, 1, room.SubscriberCount())
}

func TestRoom_AddSubscriber_Multiple(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-2")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-3")
	require.NoError(t, err)

	assert.Equal(t, 3, room.SubscriberCount())
}

func TestRoom_AddSubscriber_Duplicate(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	// Try to add same subscriber again
	_, err = room.AddSubscriber(ctx, "subscriber-1")
	assert.Error(t, err)
}

func TestRoom_AddSubscriber_WhenClosed(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	err := room.Close(ctx)
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-1")
	assert.Error(t, err)
}

func TestRoom_GetSubscriber(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	peer, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	gotPeer, ok := room.GetSubscriber("subscriber-1")
	assert.True(t, ok)
	assert.Equal(t, peer, gotPeer)
}

func TestRoom_RemoveSubscriber(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)
	assert.Equal(t, 1, room.SubscriberCount())

	err = room.RemoveSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)
	assert.Equal(t, 0, room.SubscriberCount())
}

func TestRoom_RemoveSubscriber_NotFound(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	err := room.RemoveSubscriber(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestRoom_SubscriberIDs(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.AddSubscriber(ctx, "subscriber-a")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-b")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-c")
	require.NoError(t, err)

	ids := room.SubscriberIDs()
	assert.Len(t, ids, 3)
	assert.Contains(t, ids, "subscriber-a")
	assert.Contains(t, ids, "subscriber-b")
	assert.Contains(t, ids, "subscriber-c")
}

func TestRoom_Stats_WithPublisher(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	_, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)

	stats := room.Stats()
	assert.True(t, stats.HasPublisher)
}

func TestRoom_Stats_WithSubscribers(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-2")
	require.NoError(t, err)

	stats := room.Stats()
	assert.Equal(t, 2, stats.SubscriberCount)
}

func TestRoom_Close_WithPeers(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	_, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	_, err = room.AddSubscriber(ctx, "subscriber-2")
	require.NoError(t, err)

	err = room.Close(ctx)
	require.NoError(t, err)

	assert.True(t, room.IsClosed())
}

func TestRoom_ConcurrentOperations(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = room.StreamID()
			_ = room.HasPublisher()
			_ = room.SubscriberCount()
			_ = room.Stats()
			_ = room.IsClosed()
		}()
	}

	// Concurrent subscriber operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			peerID := "concurrent-" + string(rune('a'+i))
			_, _ = room.AddSubscriber(ctx, peerID)
		}(i)
	}

	wg.Wait()
}

func TestRoom_Router(t *testing.T) {
	room := createTestRoom(t)

	// Router should be initialized
	room.mu.RLock()
	assert.NotNil(t, room.router)
	room.mu.RUnlock()
}

func TestRoom_InputChannel(t *testing.T) {
	room := createTestRoom(t)

	// InputChannel should be initialized
	room.mu.RLock()
	assert.NotNil(t, room.inputChannel)
	room.mu.RUnlock()
}

func TestRoom_ContextTimeout(t *testing.T) {
	room := createTestRoom(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := room.SetPublisher(ctx, "publisher-1")
	require.NoError(t, err)
}

func TestRoom_HandlePublisherStateChange(t *testing.T) {
	room := createTestRoom(t)

	// Call internal handler directly
	room.handlePublisherStateChange(PeerStateConnected)
	room.handlePublisherStateChange(PeerStateDisconnected)

	// Should not panic
}

func TestRoom_HandleSubscriberStateChange(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	// Call internal handler
	room.handleSubscriberStateChange("subscriber-1", PeerStateConnected)

	// Should not panic
}

func TestRoom_HandleSubscriberStateChange_Disconnected(t *testing.T) {
	room := createTestRoom(t)

	ctx := context.Background()
	_, err := room.AddSubscriber(ctx, "subscriber-1")
	require.NoError(t, err)

	// This will trigger auto-cleanup in a goroutine
	room.handleSubscriberStateChange("subscriber-1", PeerStateDisconnected)

	// Give goroutine time to run
	time.Sleep(50 * time.Millisecond)

	// Subscriber should be removed
	assert.Equal(t, 0, room.SubscriberCount())
}

func BenchmarkRoom_Stats(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer sfu.Close(context.Background())

	room, _ := sfu.CreateRoom("bench-stream")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = room.Stats()
	}
}

func BenchmarkRoom_SubscriberCount(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer sfu.Close(context.Background())

	room, _ := sfu.CreateRoom("bench-stream")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = room.SubscriberCount()
	}
}

func BenchmarkRoom_HasPublisher(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer sfu.Close(context.Background())

	room, _ := sfu.CreateRoom("bench-stream")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = room.HasPublisher()
	}
}

func BenchmarkRoom_AddSubscriber(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer sfu.Close(context.Background())

	room, _ := sfu.CreateRoom("bench-stream")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		peerID := "subscriber-" + string(rune(i%26+'a'))
		room.AddSubscriber(ctx, peerID)
	}
}

func TestRoom_Logger(t *testing.T) {
	room := createTestRoom(t)

	room.mu.RLock()
	assert.NotNil(t, room.logger)
	room.mu.RUnlock()
}

func TestRoom_SFU(t *testing.T) {
	room := createTestRoom(t)

	room.mu.RLock()
	assert.NotNil(t, room.sfu)
	room.mu.RUnlock()
}

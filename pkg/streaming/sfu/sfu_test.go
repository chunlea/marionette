package sfu

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNew(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, sfu)

	defer func() { _ = sfu.Close(context.Background()) }()

	assert.Equal(t, 0, sfu.RoomCount())
}

func TestNew_NilLogger(t *testing.T) {
	cfg := DefaultConfig()

	sfu, err := New(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, sfu)

	defer func() { _ = sfu.Close(context.Background()) }()
}

func TestNew_WithICEServers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ICEServers = []webrtc.ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
	}
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, sfu)

	defer func() { _ = sfu.Close(context.Background()) }()
}

func TestSFU_CreateRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	room, err := sfu.CreateRoom("stream-1")
	require.NoError(t, err)
	require.NotNil(t, room)

	assert.Equal(t, "stream-1", room.StreamID())
	assert.Equal(t, 1, sfu.RoomCount())
}

func TestSFU_CreateRoom_Duplicate(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	_, err = sfu.CreateRoom("stream-1")
	require.NoError(t, err)

	_, err = sfu.CreateRoom("stream-1")
	assert.Error(t, err)
}

func TestSFU_GetRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	_, err = sfu.CreateRoom("stream-1")
	require.NoError(t, err)

	room, ok := sfu.GetRoom("stream-1")
	assert.True(t, ok)
	assert.NotNil(t, room)
	assert.Equal(t, "stream-1", room.StreamID())
}

func TestSFU_GetRoom_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	room, ok := sfu.GetRoom("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, room)
}

func TestSFU_GetOrCreateRoom_New(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	room, err := sfu.GetOrCreateRoom("stream-1")
	require.NoError(t, err)
	require.NotNil(t, room)

	assert.Equal(t, "stream-1", room.StreamID())
	assert.Equal(t, 1, sfu.RoomCount())
}

func TestSFU_GetOrCreateRoom_Existing(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	room1, err := sfu.GetOrCreateRoom("stream-1")
	require.NoError(t, err)

	room2, err := sfu.GetOrCreateRoom("stream-1")
	require.NoError(t, err)

	assert.Equal(t, room1, room2)
	assert.Equal(t, 1, sfu.RoomCount())
}

func TestSFU_RemoveRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	_, err = sfu.CreateRoom("stream-1")
	require.NoError(t, err)
	assert.Equal(t, 1, sfu.RoomCount())

	ctx := context.Background()
	err = sfu.RemoveRoom(ctx, "stream-1")
	require.NoError(t, err)

	assert.Equal(t, 0, sfu.RoomCount())
}

func TestSFU_RemoveRoom_NotFound(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	ctx := context.Background()
	err = sfu.RemoveRoom(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestSFU_ListRooms(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	_, err = sfu.CreateRoom("stream-a")
	require.NoError(t, err)

	_, err = sfu.CreateRoom("stream-b")
	require.NoError(t, err)

	_, err = sfu.CreateRoom("stream-c")
	require.NoError(t, err)

	rooms := sfu.ListRooms()
	assert.Len(t, rooms, 3)
	assert.Contains(t, rooms, "stream-a")
	assert.Contains(t, rooms, "stream-b")
	assert.Contains(t, rooms, "stream-c")
}

func TestSFU_ListRooms_Empty(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	rooms := sfu.ListRooms()
	assert.Empty(t, rooms)
}

func TestSFU_RoomCount(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	assert.Equal(t, 0, sfu.RoomCount())

	_, _ = sfu.CreateRoom("stream-1")
	assert.Equal(t, 1, sfu.RoomCount())

	_, _ = sfu.CreateRoom("stream-2")
	assert.Equal(t, 2, sfu.RoomCount())

	ctx := context.Background()
	_ = sfu.RemoveRoom(ctx, "stream-1")
	assert.Equal(t, 1, sfu.RoomCount())
}

func TestSFU_Close(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)

	_, _ = sfu.CreateRoom("stream-1")
	_, _ = sfu.CreateRoom("stream-2")

	ctx := context.Background()
	err = sfu.Close(ctx)
	require.NoError(t, err)

	assert.Equal(t, 0, sfu.RoomCount())
}

func TestSFU_Close_Empty(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = sfu.Close(ctx)
	require.NoError(t, err)
}

func TestSFU_GetStats(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	stats := sfu.GetStats()
	assert.Equal(t, 0, stats.RoomCount)
	assert.Equal(t, 0, stats.TotalPublishers)
	assert.Equal(t, 0, stats.TotalSubscribers)
}

func TestSFU_GetStats_WithRooms(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	room, _ := sfu.CreateRoom("stream-1")

	ctx := context.Background()
	_, _ = room.SetPublisher(ctx, "publisher-1")
	_, _ = room.AddSubscriber(ctx, "subscriber-1")
	_, _ = room.AddSubscriber(ctx, "subscriber-2")

	stats := sfu.GetStats()
	assert.Equal(t, 1, stats.RoomCount)
	assert.Equal(t, 1, stats.TotalPublishers)
	assert.Equal(t, 2, stats.TotalSubscribers)
}

func TestSFU_GetStats_MultipleRooms(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	ctx := context.Background()

	room1, _ := sfu.CreateRoom("stream-1")
	_, _ = room1.SetPublisher(ctx, "publisher-1")
	_, _ = room1.AddSubscriber(ctx, "subscriber-1")

	room2, _ := sfu.CreateRoom("stream-2")
	_, _ = room2.SetPublisher(ctx, "publisher-2")
	_, _ = room2.AddSubscriber(ctx, "subscriber-2")
	_, _ = room2.AddSubscriber(ctx, "subscriber-3")

	stats := sfu.GetStats()
	assert.Equal(t, 2, stats.RoomCount)
	assert.Equal(t, 2, stats.TotalPublishers)
	assert.Equal(t, 3, stats.TotalSubscribers)
}

func TestStats_Struct(t *testing.T) {
	stats := Stats{
		RoomCount:        5,
		TotalPublishers:  5,
		TotalSubscribers: 15,
	}

	assert.Equal(t, 5, stats.RoomCount)
	assert.Equal(t, 5, stats.TotalPublishers)
	assert.Equal(t, 15, stats.TotalSubscribers)
}

func TestSFU_Config(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PLIInterval = 2000
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	gotCfg := sfu.Config()
	assert.Equal(t, uint16(2000), gotCfg.PLIInterval)
}

func TestSFU_ConcurrentCreateRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	var wg sync.WaitGroup

	// Create 10 rooms concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			streamID := "stream-" + string(rune('a'+i))
			_, _ = sfu.CreateRoom(streamID)
		}(i)
	}

	wg.Wait()

	// Should have created all rooms (or some failed due to duplicates)
	assert.GreaterOrEqual(t, sfu.RoomCount(), 1)
	assert.LessOrEqual(t, sfu.RoomCount(), 10)
}

func TestSFU_ConcurrentGetRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	_, _ = sfu.CreateRoom("stream-1")

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sfu.GetRoom("stream-1")
		}()
	}

	wg.Wait()
}

func TestSFU_ConcurrentGetOrCreateRoom(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	var wg sync.WaitGroup
	var rooms []*Room
	var mu sync.Mutex

	// Concurrent GetOrCreate for same room
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			room, err := sfu.GetOrCreateRoom("stream-1")
			if err == nil {
				mu.Lock()
				rooms = append(rooms, room)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// All should get the same room
	assert.Len(t, rooms, 10)
	for _, room := range rooms {
		assert.Equal(t, rooms[0], room)
	}
	assert.Equal(t, 1, sfu.RoomCount())
}

func TestSFU_ConcurrentMixedOperations(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	var wg sync.WaitGroup

	// Mixed operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			streamID := "stream-" + string(rune('a'+i%3))

			switch i % 4 {
			case 0:
				_, _ = sfu.CreateRoom(streamID)
			case 1:
				_, _ = sfu.GetRoom(streamID)
			case 2:
				_, _ = sfu.GetOrCreateRoom(streamID)
			case 3:
				_ = sfu.RoomCount()
				_ = sfu.GetStats()
			}
		}(i)
	}

	wg.Wait()
}

func TestSFU_ContextTimeout(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = sfu.Close(ctx)
	require.NoError(t, err)
}

func TestSFU_API(t *testing.T) {
	cfg := DefaultConfig()
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	// Verify API is initialized
	sfu.mu.RLock()
	assert.NotNil(t, sfu.api)
	sfu.mu.RUnlock()
}

func BenchmarkSFU_CreateRoom(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		streamID := "stream-" + string(rune(i%26+'a'))
		_, _ = sfu.GetOrCreateRoom(streamID)
	}
}

func BenchmarkSFU_GetRoom(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	_, _ = sfu.CreateRoom("stream-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sfu.GetRoom("stream-1")
	}
}

func BenchmarkSFU_RoomCount(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	for i := 0; i < 100; i++ {
		_, _ = sfu.CreateRoom("stream-" + string(rune(i%26+'a')))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sfu.RoomCount()
	}
}

func BenchmarkSFU_GetStats(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		room, _ := sfu.CreateRoom("stream-" + string(rune(i+'a')))
		_, _ = room.SetPublisher(ctx, "publisher-"+string(rune(i+'a')))
		for j := 0; j < 5; j++ {
			_, _ = room.AddSubscriber(ctx, "subscriber-"+string(rune(i+'a'))+string(rune(j+'0')))
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sfu.GetStats()
	}
}

func BenchmarkSFU_ConcurrentGetRoom(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	_, _ = sfu.CreateRoom("stream-1")

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = sfu.GetRoom("stream-1")
		}
	})
}

func TestSFU_WithPLIInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PLIInterval = 500
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	assert.Equal(t, uint16(500), sfu.Config().PLIInterval)
}

func TestSFU_WithZeroPLIInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PLIInterval = 0
	logger := zaptest.NewLogger(t)

	sfu, err := New(cfg, logger)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	// Should not add PLI interceptor
	assert.Equal(t, uint16(0), sfu.Config().PLIInterval)
}

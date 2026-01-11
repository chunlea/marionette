package sfu

import (
	"sync"
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewInputChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a mock room
	ic := NewInputChannel(nil, logger)

	assert.NotNil(t, ic)
	assert.False(t, ic.IsClosed())
	assert.False(t, ic.HasPublisherChannel())
	assert.Equal(t, 0, ic.SubscriberCount())
}

func TestNewInputChannel_NilLogger(t *testing.T) {
	ic := NewInputChannel(nil, nil)

	assert.NotNil(t, ic)
	assert.NotNil(t, ic.logger)
}

func TestInputChannel_HasPublisherChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Initially no publisher channel
	assert.False(t, ic.HasPublisherChannel())
}

func TestInputChannel_SubscriberCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	assert.Equal(t, 0, ic.SubscriberCount())
}

func TestInputChannel_IsClosed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	assert.False(t, ic.IsClosed())

	ic.Close()

	assert.True(t, ic.IsClosed())
}

func TestInputChannel_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.Close()

	assert.True(t, ic.IsClosed())
}

func TestInputChannel_CloseIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Close multiple times should not panic
	ic.Close()
	ic.Close()
	ic.Close()

	assert.True(t, ic.IsClosed())
}

func TestInputChannel_Stats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	stats := ic.Stats()

	assert.False(t, stats.HasPublisherChannel)
	assert.False(t, stats.PublisherChannelOpen)
	assert.Equal(t, 0, stats.SubscriberCount)
}

func TestInputChannelStats_Struct(t *testing.T) {
	stats := InputChannelStats{
		HasPublisherChannel:  true,
		SubscriberCount:      5,
		PublisherChannelOpen: true,
	}

	assert.True(t, stats.HasPublisherChannel)
	assert.Equal(t, 5, stats.SubscriberCount)
	assert.True(t, stats.PublisherChannelOpen)
}

func TestInputChannel_RemoveSubscriberChannel_NotExists(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Should not panic
	ic.RemoveSubscriberChannel("nonexistent")
}

func TestInputChannel_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ic.IsClosed()
			_ = ic.HasPublisherChannel()
			_ = ic.SubscriberCount()
			_ = ic.Stats()
		}()
	}

	// Concurrent removes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ic.RemoveSubscriberChannel("peer-" + string(rune('a'+i)))
		}(i)
	}

	wg.Wait()
}

func TestInputChannel_SubscriberChannelsMap(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.mu.RLock()
	assert.NotNil(t, ic.subscriberChannels)
	ic.mu.RUnlock()
}

func TestInputChannel_CloseEmptiesChannels(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.Close()

	ic.mu.RLock()
	assert.Empty(t, ic.subscriberChannels)
	ic.mu.RUnlock()
}

func TestInputChannel_SetPublisherChannel_WhenClosed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.Close()

	// Create a mock data channel
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	// Should not set when closed
	ic.SetPublisherChannel(dc)

	assert.False(t, ic.HasPublisherChannel())
}

func TestInputChannel_AddSubscriberChannel_WhenClosed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.Close()

	// Create a mock data channel
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	err = ic.AddSubscriberChannel("peer-1", dc)
	assert.Error(t, err)
}

func TestInputChannel_ForwardToPublisher_NoPublisher(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Should not panic when forwarding with no publisher
	ic.forwardToPublisher("peer-1", []byte("test data"))
}

func TestInputChannel_StatsAfterClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	ic.Close()

	stats := ic.Stats()
	assert.False(t, stats.HasPublisherChannel)
	assert.Equal(t, 0, stats.SubscriberCount)
}

func TestInputChannel_WithDataChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Create peer connections for data channels
	pc1, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc1.Close() }()

	dc1, err := pc1.CreateDataChannel("input", nil)
	require.NoError(t, err)

	err = ic.AddSubscriberChannel("subscriber-1", dc1)
	assert.NoError(t, err)

	assert.Equal(t, 1, ic.SubscriberCount())

	// Remove subscriber
	ic.RemoveSubscriberChannel("subscriber-1")
	assert.Equal(t, 0, ic.SubscriberCount())
}

func TestInputChannel_SetPublisherChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	ic.SetPublisherChannel(dc)

	assert.True(t, ic.HasPublisherChannel())
}

func TestInputChannel_MultipleSubscribers(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Create multiple peer connections
	for i := 0; i < 5; i++ {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		require.NoError(t, err)
		defer func(pc *webrtc.PeerConnection) { _ = pc.Close() }(pc)

		dc, err := pc.CreateDataChannel("input", nil)
		require.NoError(t, err)

		peerID := "subscriber-" + string(rune('a'+i))
		err = ic.AddSubscriberChannel(peerID, dc)
		assert.NoError(t, err)
	}

	assert.Equal(t, 5, ic.SubscriberCount())

	// Remove some
	ic.RemoveSubscriberChannel("subscriber-a")
	ic.RemoveSubscriberChannel("subscriber-c")

	assert.Equal(t, 3, ic.SubscriberCount())
}

func TestInputChannel_RemoveSubscriberChannel_Idempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	err = ic.AddSubscriberChannel("subscriber-1", dc)
	assert.NoError(t, err)
	assert.Equal(t, 1, ic.SubscriberCount())

	// Remove multiple times
	ic.RemoveSubscriberChannel("subscriber-1")
	ic.RemoveSubscriberChannel("subscriber-1")
	ic.RemoveSubscriberChannel("subscriber-1")

	assert.Equal(t, 0, ic.SubscriberCount())
}

func BenchmarkInputChannel_Stats(b *testing.B) {
	ic := NewInputChannel(nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ic.Stats()
	}
}

func BenchmarkInputChannel_SubscriberCount(b *testing.B) {
	ic := NewInputChannel(nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ic.SubscriberCount()
	}
}

func BenchmarkInputChannel_HasPublisherChannel(b *testing.B) {
	ic := NewInputChannel(nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ic.HasPublisherChannel()
	}
}

func BenchmarkInputChannel_ConcurrentReads(b *testing.B) {
	ic := NewInputChannel(nil, nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ic.SubscriberCount()
			_ = ic.HasPublisherChannel()
		}
	})
}

func TestInputChannel_Logger(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	assert.NotNil(t, ic.logger)
}

func TestInputChannel_Room(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ic := NewInputChannel(nil, logger)

	// Room should be nil when passed nil
	assert.Nil(t, ic.room)
}

func TestInputChannel_SetPublisherChannel_Callbacks(t *testing.T) {
	logger := zap.NewNop() // Use nop logger to avoid async logging
	ic := NewInputChannel(nil, logger)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	ic.SetPublisherChannel(dc)

	// Verify channel is set
	assert.True(t, ic.HasPublisherChannel())

	stats := ic.Stats()
	assert.True(t, stats.HasPublisherChannel)
}

func TestInputChannel_AddSubscriberChannel_Callbacks(t *testing.T) {
	logger := zap.NewNop() // Use nop logger to avoid async logging
	ic := NewInputChannel(nil, logger)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	err = ic.AddSubscriberChannel("sub-1", dc)
	require.NoError(t, err)

	assert.Equal(t, 1, ic.SubscriberCount())
}

func TestInputChannel_ForwardToPublisher_NotOpen(t *testing.T) {
	logger := zap.NewNop() // Use nop logger
	ic := NewInputChannel(nil, logger)

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer func() { _ = pc.Close() }()

	dc, err := pc.CreateDataChannel("input", nil)
	require.NoError(t, err)

	ic.SetPublisherChannel(dc)

	// Data channel is not open yet (connecting state), so forward should drop
	ic.forwardToPublisher("peer-1", []byte("test"))

	// Should not panic
}

func TestInputChannel_Stats_WithOpenChannel(t *testing.T) {
	logger := zap.NewNop()
	ic := NewInputChannel(nil, logger)

	// Without a publisher channel
	stats := ic.Stats()
	assert.False(t, stats.HasPublisherChannel)
	assert.False(t, stats.PublisherChannelOpen)
}

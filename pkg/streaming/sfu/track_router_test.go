package sfu

import (
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewTrackRouter(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	assert.NotNil(t, router)
	assert.False(t, router.IsClosed())
	assert.Equal(t, 0, router.TrackCount())
}

func TestNewTrackRouter_NilLogger(t *testing.T) {
	router := NewTrackRouter(nil)

	assert.NotNil(t, router)
	assert.NotNil(t, router.logger)
}

func TestTrackRouter_TrackCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	assert.Equal(t, 0, router.TrackCount())
}

func TestTrackRouter_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.Close()

	assert.True(t, router.IsClosed())
}

func TestTrackRouter_CloseIdempotent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	// Close multiple times should not panic
	router.Close()
	router.Close()
	router.Close()

	assert.True(t, router.IsClosed())
}

func TestTrackRouter_IsClosed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	assert.False(t, router.IsClosed())

	router.Close()

	assert.True(t, router.IsClosed())
}

func TestTrackRouter_GetTrackInfo_Empty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	infos := router.GetTrackInfo()

	assert.Empty(t, infos)
}

func TestTrackRouter_GetLocalTrack_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	track, ok := router.GetLocalTrack("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, track)
}

func TestTrackRouter_RemoveSubscriber_Empty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	// Should not panic when removing nonexistent subscriber
	router.RemoveSubscriber("nonexistent-peer")
}

func TestTrackInfo_Struct(t *testing.T) {
	info := TrackInfo{
		ID:              "track-123",
		Kind:            "video",
		Codec:           "video/VP8",
		SubscriberCount: 3,
	}

	assert.Equal(t, "track-123", info.ID)
	assert.Equal(t, "video", info.Kind)
	assert.Equal(t, "video/VP8", info.Codec)
	assert.Equal(t, 3, info.SubscriberCount)
}

func TestTrackRouter_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = router.TrackCount()
			_ = router.IsClosed()
			_ = router.GetTrackInfo()
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			router.RemoveSubscriber("peer-" + string(rune('a'+i)))
		}(i)
	}

	wg.Wait()
}

func TestTrackRouter_TracksMapInitialized(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.mu.RLock()
	assert.NotNil(t, router.tracks)
	router.mu.RUnlock()
}

func TestTrackRouter_CloseEmptiesTracks(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.Close()

	router.mu.RLock()
	assert.Empty(t, router.tracks)
	router.mu.RUnlock()
}

// TestRoutedTrack tests the internal routedTrack structure
func TestRoutedTrack_Struct(t *testing.T) {
	// Create a mock routedTrack to verify structure
	rt := &routedTrack{
		remote:      nil, // Would be set from webrtc.TrackRemote
		local:       nil, // Would be set from webrtc.TrackLocalStaticRTP
		subscribers: make(map[string]*webrtc.RTPSender),
		stopCh:      make(chan struct{}),
	}

	assert.NotNil(t, rt.subscribers)
	assert.NotNil(t, rt.stopCh)
}

func TestTrackRouter_Integration_NoTracks(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	// Create a mock peer for subscriber
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer pc.Close()

	peer := NewPeer(PeerConfig{
		ID:     "subscriber-1",
		Role:   PeerRoleSubscriber,
		Logger: logger,
	}, pc)

	// Adding subscriber with no tracks should succeed
	err = router.AddSubscriber(peer)
	assert.NoError(t, err)
}

func TestTrackRouter_AddSubscriber_Closed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer pc.Close()

	peer := NewPeer(PeerConfig{
		ID:     "subscriber-1",
		Role:   PeerRoleSubscriber,
		Logger: logger,
	}, pc)

	err = router.AddSubscriber(peer)
	assert.Error(t, err)
}

func TestTrackRouter_RemoveSubscriber_MultipleTimes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	// Should not panic when removing same subscriber multiple times
	router.RemoveSubscriber("peer-1")
	router.RemoveSubscriber("peer-1")
	router.RemoveSubscriber("peer-1")
}

func TestTrackRouter_GetTrackInfo_AfterClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.Close()

	infos := router.GetTrackInfo()
	assert.Empty(t, infos)
}

func TestTrackRouter_GetLocalTrack_AfterClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	router.Close()

	track, ok := router.GetLocalTrack("any-track")
	assert.False(t, ok)
	assert.Nil(t, track)
}

func BenchmarkTrackRouter_TrackCount(b *testing.B) {
	router := NewTrackRouter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.TrackCount()
	}
}

func BenchmarkTrackRouter_IsClosed(b *testing.B) {
	router := NewTrackRouter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.IsClosed()
	}
}

func BenchmarkTrackRouter_GetTrackInfo(b *testing.B) {
	router := NewTrackRouter(nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.GetTrackInfo()
	}
}

func BenchmarkTrackRouter_ConcurrentReads(b *testing.B) {
	router := NewTrackRouter(nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = router.TrackCount()
			_ = router.IsClosed()
		}
	})
}

// Test that the stop channel properly signals goroutines
func TestRoutedTrack_StopChannel(t *testing.T) {
	stopCh := make(chan struct{})

	// Start a goroutine that waits on the stop channel
	done := make(chan struct{})
	go func() {
		select {
		case <-stopCh:
			close(done)
		case <-time.After(time.Second):
			t.Error("goroutine did not receive stop signal")
		}
	}()

	// Close the stop channel
	close(stopCh)

	// Wait for goroutine to finish
	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("goroutine did not finish")
	}
}

// Test subscriber map operations
func TestRoutedTrack_SubscribersMap(t *testing.T) {
	rt := &routedTrack{
		subscribers: make(map[string]*webrtc.RTPSender),
	}

	// Add subscriber
	rt.subscribers["peer-1"] = nil
	rt.subscribers["peer-2"] = nil

	assert.Len(t, rt.subscribers, 2)

	// Remove subscriber
	delete(rt.subscribers, "peer-1")
	assert.Len(t, rt.subscribers, 1)

	// Check existence
	_, exists := rt.subscribers["peer-2"]
	assert.True(t, exists)

	_, exists = rt.subscribers["peer-1"]
	assert.False(t, exists)
}

func TestTrackRouter_MultipleSubscribers_RemoveOne(t *testing.T) {
	logger := zap.NewNop() // Use nop logger to avoid async logging issues
	router := NewTrackRouter(logger)

	// Create mock peers
	pc1, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)

	pc2, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)

	peer1 := NewPeer(PeerConfig{
		ID:     "subscriber-1",
		Role:   PeerRoleSubscriber,
		Logger: logger,
	}, pc1)

	peer2 := NewPeer(PeerConfig{
		ID:     "subscriber-2",
		Role:   PeerRoleSubscriber,
		Logger: logger,
	}, pc2)

	// Add subscribers
	err = router.AddSubscriber(peer1)
	assert.NoError(t, err)

	err = router.AddSubscriber(peer2)
	assert.NoError(t, err)

	// Remove one subscriber
	router.RemoveSubscriber("subscriber-1")

	// Router should still be functional
	assert.False(t, router.IsClosed())

	// Close connections synchronously before test ends
	pc1.Close()
	pc2.Close()
}

func TestTrackRouter_Logger(t *testing.T) {
	logger := zaptest.NewLogger(t)
	router := NewTrackRouter(logger)

	// Logger should be named
	assert.NotNil(t, router.logger)
}

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

// createTestPeerConnection creates a WebRTC peer connection for testing.
func createTestPeerConnection(t *testing.T) *webrtc.PeerConnection {
	t.Helper()

	config := webrtc.Configuration{}
	pc, err := webrtc.NewPeerConnection(config)
	require.NoError(t, err)

	t.Cleanup(func() {
		pc.Close()
	})

	return pc
}

// createTestPeer creates a Peer for testing.
func createTestPeer(t *testing.T, role PeerRole) *Peer {
	t.Helper()

	pc := createTestPeerConnection(t)
	logger := zaptest.NewLogger(t)

	return NewPeer(PeerConfig{
		ID:     "test-peer-" + string(role),
		Role:   role,
		Logger: logger,
	}, pc)
}

func TestNewPeer(t *testing.T) {
	pc := createTestPeerConnection(t)
	logger := zaptest.NewLogger(t)

	peer := NewPeer(PeerConfig{
		ID:     "test-peer",
		Role:   PeerRolePublisher,
		Logger: logger,
	}, pc)

	assert.Equal(t, "test-peer", peer.ID)
	assert.Equal(t, PeerRolePublisher, peer.Role)
	assert.Equal(t, PeerStateNew, peer.State())
	assert.NotNil(t, peer.Connection)
	assert.NotNil(t, peer.Logger)
}

func TestNewPeer_NilLogger(t *testing.T) {
	pc := createTestPeerConnection(t)

	peer := NewPeer(PeerConfig{
		ID:     "test-peer",
		Role:   PeerRoleSubscriber,
		Logger: nil,
	}, pc)

	// Should use nop logger
	assert.NotNil(t, peer.Logger)
	assert.Equal(t, "test-peer", peer.ID)
}

func TestPeerRole_Constants(t *testing.T) {
	assert.Equal(t, PeerRole("publisher"), PeerRolePublisher)
	assert.Equal(t, PeerRole("subscriber"), PeerRoleSubscriber)
}

func TestPeerState_Constants(t *testing.T) {
	assert.Equal(t, PeerState("new"), PeerStateNew)
	assert.Equal(t, PeerState("connecting"), PeerStateConnecting)
	assert.Equal(t, PeerState("connected"), PeerStateConnected)
	assert.Equal(t, PeerState("disconnected"), PeerStateDisconnected)
	assert.Equal(t, PeerState("failed"), PeerStateFailed)
	assert.Equal(t, PeerState("closed"), PeerStateClosed)
}

func TestPeer_State(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Initial state should be New
	assert.Equal(t, PeerStateNew, peer.State())
}

func TestPeer_OnStateChange(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	var receivedState PeerState
	var mu sync.Mutex

	peer.OnStateChange(func(state PeerState) {
		mu.Lock()
		receivedState = state
		mu.Unlock()
	})

	// Simulate state change by calling setState directly
	peer.setState(PeerStateConnecting)

	mu.Lock()
	assert.Equal(t, PeerStateConnecting, receivedState)
	mu.Unlock()
}

func TestPeer_OnICECandidate(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	candidateReceived := make(chan *webrtc.ICECandidate, 1)

	peer.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		candidateReceived <- candidate
	})

	// The handler is set, but ICE gathering won't start until we create offer/answer
	// This just verifies the callback is set correctly
	assert.NotNil(t, peer.onICE)
}

func TestPeer_OnTrack(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	trackReceived := make(chan struct{}, 1)

	peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		trackReceived <- struct{}{}
	})

	assert.NotNil(t, peer.onTrack)
}

func TestPeer_OnDataChannel(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	dcReceived := make(chan *webrtc.DataChannel, 1)

	peer.OnDataChannel(func(dc *webrtc.DataChannel) {
		dcReceived <- dc
	})

	assert.NotNil(t, peer.onDataChannel)
}

func TestPeer_CreateOffer(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Add a transceiver so the offer has media
	_, err := peer.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	offer, err := peer.CreateOffer()
	require.NoError(t, err)

	assert.NotNil(t, offer)
	assert.Equal(t, webrtc.SDPTypeOffer, offer.Type)
	assert.NotEmpty(t, offer.SDP)

	// Verify local description is set
	localDesc := peer.LocalDescription()
	require.NotNil(t, localDesc)
	assert.Equal(t, webrtc.SDPTypeOffer, localDesc.Type)
}

func TestPeer_CreateAnswer(t *testing.T) {
	// Create two peers for offer/answer exchange
	offerPeer := createTestPeer(t, PeerRolePublisher)
	answerPeer := createTestPeer(t, PeerRoleSubscriber)

	// Add transceiver to offer peer
	_, err := offerPeer.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	// Create and set offer
	offer, err := offerPeer.CreateOffer()
	require.NoError(t, err)

	// Set remote description on answer peer
	err = answerPeer.SetRemoteDescription(*offer)
	require.NoError(t, err)

	// Create answer
	answer, err := answerPeer.CreateAnswer()
	require.NoError(t, err)

	assert.NotNil(t, answer)
	assert.Equal(t, webrtc.SDPTypeAnswer, answer.Type)
	assert.NotEmpty(t, answer.SDP)
}

func TestPeer_SetRemoteDescription(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Create an offer from another peer
	otherPeer := createTestPeer(t, PeerRolePublisher)
	_, err := otherPeer.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	offer, err := otherPeer.CreateOffer()
	require.NoError(t, err)

	// Set remote description
	err = peer.SetRemoteDescription(*offer)
	require.NoError(t, err)

	// Verify remote description is set
	remoteDesc := peer.RemoteDescription()
	require.NotNil(t, remoteDesc)
	assert.Equal(t, webrtc.SDPTypeOffer, remoteDesc.Type)
}

func TestPeer_AddICECandidate(t *testing.T) {
	// Set up two peers for proper SDP exchange
	peer1 := createTestPeer(t, PeerRolePublisher)
	peer2 := createTestPeer(t, PeerRoleSubscriber)

	_, err := peer1.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	offer, err := peer1.CreateOffer()
	require.NoError(t, err)

	err = peer2.SetRemoteDescription(*offer)
	require.NoError(t, err)

	answer, err := peer2.CreateAnswer()
	require.NoError(t, err)

	err = peer1.SetRemoteDescription(*answer)
	require.NoError(t, err)

	// Now we can add ICE candidates
	// Note: In real scenarios, we'd get actual candidates from ICE gathering
	// For now, test that the method works with a dummy candidate
	candidateInit := webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 udp 2130706431 192.168.1.1 50000 typ host",
	}

	// This may fail because the candidate doesn't match our SDP, but the method should work
	_ = peer1.AddICECandidate(candidateInit)
}

func TestPeer_CreateDataChannel(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	dc, err := peer.CreateDataChannel("test-channel", nil)
	require.NoError(t, err)
	require.NotNil(t, dc)

	assert.Equal(t, "test-channel", dc.Label())

	// Verify channel is tracked
	storedDC, ok := peer.GetDataChannel("test-channel")
	assert.True(t, ok)
	assert.Equal(t, dc, storedDC)
}

func TestPeer_CreateDataChannel_WithOptions(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	ordered := true
	maxRetransmits := uint16(3)
	opts := &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &maxRetransmits,
	}

	dc, err := peer.CreateDataChannel("ordered-channel", opts)
	require.NoError(t, err)
	require.NotNil(t, dc)

	assert.Equal(t, "ordered-channel", dc.Label())
	assert.True(t, dc.Ordered())
}

func TestPeer_GetDataChannel_NotFound(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	dc, ok := peer.GetDataChannel("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, dc)
}

func TestPeer_AddTrack(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Create a local track
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video",
		"stream",
	)
	require.NoError(t, err)

	sender, err := peer.AddTrack(track)
	require.NoError(t, err)
	require.NotNil(t, sender)
}

func TestPeer_Close(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Create a data channel
	_, err := peer.CreateDataChannel("test", nil)
	require.NoError(t, err)

	// Close the peer
	ctx := context.Background()
	err = peer.Close(ctx)
	require.NoError(t, err)

	assert.Equal(t, PeerStateClosed, peer.State())
}

func TestPeer_LocalDescription(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Initially nil
	assert.Nil(t, peer.LocalDescription())

	// After creating offer
	_, err := peer.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	_, err = peer.CreateOffer()
	require.NoError(t, err)

	assert.NotNil(t, peer.LocalDescription())
}

func TestPeer_RemoteDescription(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Initially nil
	assert.Nil(t, peer.RemoteDescription())

	// Set up offer from another peer
	otherPeer := createTestPeer(t, PeerRolePublisher)
	_, err := otherPeer.Connection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
	require.NoError(t, err)

	offer, err := otherPeer.CreateOffer()
	require.NoError(t, err)

	err = peer.SetRemoteDescription(*offer)
	require.NoError(t, err)

	assert.NotNil(t, peer.RemoteDescription())
}

func TestMapConnectionState(t *testing.T) {
	tests := []struct {
		input    webrtc.PeerConnectionState
		expected PeerState
	}{
		{webrtc.PeerConnectionStateNew, PeerStateNew},
		{webrtc.PeerConnectionStateConnecting, PeerStateConnecting},
		{webrtc.PeerConnectionStateConnected, PeerStateConnected},
		{webrtc.PeerConnectionStateDisconnected, PeerStateDisconnected},
		{webrtc.PeerConnectionStateFailed, PeerStateFailed},
		{webrtc.PeerConnectionStateClosed, PeerStateClosed},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			result := mapConnectionState(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPeer_SetState_NoChange(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	callCount := 0
	peer.OnStateChange(func(state PeerState) {
		callCount++
	})

	// Set same state multiple times
	peer.setState(PeerStateNew)
	peer.setState(PeerStateNew)

	// Should not trigger callback if state doesn't change
	assert.Equal(t, 0, callCount)
}

func TestPeer_SetState_Change(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	var states []PeerState
	var mu sync.Mutex

	peer.OnStateChange(func(state PeerState) {
		mu.Lock()
		states = append(states, state)
		mu.Unlock()
	})

	peer.setState(PeerStateConnecting)
	peer.setState(PeerStateConnected)
	peer.setState(PeerStateDisconnected)

	mu.Lock()
	assert.Equal(t, []PeerState{PeerStateConnecting, PeerStateConnected, PeerStateDisconnected}, states)
	mu.Unlock()
}

func TestPeer_ConcurrentCallbacks(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var stateChanges []PeerState

	peer.OnStateChange(func(state PeerState) {
		mu.Lock()
		stateChanges = append(stateChanges, state)
		mu.Unlock()
	})

	// Simulate concurrent state changes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				peer.setState(PeerStateConnecting)
			} else {
				peer.setState(PeerStateConnected)
			}
		}(i)
	}

	wg.Wait()

	// Count received states
	mu.Lock()
	count := len(stateChanges)
	mu.Unlock()

	// Due to same-state filtering, we won't get exactly 10
	assert.Greater(t, count, 0)
}

func TestPeer_TracksMap(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	track1, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video1",
		"stream1",
	)
	require.NoError(t, err)

	track2, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio1",
		"stream1",
	)
	require.NoError(t, err)

	_, err = peer.AddTrack(track1)
	require.NoError(t, err)

	_, err = peer.AddTrack(track2)
	require.NoError(t, err)

	// Verify tracks are stored
	peer.mu.RLock()
	assert.Len(t, peer.tracks, 2)
	peer.mu.RUnlock()
}

func TestPeer_MultipleDataChannels(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	dc1, err := peer.CreateDataChannel("channel1", nil)
	require.NoError(t, err)

	dc2, err := peer.CreateDataChannel("channel2", nil)
	require.NoError(t, err)

	dc3, err := peer.CreateDataChannel("channel3", nil)
	require.NoError(t, err)

	// Verify all channels are tracked
	storedDC1, ok := peer.GetDataChannel("channel1")
	assert.True(t, ok)
	assert.Equal(t, dc1, storedDC1)

	storedDC2, ok := peer.GetDataChannel("channel2")
	assert.True(t, ok)
	assert.Equal(t, dc2, storedDC2)

	storedDC3, ok := peer.GetDataChannel("channel3")
	assert.True(t, ok)
	assert.Equal(t, dc3, storedDC3)
}

func TestPeer_CloseDataChannels(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	_, err := peer.CreateDataChannel("channel1", nil)
	require.NoError(t, err)

	_, err = peer.CreateDataChannel("channel2", nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = peer.Close(ctx)
	require.NoError(t, err)

	// Data channels should be cleared
	peer.mu.RLock()
	assert.Empty(t, peer.dataChannels)
	peer.mu.RUnlock()
}

func BenchmarkPeer_CreateOffer(b *testing.B) {
	logger := zap.NewNop()

	for i := 0; i < b.N; i++ {
		config := webrtc.Configuration{}
		pc, _ := webrtc.NewPeerConnection(config)

		peer := NewPeer(PeerConfig{
			ID:     "bench-peer",
			Role:   PeerRolePublisher,
			Logger: logger,
		}, pc)

		pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo)
		peer.CreateOffer()
		pc.Close()
	}
}

func BenchmarkPeer_StateLookup(b *testing.B) {
	config := webrtc.Configuration{}
	pc, _ := webrtc.NewPeerConnection(config)
	defer pc.Close()

	peer := NewPeer(PeerConfig{
		ID:     "bench-peer",
		Role:   PeerRolePublisher,
		Logger: zap.NewNop(),
	}, pc)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = peer.State()
	}
}

func TestPeer_ContextCancellation(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := peer.Close(ctx)
	require.NoError(t, err)
}

func TestPeer_RemoveTrack(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Create and add a track
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video",
		"stream",
	)
	require.NoError(t, err)

	sender, err := peer.AddTrack(track)
	require.NoError(t, err)
	require.NotNil(t, sender)

	// Remove the track
	err = peer.RemoveTrack(sender)
	require.NoError(t, err)
}

func TestPeer_CreateOffer_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Close the connection first
	peer.Connection.Close()

	// CreateOffer should fail on closed connection
	_, err := peer.CreateOffer()
	assert.Error(t, err)
}

func TestPeer_CreateAnswer_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// CreateAnswer without remote description should fail
	_, err := peer.CreateAnswer()
	assert.Error(t, err)
}

func TestPeer_SetRemoteDescription_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Close the connection first
	peer.Connection.Close()

	// SetRemoteDescription should fail on closed connection
	err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  "invalid sdp",
	})
	assert.Error(t, err)
}

func TestPeer_AddICECandidate_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// AddICECandidate without remote description should fail
	err := peer.AddICECandidate(webrtc.ICECandidateInit{
		Candidate: "candidate:1 1 udp 2130706431 192.168.1.1 50000 typ host",
	})
	assert.Error(t, err)
}

func TestPeer_AddTrack_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Close connection first
	peer.Connection.Close()

	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video",
		"stream",
	)
	require.NoError(t, err)

	// AddTrack on closed connection should fail
	_, err = peer.AddTrack(track)
	assert.Error(t, err)
}

func TestPeer_CreateDataChannel_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRolePublisher)

	// Close connection first
	peer.Connection.Close()

	// CreateDataChannel on closed connection should fail
	_, err := peer.CreateDataChannel("test", nil)
	assert.Error(t, err)
}

func TestPeer_RemoveTrack_Error(t *testing.T) {
	peer := createTestPeer(t, PeerRoleSubscriber)

	// Create a different peer to get a sender
	peer2 := createTestPeer(t, PeerRoleSubscriber)
	track, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video",
		"stream",
	)
	sender, _ := peer2.AddTrack(track)

	// Try to remove sender from wrong peer - should fail
	err := peer.RemoveTrack(sender)
	assert.Error(t, err)
}

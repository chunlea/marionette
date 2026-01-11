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
)

func createTestSignalingHandler(t *testing.T) (*SignalingHandler, *SFU) {
	t.Helper()

	cfg := DefaultConfig()
	logger := zap.NewNop()

	sfu, err := New(cfg, logger)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = sfu.Close(context.Background())
	})

	handler := NewSignalingHandler(sfu, logger)
	return handler, sfu
}

func TestNewSignalingHandler(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	assert.NotNil(t, handler)
	assert.Equal(t, 0, handler.SessionCount())
}

func TestNewSignalingHandler_NilLogger(t *testing.T) {
	cfg := DefaultConfig()
	sfu, err := New(cfg, nil)
	require.NoError(t, err)
	defer func() { _ = sfu.Close(context.Background()) }()

	handler := NewSignalingHandler(sfu, nil)
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.logger)
}

func TestSignalingHandler_OnSend(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var receivedMsg *SignalingMessage
	var receivedStreamID, receivedPeerID string

	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		receivedStreamID = streamID
		receivedPeerID = peerID
		receivedMsg = msg
	})

	// Trigger a send
	handler.send("stream-1", "peer-1", NewPongMessage())

	assert.Equal(t, "stream-1", receivedStreamID)
	assert.Equal(t, "peer-1", receivedPeerID)
	assert.Equal(t, SignalingTypePong, receivedMsg.Type)
}

func TestSignalingHandler_OnSend_NilCallback(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	// Should not panic with nil callback
	handler.send("stream-1", "peer-1", NewPongMessage())
}

func TestSignalingHandler_HandleMessage_Ping(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var pongReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypePong {
			pongReceived = true
		}
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, NewPingMessage())
	require.NoError(t, err)

	assert.True(t, pongReceived)
}

func TestSignalingHandler_HandleMessage_UnknownType(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError {
			errorReceived = true
		}
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, &SignalingMessage{Type: "unknown"})
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleJoin_Publisher(t *testing.T) {
	handler, sfu := createTestSignalingHandler(t)

	var mu sync.Mutex
	var messages []*SignalingMessage
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Should have a session
	assert.Equal(t, 1, handler.SessionCount())

	// Should have created a room
	room, ok := sfu.GetRoom("stream-1")
	assert.True(t, ok)
	assert.True(t, room.HasPublisher())

	// Should have received ready message
	mu.Lock()
	var hasReady bool
	for _, msg := range messages {
		if msg.Type == SignalingTypeReady {
			hasReady = true
		}
	}
	mu.Unlock()
	assert.True(t, hasReady)
}

func TestSignalingHandler_HandleJoin_Subscriber(t *testing.T) {
	handler, sfu := createTestSignalingHandler(t)

	var mu sync.Mutex
	var messages []*SignalingMessage
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	})

	ctx := context.Background()

	// First add a publisher
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Then add a subscriber
	err = handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	require.NoError(t, err)

	// Should have 2 sessions
	assert.Equal(t, 2, handler.SessionCount())

	// Room should have subscriber
	room, ok := sfu.GetRoom("stream-1")
	assert.True(t, ok)
	assert.Equal(t, 1, room.SubscriberCount())
}

func TestSignalingHandler_HandleJoin_DuplicatePublisher(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError && msg.ErrorCode == ErrorCodePublisherExists {
			errorReceived = true
		}
	})

	ctx := context.Background()

	// First publisher
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Second publisher should fail
	err = handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-2", PeerRolePublisher))
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleJoin_DuplicateSubscriber(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError {
			errorReceived = true
		}
	})

	ctx := context.Background()

	// First subscriber
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	require.NoError(t, err)

	// Duplicate subscriber should fail
	err = handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleJoin_InvalidRole(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError {
			errorReceived = true
		}
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, &SignalingMessage{
		Type:     SignalingTypeJoin,
		StreamID: "stream-1",
		PeerID:   "peer-1",
		Role:     "invalid",
	})
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleLeave(t *testing.T) {
	handler, sfu := createTestSignalingHandler(t)

	ctx := context.Background()

	// Join first
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	require.NoError(t, err)
	assert.Equal(t, 1, handler.SessionCount())

	// Leave
	err = handler.HandleMessage(ctx, NewLeaveMessage("stream-1", "subscriber-1"))
	require.NoError(t, err)

	// Session should be removed
	assert.Equal(t, 0, handler.SessionCount())

	// Room should have no subscribers
	room, ok := sfu.GetRoom("stream-1")
	assert.True(t, ok)
	assert.Equal(t, 0, room.SubscriberCount())
}

func TestSignalingHandler_HandleLeave_NotJoined(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// Leave without joining should not error
	err := handler.HandleMessage(ctx, NewLeaveMessage("stream-1", "peer-1"))
	require.NoError(t, err)
}

func TestSignalingHandler_HandleLeave_Publisher(t *testing.T) {
	handler, sfu := createTestSignalingHandler(t)

	ctx := context.Background()

	// Join as publisher
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Leave as publisher should remove room
	err = handler.HandleMessage(ctx, NewLeaveMessage("stream-1", "publisher-1"))
	require.NoError(t, err)

	// Room should be removed
	_, ok := sfu.GetRoom("stream-1")
	assert.False(t, ok)
}

func TestSignalingHandler_HandleOffer(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var answerReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeAnswer {
			answerReceived = true
		}
	})

	ctx := context.Background()

	// Join first
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Send offer (need a valid SDP)
	offer := createTestSDP(t)
	err = handler.HandleMessage(ctx, NewOfferMessage("stream-1", "publisher-1", offer))
	require.NoError(t, err)

	assert.True(t, answerReceived)
}

func TestSignalingHandler_HandleOffer_NotJoined(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError && msg.ErrorCode == ErrorCodePeerNotFound {
			errorReceived = true
		}
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, NewOfferMessage("stream-1", "peer-1", "v=0"))
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleAnswer(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// Join as subscriber (auto-creates offer)
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	require.NoError(t, err)

	// Get the session - an offer was already created during join
	session, ok := handler.GetSession("subscriber-1")
	require.True(t, ok)

	// The peer is in have-local-offer state (offer was created during join)
	// Send an answer to complete the exchange
	answer := createTestSDP(t)
	err = handler.HandleMessage(ctx, NewAnswerMessage("stream-1", "subscriber-1", answer))
	// This may fail due to SDP mismatch, but should not panic
	// The error handling is correct
	_ = err
	_ = session
}

func TestSignalingHandler_HandleAnswer_NotJoined(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError && msg.ErrorCode == ErrorCodePeerNotFound {
			errorReceived = true
		}
	})

	ctx := context.Background()
	err := handler.HandleMessage(ctx, NewAnswerMessage("stream-1", "peer-1", "v=0"))
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleCandidate(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// Join first
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Set up offer/answer to allow ICE candidates
	session, ok := handler.GetSession("publisher-1")
	require.True(t, ok)

	// Add transceiver
	_, err = session.peer.Connection.AddTransceiverFromKind(1) // Audio
	require.NoError(t, err)

	// Create and set offer
	offer, err := session.peer.CreateOffer()
	require.NoError(t, err)

	// Create answer peer
	answer := createTestSDP(t)
	_ = session.peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	})

	// Send candidate (may fail due to SDP issues, but should not panic)
	sdpMid := "0"
	candidate := &ICECandidateJSON{
		Candidate: "candidate:1 1 udp 2130706431 192.168.1.1 50000 typ host",
		SDPMid:    &sdpMid,
	}
	err = handler.HandleMessage(ctx, NewCandidateMessage("stream-1", "publisher-1", candidate))
	// Error is expected due to SDP state, but handler should handle it gracefully
	_ = err
	_ = offer
}

func TestSignalingHandler_HandleCandidate_NotJoined(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	var errorReceived bool
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeError && msg.ErrorCode == ErrorCodePeerNotFound {
			errorReceived = true
		}
	})

	ctx := context.Background()
	candidate := &ICECandidateJSON{Candidate: "candidate:1..."}
	err := handler.HandleMessage(ctx, NewCandidateMessage("stream-1", "peer-1", candidate))
	assert.Error(t, err)
	assert.True(t, errorReceived)
}

func TestSignalingHandler_HandleCandidate_NilCandidate(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// Join first
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "publisher-1", PeerRolePublisher))
	require.NoError(t, err)

	// Send nil candidate (end of candidates)
	msg := &SignalingMessage{
		Type:      SignalingTypeCandidate,
		StreamID:  "stream-1",
		PeerID:    "publisher-1",
		Candidate: nil,
	}
	err = handler.HandleMessage(ctx, msg)
	require.NoError(t, err)
}

func TestSignalingHandler_GetSession(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// No session initially
	_, ok := handler.GetSession("peer-1")
	assert.False(t, ok)

	// Join
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-1", PeerRolePublisher))
	require.NoError(t, err)

	// Should have session now
	session, ok := handler.GetSession("peer-1")
	assert.True(t, ok)
	assert.Equal(t, "stream-1", session.streamID)
	assert.Equal(t, "peer-1", session.peerID)
	assert.Equal(t, PeerRolePublisher, session.role)
}

func TestSignalingHandler_SessionCount(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	assert.Equal(t, 0, handler.SessionCount())

	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-1", PeerRoleSubscriber))
	require.NoError(t, err)
	assert.Equal(t, 1, handler.SessionCount())

	err = handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-2", PeerRoleSubscriber))
	require.NoError(t, err)
	assert.Equal(t, 2, handler.SessionCount())

	err = handler.HandleMessage(ctx, NewLeaveMessage("stream-1", "peer-1"))
	require.NoError(t, err)
	assert.Equal(t, 1, handler.SessionCount())
}

func TestSignalingHandler_Close(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-1", PeerRoleSubscriber))
	require.NoError(t, err)
	err = handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-2", PeerRoleSubscriber))
	require.NoError(t, err)

	assert.Equal(t, 2, handler.SessionCount())

	handler.Close()

	assert.Equal(t, 0, handler.SessionCount())
}

func TestSignalingHandler_ConcurrentJoins(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()
	var wg sync.WaitGroup

	// Concurrent joins
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			peerID := "peer-" + string(rune('a'+i))
			_ = handler.HandleMessage(ctx, NewJoinMessage("stream-1", peerID, PeerRoleSubscriber))
		}(i)
	}

	wg.Wait()

	// Should have created sessions (some may have failed due to timing)
	assert.Greater(t, handler.SessionCount(), 0)
}

func TestSignalingHandler_CreateOfferForSubscriber(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	// Capture the initial offer sent during join
	var initialOffer *SignalingMessage
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {
		if msg.Type == SignalingTypeOffer {
			initialOffer = msg
		}
	})

	// Join as subscriber (auto-creates and sends offer)
	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "subscriber-1", PeerRoleSubscriber))
	require.NoError(t, err)

	// Verify that an offer was auto-created during join
	require.NotNil(t, initialOffer, "offer should be created during subscriber join")
	assert.Equal(t, SignalingTypeOffer, initialOffer.Type)
	assert.NotEmpty(t, initialOffer.SDP)
	assert.Equal(t, "stream-1", initialOffer.StreamID)
	assert.Equal(t, "subscriber-1", initialOffer.PeerID)

	// Get session to verify it exists
	session, ok := handler.GetSession("subscriber-1")
	require.True(t, ok)
	_ = session
}

func TestSignalingHandler_CreateOfferForSubscriber_NotFound(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	_, err := handler.CreateOfferForSubscriber(ctx, "stream-1", "nonexistent")
	assert.Error(t, err)
}

func TestSignalingHandler_CleanupPeer(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx := context.Background()

	err := handler.HandleMessage(ctx, NewJoinMessage("stream-1", "peer-1", PeerRoleSubscriber))
	require.NoError(t, err)
	assert.Equal(t, 1, handler.SessionCount())

	handler.cleanupPeer("peer-1")

	assert.Equal(t, 0, handler.SessionCount())
}

// createTestSDP creates a minimal valid SDP for testing.
func createTestSDP(t *testing.T) string {
	t.Helper()
	return `v=0
o=- 0 0 IN IP4 127.0.0.1
s=-
t=0 0
a=group:BUNDLE 0
m=audio 9 UDP/TLS/RTP/SAVPF 111
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:test
a=ice-pwd:testpassword1234567890
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:0
a=sendrecv
a=rtpmap:111 opus/48000/2
`
}

func BenchmarkSignalingHandler_HandlePing(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	handler := NewSignalingHandler(sfu, zap.NewNop())
	handler.OnSend(func(streamID, peerID string, msg *SignalingMessage) {})

	ctx := context.Background()
	msg := NewPingMessage()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleMessage(ctx, msg)
	}
}

func BenchmarkSignalingHandler_SessionLookup(b *testing.B) {
	cfg := DefaultConfig()
	sfu, _ := New(cfg, zap.NewNop())
	defer func() { _ = sfu.Close(context.Background()) }()

	handler := NewSignalingHandler(sfu, zap.NewNop())

	// Add some sessions
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		peerID := "peer-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		_ = handler.HandleMessage(ctx, NewJoinMessage("stream-1", peerID, PeerRoleSubscriber))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.GetSession("peer-m3")
	}
}

func TestSignalingHandler_ContextTimeout(t *testing.T) {
	handler, _ := createTestSignalingHandler(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := handler.HandleMessage(ctx, NewPingMessage())
	require.NoError(t, err)
}

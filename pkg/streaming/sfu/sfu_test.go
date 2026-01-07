package sfu

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

func TestNewSFU(t *testing.T) {
	config := DefaultConfig()
	logger := zap.NewNop()

	sfu, err := New(config, logger)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sfu == nil {
		t.Fatal("expected non-nil SFU")
	}
}

func TestNewSFUNilLogger(t *testing.T) {
	config := DefaultConfig()

	sfu, err := New(config, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sfu == nil {
		t.Fatal("expected non-nil SFU")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if len(config.ICEServers) == 0 {
		t.Error("expected at least one ICE server")
	}
	if !config.EnableTWCC {
		t.Error("expected TWCC to be enabled")
	}
	if !config.EnableRTCPReports {
		t.Error("expected RTCP reports to be enabled")
	}
	if config.PLIInterval != 3 {
		t.Errorf("expected PLI interval 3, got %d", config.PLIInterval)
	}
	if config.MaxBufferedAmount != 1024*1024 {
		t.Errorf("expected max buffered amount 1MB, got %d", config.MaxBufferedAmount)
	}
}

func TestSFUCreateRoom(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	room, err := sfu.CreateRoom("stream-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if room == nil {
		t.Fatal("expected non-nil room")
	}
	if room.StreamID() != "stream-1" {
		t.Errorf("expected stream ID %q, got %q", "stream-1", room.StreamID())
	}
}

func TestSFUCreateRoomDuplicate(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error creating first room: %v", err)
	}

	_, err = sfu.CreateRoom("stream-1")
	if err == nil {
		t.Fatal("expected error for duplicate room")
	}
}

func TestSFUGetRoom(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	room, ok := sfu.GetRoom("stream-1")
	if !ok {
		t.Fatal("expected room to be found")
	}
	if room.StreamID() != "stream-1" {
		t.Errorf("expected stream ID %q, got %q", "stream-1", room.StreamID())
	}

	_, ok = sfu.GetRoom("nonexistent")
	if ok {
		t.Error("expected room not to be found")
	}
}

func TestSFURemoveRoom(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = sfu.RemoveRoom(ctx, "stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := sfu.GetRoom("stream-1")
	if ok {
		t.Error("expected room to be removed")
	}
}

func TestSFURemoveRoomNotFound(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	ctx := context.Background()
	err := sfu.RemoveRoom(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent room")
	}
}

func TestSFUListRooms(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = sfu.CreateRoom("stream-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rooms := sfu.ListRooms()
	if len(rooms) != 2 {
		t.Errorf("expected 2 rooms, got %d", len(rooms))
	}
}

func TestSFUGetStats(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := sfu.GetStats()
	if stats.RoomCount != 1 {
		t.Errorf("expected room count 1, got %d", stats.RoomCount)
	}
}

func TestSFUClose(t *testing.T) {
	sfu := createTestSFU(t)

	_, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = sfu.Close(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rooms := sfu.ListRooms()
	if len(rooms) != 0 {
		t.Errorf("expected 0 rooms after close, got %d", len(rooms))
	}
}

// Peer tests

func TestPeerStateMapping(t *testing.T) {
	tests := []struct {
		webrtcState webrtc.PeerConnectionState
		expected    PeerState
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
			result := mapConnectionState(tt.webrtcState)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// Room tests

func TestRoomSetPublisherTwice(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	room, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	_, err = room.SetPublisher(ctx, "pub-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = room.SetPublisher(ctx, "pub-2")
	if err == nil {
		t.Error("expected error for second publisher")
	}
}

func TestRoomSubscriberCount(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	room, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if room.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", room.SubscriberCount())
	}
}

func TestRoomStats(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	room, err := sfu.CreateRoom("stream-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := room.Stats()
	if stats.StreamID != "stream-1" {
		t.Errorf("expected stream ID %q, got %q", "stream-1", stats.StreamID)
	}
	if stats.HasPublisher {
		t.Error("expected no publisher")
	}
	if stats.SubscriberCount != 0 {
		t.Errorf("expected 0 subscribers, got %d", stats.SubscriberCount)
	}
}

// TrackRouter tests

func TestTrackRouterTrackCount(t *testing.T) {
	logger := zap.NewNop()
	router := newTrackRouter(logger)

	if router.TrackCount() != 0 {
		t.Errorf("expected 0 tracks, got %d", router.TrackCount())
	}
}

func TestTrackRouterGetLocalTrackNotFound(t *testing.T) {
	logger := zap.NewNop()
	router := newTrackRouter(logger)

	_, ok := router.GetLocalTrack("nonexistent")
	if ok {
		t.Error("expected track not to be found")
	}
}

func TestTrackRouterClose(t *testing.T) {
	logger := zap.NewNop()
	router := newTrackRouter(logger)

	router.Close()

	// Should be safe to close again
	router.Close()
}

func TestTrackRouterGetTrackInfo(t *testing.T) {
	logger := zap.NewNop()
	router := newTrackRouter(logger)

	infos := router.GetTrackInfo()
	if len(infos) != 0 {
		t.Errorf("expected 0 track infos, got %d", len(infos))
	}
}

// InputChannel tests

func TestInputChannelStats(t *testing.T) {
	logger := zap.NewNop()
	ic := newInputChannel(nil, logger)

	stats := ic.GetStats()
	if stats.HasPublisherChannel {
		t.Error("expected no publisher channel")
	}
	if stats.SubscriberCount != 0 {
		t.Errorf("expected 0 subscribers, got %d", stats.SubscriberCount)
	}
}

func TestInputChannelClose(t *testing.T) {
	logger := zap.NewNop()
	ic := newInputChannel(nil, logger)

	ic.Close()

	// Should be safe to close again
	ic.Close()
}

// Signaling tests

func TestSignalingMessageTypes(t *testing.T) {
	if SignalingTypeOffer != "offer" {
		t.Errorf("expected %q, got %q", "offer", SignalingTypeOffer)
	}
	if SignalingTypeAnswer != "answer" {
		t.Errorf("expected %q, got %q", "answer", SignalingTypeAnswer)
	}
	if SignalingTypeCandidate != "candidate" {
		t.Errorf("expected %q, got %q", "candidate", SignalingTypeCandidate)
	}
	if SignalingTypeError != "error" {
		t.Errorf("expected %q, got %q", "error", SignalingTypeError)
	}
}

func TestNewOfferMessage(t *testing.T) {
	msg := NewOfferMessage("stream-1", "peer-1", "v=0...")

	if msg.Type != SignalingTypeOffer {
		t.Errorf("expected type %q, got %q", SignalingTypeOffer, msg.Type)
	}
	if msg.StreamID != "stream-1" {
		t.Errorf("expected stream ID %q, got %q", "stream-1", msg.StreamID)
	}
	if msg.PeerID != "peer-1" {
		t.Errorf("expected peer ID %q, got %q", "peer-1", msg.PeerID)
	}
	if msg.SDP != "v=0..." {
		t.Errorf("expected SDP %q, got %q", "v=0...", msg.SDP)
	}
}

func TestNewAnswerMessage(t *testing.T) {
	msg := NewAnswerMessage("stream-1", "peer-1", "v=0...")

	if msg.Type != SignalingTypeAnswer {
		t.Errorf("expected type %q, got %q", SignalingTypeAnswer, msg.Type)
	}
}

func TestNewErrorMessage(t *testing.T) {
	msg := NewErrorMessage("stream-1", "peer-1", "something went wrong")

	if msg.Type != SignalingTypeError {
		t.Errorf("expected type %q, got %q", SignalingTypeError, msg.Type)
	}
	if msg.Error != "something went wrong" {
		t.Errorf("expected error %q, got %q", "something went wrong", msg.Error)
	}
}

func TestParseSignalingMessage(t *testing.T) {
	original := NewOfferMessage("stream-1", "peer-1", "v=0...")
	data, err := original.ToJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := ParseSignalingMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Type != original.Type {
		t.Errorf("expected type %q, got %q", original.Type, parsed.Type)
	}
	if parsed.StreamID != original.StreamID {
		t.Errorf("expected stream ID %q, got %q", original.StreamID, parsed.StreamID)
	}
}

func TestParseSignalingMessageInvalid(t *testing.T) {
	_, err := ParseSignalingMessage([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestICECandidateJSON(t *testing.T) {
	sdpMid := "0"
	sdpMLineIndex := uint16(0)
	candidateJSON := &ICECandidateJSON{
		Candidate:     "candidate:1 1 UDP 2122252543 192.168.1.1 12345 typ host",
		SDPMid:        &sdpMid,
		SDPMLineIndex: &sdpMLineIndex,
	}

	init := candidateJSON.ToWebRTC()

	if init.Candidate != candidateJSON.Candidate {
		t.Errorf("expected candidate %q, got %q", candidateJSON.Candidate, init.Candidate)
	}
	if init.SDPMid == nil || *init.SDPMid != sdpMid {
		t.Error("expected SDPMid to match")
	}
}

func TestSignalingMessageToJSON(t *testing.T) {
	msg := NewOfferMessage("stream-1", "peer-1", "v=0...")

	data, err := msg.ToJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed["type"] != "offer" {
		t.Errorf("expected type %q, got %v", "offer", parsed["type"])
	}
}

func TestSignalingHandlerHandleUnknownType(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	handler := NewSignalingHandler(sfu)

	msg := &SignalingMessage{
		Type: "unknown",
	}

	err := handler.HandleMessage(msg)
	if err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestSignalingHandlerHandleOfferRoomNotFound(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	handler := NewSignalingHandler(sfu)

	msg := NewOfferMessage("nonexistent", "peer-1", "v=0...")

	err := handler.HandleMessage(msg)
	if err == nil {
		t.Error("expected error for nonexistent room")
	}
}

func TestSignalingHandlerHandleAnswerRoomNotFound(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	handler := NewSignalingHandler(sfu)

	msg := NewAnswerMessage("nonexistent", "peer-1", "v=0...")

	err := handler.HandleMessage(msg)
	if err == nil {
		t.Error("expected error for nonexistent room")
	}
}

func TestSignalingHandlerHandleCandidateRoomNotFound(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	handler := NewSignalingHandler(sfu)

	sdpMid := "0"
	msg := &SignalingMessage{
		Type:     SignalingTypeCandidate,
		StreamID: "nonexistent",
		PeerID:   "peer-1",
		Candidate: &ICECandidateJSON{
			Candidate: "candidate:1 1 UDP 2122252543 192.168.1.1 12345 typ host",
			SDPMid:    &sdpMid,
		},
	}

	err := handler.HandleMessage(msg)
	if err == nil {
		t.Error("expected error for nonexistent room")
	}
}

func TestSignalingHandlerHandleCandidateNil(t *testing.T) {
	sfu := createTestSFU(t)
	defer sfu.Close(context.Background())

	handler := NewSignalingHandler(sfu)

	// Nil candidate (end of candidates)
	msg := &SignalingMessage{
		Type:      SignalingTypeCandidate,
		StreamID:  "stream-1",
		PeerID:    "peer-1",
		Candidate: nil,
	}

	err := handler.HandleMessage(msg)
	if err != nil {
		t.Errorf("unexpected error for nil candidate: %v", err)
	}
}

// Helper functions

func createTestSFU(t *testing.T) *SFU {
	t.Helper()
	config := DefaultConfig()
	logger := zap.NewNop()

	sfu, err := New(config, logger)
	if err != nil {
		t.Fatalf("failed to create SFU: %v", err)
	}
	return sfu
}

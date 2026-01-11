package sfu

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignalingMessageType_Constants(t *testing.T) {
	assert.Equal(t, SignalingMessageType("offer"), SignalingTypeOffer)
	assert.Equal(t, SignalingMessageType("answer"), SignalingTypeAnswer)
	assert.Equal(t, SignalingMessageType("candidate"), SignalingTypeCandidate)
	assert.Equal(t, SignalingMessageType("ping"), SignalingTypePing)
	assert.Equal(t, SignalingMessageType("pong"), SignalingTypePong)
	assert.Equal(t, SignalingMessageType("error"), SignalingTypeError)
	assert.Equal(t, SignalingMessageType("join"), SignalingTypeJoin)
	assert.Equal(t, SignalingMessageType("leave"), SignalingTypeLeave)
	assert.Equal(t, SignalingMessageType("ready"), SignalingTypeReady)
}

func TestParseSignalingMessage_Offer(t *testing.T) {
	data := []byte(`{"type":"offer","stream_id":"stream-1","peer_id":"peer-1","sdp":"v=0\r\n..."}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeOffer, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, "v=0\r\n...", msg.SDP)
}

func TestParseSignalingMessage_Answer(t *testing.T) {
	data := []byte(`{"type":"answer","stream_id":"stream-1","peer_id":"peer-1","sdp":"v=0\r\n..."}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeAnswer, msg.Type)
	assert.Equal(t, "v=0\r\n...", msg.SDP)
}

func TestParseSignalingMessage_Candidate(t *testing.T) {
	data := []byte(`{
		"type":"candidate",
		"stream_id":"stream-1",
		"peer_id":"peer-1",
		"candidate":{
			"candidate":"candidate:1 1 udp 2130706431 192.168.1.1 50000 typ host",
			"sdpMid":"0",
			"sdpMLineIndex":0
		}
	}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeCandidate, msg.Type)
	require.NotNil(t, msg.Candidate)
	assert.Contains(t, msg.Candidate.Candidate, "candidate:1")
	assert.Equal(t, "0", *msg.Candidate.SDPMid)
	assert.Equal(t, uint16(0), *msg.Candidate.SDPMLineIndex)
}

func TestParseSignalingMessage_Join(t *testing.T) {
	data := []byte(`{"type":"join","stream_id":"stream-1","peer_id":"peer-1","role":"publisher"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeJoin, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, PeerRolePublisher, msg.Role)
}

func TestParseSignalingMessage_Leave(t *testing.T) {
	data := []byte(`{"type":"leave","stream_id":"stream-1","peer_id":"peer-1"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeLeave, msg.Type)
}

func TestParseSignalingMessage_Ping(t *testing.T) {
	data := []byte(`{"type":"ping"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypePing, msg.Type)
}

func TestParseSignalingMessage_Pong(t *testing.T) {
	data := []byte(`{"type":"pong"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypePong, msg.Type)
}

func TestParseSignalingMessage_Error(t *testing.T) {
	data := []byte(`{"type":"error","error":"something went wrong","error_code":"internal_error"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeError, msg.Type)
	assert.Equal(t, "something went wrong", msg.Error)
	assert.Equal(t, "internal_error", msg.ErrorCode)
}

func TestParseSignalingMessage_Ready(t *testing.T) {
	data := []byte(`{"type":"ready","stream_id":"stream-1","peer_id":"peer-1"}`)

	msg, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, SignalingTypeReady, msg.Type)
}

func TestParseSignalingMessage_InvalidJSON(t *testing.T) {
	data := []byte(`{invalid json}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_MissingType(t *testing.T) {
	data := []byte(`{"stream_id":"stream-1"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_UnknownType(t *testing.T) {
	data := []byte(`{"type":"unknown"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_OfferMissingSDP(t *testing.T) {
	data := []byte(`{"type":"offer","stream_id":"stream-1"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_AnswerMissingSDP(t *testing.T) {
	data := []byte(`{"type":"answer","stream_id":"stream-1"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_CandidateMissingCandidate(t *testing.T) {
	data := []byte(`{"type":"candidate","stream_id":"stream-1"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_JoinMissingStreamID(t *testing.T) {
	data := []byte(`{"type":"join","peer_id":"peer-1","role":"publisher"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestParseSignalingMessage_JoinMissingRole(t *testing.T) {
	data := []byte(`{"type":"join","stream_id":"stream-1","peer_id":"peer-1"}`)

	_, err := ParseSignalingMessage(data)
	assert.Error(t, err)
}

func TestSignalingMessage_ToJSON(t *testing.T) {
	msg := &SignalingMessage{
		Type:     SignalingTypeOffer,
		StreamID: "stream-1",
		PeerID:   "peer-1",
		SDP:      "v=0\r\n...",
	}

	data, err := msg.ToJSON()
	require.NoError(t, err)

	// Parse back
	var parsed SignalingMessage
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, msg.Type, parsed.Type)
	assert.Equal(t, msg.StreamID, parsed.StreamID)
	assert.Equal(t, msg.PeerID, parsed.PeerID)
	assert.Equal(t, msg.SDP, parsed.SDP)
}

func TestSignalingMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     SignalingMessage
		wantErr bool
	}{
		{
			name:    "empty type",
			msg:     SignalingMessage{},
			wantErr: true,
		},
		{
			name:    "valid offer",
			msg:     SignalingMessage{Type: SignalingTypeOffer, SDP: "v=0"},
			wantErr: false,
		},
		{
			name:    "offer without SDP",
			msg:     SignalingMessage{Type: SignalingTypeOffer},
			wantErr: true,
		},
		{
			name:    "valid answer",
			msg:     SignalingMessage{Type: SignalingTypeAnswer, SDP: "v=0"},
			wantErr: false,
		},
		{
			name:    "answer without SDP",
			msg:     SignalingMessage{Type: SignalingTypeAnswer},
			wantErr: true,
		},
		{
			name: "valid candidate",
			msg: SignalingMessage{
				Type:      SignalingTypeCandidate,
				Candidate: &ICECandidateJSON{Candidate: "..."},
			},
			wantErr: false,
		},
		{
			name:    "candidate without candidate",
			msg:     SignalingMessage{Type: SignalingTypeCandidate},
			wantErr: true,
		},
		{
			name: "valid join",
			msg: SignalingMessage{
				Type:     SignalingTypeJoin,
				StreamID: "stream-1",
				Role:     PeerRolePublisher,
			},
			wantErr: false,
		},
		{
			name:    "join without stream_id",
			msg:     SignalingMessage{Type: SignalingTypeJoin, Role: PeerRolePublisher},
			wantErr: true,
		},
		{
			name:    "join without role",
			msg:     SignalingMessage{Type: SignalingTypeJoin, StreamID: "stream-1"},
			wantErr: true,
		},
		{
			name:    "valid ping",
			msg:     SignalingMessage{Type: SignalingTypePing},
			wantErr: false,
		},
		{
			name:    "valid pong",
			msg:     SignalingMessage{Type: SignalingTypePong},
			wantErr: false,
		},
		{
			name:    "valid error",
			msg:     SignalingMessage{Type: SignalingTypeError, Error: "err"},
			wantErr: false,
		},
		{
			name:    "valid leave",
			msg:     SignalingMessage{Type: SignalingTypeLeave},
			wantErr: false,
		},
		{
			name:    "valid ready",
			msg:     SignalingMessage{Type: SignalingTypeReady},
			wantErr: false,
		},
		{
			name:    "unknown type",
			msg:     SignalingMessage{Type: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewOfferMessage(t *testing.T) {
	msg := NewOfferMessage("stream-1", "peer-1", "v=0")

	assert.Equal(t, SignalingTypeOffer, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, "v=0", msg.SDP)
}

func TestNewAnswerMessage(t *testing.T) {
	msg := NewAnswerMessage("stream-1", "peer-1", "v=0")

	assert.Equal(t, SignalingTypeAnswer, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, "v=0", msg.SDP)
}

func TestNewCandidateMessage(t *testing.T) {
	candidate := &ICECandidateJSON{Candidate: "candidate:1..."}
	msg := NewCandidateMessage("stream-1", "peer-1", candidate)

	assert.Equal(t, SignalingTypeCandidate, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, candidate, msg.Candidate)
}

func TestNewJoinMessage(t *testing.T) {
	msg := NewJoinMessage("stream-1", "peer-1", PeerRolePublisher)

	assert.Equal(t, SignalingTypeJoin, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, PeerRolePublisher, msg.Role)
}

func TestNewLeaveMessage(t *testing.T) {
	msg := NewLeaveMessage("stream-1", "peer-1")

	assert.Equal(t, SignalingTypeLeave, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
}

func TestNewReadyMessage(t *testing.T) {
	msg := NewReadyMessage("stream-1", "peer-1")

	assert.Equal(t, SignalingTypeReady, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
}

func TestNewPingMessage(t *testing.T) {
	msg := NewPingMessage()

	assert.Equal(t, SignalingTypePing, msg.Type)
}

func TestNewPongMessage(t *testing.T) {
	msg := NewPongMessage()

	assert.Equal(t, SignalingTypePong, msg.Type)
}

func TestNewErrorMessage(t *testing.T) {
	msg := NewErrorMessage("stream-1", "peer-1", ErrorCodeInternalError, "something went wrong")

	assert.Equal(t, SignalingTypeError, msg.Type)
	assert.Equal(t, "stream-1", msg.StreamID)
	assert.Equal(t, "peer-1", msg.PeerID)
	assert.Equal(t, ErrorCodeInternalError, msg.ErrorCode)
	assert.Equal(t, "something went wrong", msg.Error)
}

func TestErrorCodes(t *testing.T) {
	assert.Equal(t, "invalid_message", ErrorCodeInvalidMessage)
	assert.Equal(t, "room_not_found", ErrorCodeRoomNotFound)
	assert.Equal(t, "room_full", ErrorCodeRoomFull)
	assert.Equal(t, "publisher_exists", ErrorCodePublisherExists)
	assert.Equal(t, "peer_not_found", ErrorCodePeerNotFound)
	assert.Equal(t, "internal_error", ErrorCodeInternalError)
	assert.Equal(t, "unauthorized", ErrorCodeUnauthorized)
}

func TestICECandidateJSON_Struct(t *testing.T) {
	sdpMid := "0"
	sdpMLineIndex := uint16(0)
	usernameFragment := "frag"

	candidate := ICECandidateJSON{
		Candidate:        "candidate:1...",
		SDPMid:           &sdpMid,
		SDPMLineIndex:    &sdpMLineIndex,
		UsernameFragment: &usernameFragment,
	}

	assert.Equal(t, "candidate:1...", candidate.Candidate)
	assert.Equal(t, "0", *candidate.SDPMid)
	assert.Equal(t, uint16(0), *candidate.SDPMLineIndex)
	assert.Equal(t, "frag", *candidate.UsernameFragment)
}

func TestSignalingMessage_ToJSON_WithCandidate(t *testing.T) {
	sdpMid := "0"
	msg := &SignalingMessage{
		Type:     SignalingTypeCandidate,
		StreamID: "stream-1",
		PeerID:   "peer-1",
		Candidate: &ICECandidateJSON{
			Candidate: "candidate:1...",
			SDPMid:    &sdpMid,
		},
	}

	data, err := msg.ToJSON()
	require.NoError(t, err)

	// Parse back
	parsed, err := ParseSignalingMessage(data)
	require.NoError(t, err)

	assert.Equal(t, msg.Type, parsed.Type)
	require.NotNil(t, parsed.Candidate)
	assert.Equal(t, "candidate:1...", parsed.Candidate.Candidate)
	assert.Equal(t, "0", *parsed.Candidate.SDPMid)
}

func BenchmarkParseSignalingMessage(b *testing.B) {
	data := []byte(`{"type":"offer","stream_id":"stream-1","peer_id":"peer-1","sdp":"v=0\r\n..."}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseSignalingMessage(data)
	}
}

func BenchmarkSignalingMessage_ToJSON(b *testing.B) {
	msg := &SignalingMessage{
		Type:     SignalingTypeOffer,
		StreamID: "stream-1",
		PeerID:   "peer-1",
		SDP:      "v=0\r\n...",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = msg.ToJSON()
	}
}

func BenchmarkSignalingMessage_Validate(b *testing.B) {
	msg := &SignalingMessage{
		Type:     SignalingTypeOffer,
		StreamID: "stream-1",
		PeerID:   "peer-1",
		SDP:      "v=0\r\n...",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = msg.Validate()
	}
}

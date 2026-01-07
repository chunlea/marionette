package webrtc

import (
	"encoding/json"
	"testing"

	"github.com/pion/webrtc/v3"
)

func TestNewOfferMessage(t *testing.T) {
	sdp := "v=0\r\no=- 123 123 IN IP4 127.0.0.1\r\n"
	msg, err := NewOfferMessage(sdp)
	if err != nil {
		t.Fatalf("NewOfferMessage() error = %v", err)
	}

	if msg.Type != SignalTypeOffer {
		t.Errorf("Type = %s, want %s", msg.Type, SignalTypeOffer)
	}

	// Parse it back
	offer, err := msg.ParseOffer()
	if err != nil {
		t.Fatalf("ParseOffer() error = %v", err)
	}

	if offer.SDP != sdp {
		t.Errorf("SDP = %s, want %s", offer.SDP, sdp)
	}
}

func TestNewAnswerMessage(t *testing.T) {
	sdp := "v=0\r\no=- 456 456 IN IP4 127.0.0.1\r\n"
	msg, err := NewAnswerMessage(sdp)
	if err != nil {
		t.Fatalf("NewAnswerMessage() error = %v", err)
	}

	if msg.Type != SignalTypeAnswer {
		t.Errorf("Type = %s, want %s", msg.Type, SignalTypeAnswer)
	}

	// Parse it back
	answer, err := msg.ParseAnswer()
	if err != nil {
		t.Fatalf("ParseAnswer() error = %v", err)
	}

	if answer.SDP != sdp {
		t.Errorf("SDP = %s, want %s", answer.SDP, sdp)
	}
}

func TestNewCandidateMessage(t *testing.T) {
	candidate := &webrtc.ICECandidate{
		Foundation: "123",
		Priority:   1000,
		Address:    "192.168.1.1",
		Port:       12345,
		Typ:        webrtc.ICECandidateTypeHost,
	}

	msg, err := NewCandidateMessage(candidate)
	if err != nil {
		t.Fatalf("NewCandidateMessage() error = %v", err)
	}

	if msg.Type != SignalTypeCandidate {
		t.Errorf("Type = %s, want %s", msg.Type, SignalTypeCandidate)
	}

	// Parse it back
	parsed, err := msg.ParseCandidate()
	if err != nil {
		t.Fatalf("ParseCandidate() error = %v", err)
	}

	if parsed.Candidate == "" {
		t.Error("Candidate should not be empty")
	}
}

func TestNewCandidateMessage_Nil(t *testing.T) {
	_, err := NewCandidateMessage(nil)
	if err == nil {
		t.Error("expected error for nil candidate")
	}
}

func TestNewStreamInfoMessage(t *testing.T) {
	info := StreamInfoPayload{
		Width:      1920,
		Height:     1080,
		VideoCodec: "h264",
		AudioCodec: "opus",
		HasAudio:   true,
		StreamID:   "stream_123",
	}

	msg, err := NewStreamInfoMessage(info)
	if err != nil {
		t.Fatalf("NewStreamInfoMessage() error = %v", err)
	}

	if msg.Type != SignalTypeStreamInfo {
		t.Errorf("Type = %s, want %s", msg.Type, SignalTypeStreamInfo)
	}

	// Parse it back
	parsed, err := msg.ParseStreamInfo()
	if err != nil {
		t.Fatalf("ParseStreamInfo() error = %v", err)
	}

	if parsed.Width != info.Width {
		t.Errorf("Width = %d, want %d", parsed.Width, info.Width)
	}
	if parsed.Height != info.Height {
		t.Errorf("Height = %d, want %d", parsed.Height, info.Height)
	}
	if parsed.VideoCodec != info.VideoCodec {
		t.Errorf("VideoCodec = %s, want %s", parsed.VideoCodec, info.VideoCodec)
	}
	if parsed.AudioCodec != info.AudioCodec {
		t.Errorf("AudioCodec = %s, want %s", parsed.AudioCodec, info.AudioCodec)
	}
	if parsed.HasAudio != info.HasAudio {
		t.Errorf("HasAudio = %v, want %v", parsed.HasAudio, info.HasAudio)
	}
}

func TestNewErrorMessage(t *testing.T) {
	msg, err := NewErrorMessage("test_error", "something went wrong")
	if err != nil {
		t.Fatalf("NewErrorMessage() error = %v", err)
	}

	if msg.Type != SignalTypeError {
		t.Errorf("Type = %s, want %s", msg.Type, SignalTypeError)
	}

	// Parse it back
	parsed, err := msg.ParseError()
	if err != nil {
		t.Fatalf("ParseError() error = %v", err)
	}

	if parsed.Code != "test_error" {
		t.Errorf("Code = %s, want %s", parsed.Code, "test_error")
	}
	if parsed.Message != "something went wrong" {
		t.Errorf("Message = %s, want %s", parsed.Message, "something went wrong")
	}
}

func TestNewPingPongMessages(t *testing.T) {
	ping := NewPingMessage()
	if ping.Type != SignalTypePing {
		t.Errorf("Ping Type = %s, want %s", ping.Type, SignalTypePing)
	}

	pong := NewPongMessage()
	if pong.Type != SignalTypePong {
		t.Errorf("Pong Type = %s, want %s", pong.Type, SignalTypePong)
	}
}

func TestSignalMessage_Marshal_Unmarshal(t *testing.T) {
	original, _ := NewOfferMessage("test-sdp")

	// Marshal
	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal
	parsed, err := UnmarshalSignalMessage(data)
	if err != nil {
		t.Fatalf("UnmarshalSignalMessage() error = %v", err)
	}

	if parsed.Type != original.Type {
		t.Errorf("Type = %s, want %s", parsed.Type, original.Type)
	}

	// Verify payload
	offer, err := parsed.ParseOffer()
	if err != nil {
		t.Fatalf("ParseOffer() error = %v", err)
	}
	if offer.SDP != "test-sdp" {
		t.Errorf("SDP = %s, want %s", offer.SDP, "test-sdp")
	}
}

func TestParseWrongType(t *testing.T) {
	msg, _ := NewOfferMessage("test")

	if _, err := msg.ParseAnswer(); err == nil {
		t.Error("expected error parsing offer as answer")
	}
	if _, err := msg.ParseCandidate(); err == nil {
		t.Error("expected error parsing offer as candidate")
	}
	if _, err := msg.ParseStreamInfo(); err == nil {
		t.Error("expected error parsing offer as stream-info")
	}
	if _, err := msg.ParseError(); err == nil {
		t.Error("expected error parsing offer as error")
	}
}

func TestCandidatePayload_ToICECandidateInit(t *testing.T) {
	sdpMid := "0"
	sdpMLineIndex := uint16(0)

	payload := CandidatePayload{
		Candidate:     "candidate:123 1 udp 2122260223 192.168.1.1 12345 typ host",
		SDPMid:        &sdpMid,
		SDPMLineIndex: &sdpMLineIndex,
	}

	init := payload.ToICECandidateInit()

	if init.Candidate != payload.Candidate {
		t.Error("Candidate not converted correctly")
	}
	if init.SDPMid == nil || *init.SDPMid != sdpMid {
		t.Error("SDPMid not converted correctly")
	}
	if init.SDPMLineIndex == nil || *init.SDPMLineIndex != sdpMLineIndex {
		t.Error("SDPMLineIndex not converted correctly")
	}
}

func TestOfferPayload_ToSessionDescription(t *testing.T) {
	payload := OfferPayload{SDP: "v=0\r\n"}
	sd := payload.ToSessionDescription()

	if sd.Type != webrtc.SDPTypeOffer {
		t.Errorf("Type = %v, want %v", sd.Type, webrtc.SDPTypeOffer)
	}
	if sd.SDP != payload.SDP {
		t.Errorf("SDP = %s, want %s", sd.SDP, payload.SDP)
	}
}

func TestAnswerPayload_ToSessionDescription(t *testing.T) {
	payload := AnswerPayload{SDP: "v=0\r\n"}
	sd := payload.ToSessionDescription()

	if sd.Type != webrtc.SDPTypeAnswer {
		t.Errorf("Type = %v, want %v", sd.Type, webrtc.SDPTypeAnswer)
	}
	if sd.SDP != payload.SDP {
		t.Errorf("SDP = %s, want %s", sd.SDP, payload.SDP)
	}
}

func TestUnmarshalSignalMessage_Invalid(t *testing.T) {
	_, err := UnmarshalSignalMessage([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSignalMessageJSON_Format(t *testing.T) {
	msg, _ := NewOfferMessage("test-sdp")
	data, _ := msg.Marshal()

	// Check JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse as generic JSON: %v", err)
	}

	if parsed["type"] != "offer" {
		t.Errorf("type = %v, want offer", parsed["type"])
	}

	// payload should be an object
	_, ok := parsed["payload"].(map[string]interface{})
	if !ok {
		t.Error("payload should be an object")
	}
}

package streaming

import (
	"testing"
)

func TestStreamTypes(t *testing.T) {
	tests := []struct {
		name       string
		streamType StreamType
		expected   string
	}{
		{"desktop", StreamTypeDesktop, "desktop"},
		{"browser", StreamTypeBrowser, "browser"},
		{"ios", StreamTypeIOS, "ios"},
		{"android", StreamTypeAndroid, "android"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.streamType) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.streamType)
			}
		})
	}
}

func TestStreamStates(t *testing.T) {
	tests := []struct {
		name     string
		state    StreamState
		expected string
	}{
		{"pending", StreamStatePending, "pending"},
		{"starting", StreamStateStarting, "starting"},
		{"active", StreamStateActive, "active"},
		{"paused", StreamStatePaused, "paused"},
		{"stopping", StreamStateStopping, "stopping"},
		{"stopped", StreamStateStopped, "stopped"},
		{"error", StreamStateError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.state)
			}
		})
	}
}

func TestResolutionString(t *testing.T) {
	tests := []struct {
		name     string
		res      Resolution
		expected string
	}{
		{"auto", Resolution{0, 0}, "auto"},
		{"zero width", Resolution{0, 1080}, "auto"},
		{"zero height", Resolution{1920, 0}, "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.res.String()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestInputEventTypes(t *testing.T) {
	tests := []struct {
		name      string
		eventType InputEventType
		expected  string
	}{
		{"keydown", InputEventKeyDown, "keydown"},
		{"keyup", InputEventKeyUp, "keyup"},
		{"mousemove", InputEventMouseMove, "mousemove"},
		{"mousedown", InputEventMouseDown, "mousedown"},
		{"mouseup", InputEventMouseUp, "mouseup"},
		{"mousewheel", InputEventMouseWheel, "mousewheel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.eventType)
			}
		})
	}
}

func TestSignalingMessageTypes(t *testing.T) {
	tests := []struct {
		name        string
		messageType SignalingMessageType
		expected    string
	}{
		{"offer", SignalingOffer, "offer"},
		{"answer", SignalingAnswer, "answer"},
		{"candidate", SignalingCandidate, "candidate"},
		{"error", SignalingError, "error"},
		{"ping", SignalingPing, "ping"},
		{"pong", SignalingPong, "pong"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.messageType) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.messageType)
			}
		})
	}
}

func TestStreamOptions(t *testing.T) {
	opts := StreamOptions{
		SessionID:   "sess_123",
		RunnerID:    "run_456",
		Type:        StreamTypeDesktop,
		Resolution:  Resolution{1920, 1080},
		FrameRate:   30,
		BitRate:     4000,
		EnableAudio: true,
		EnableInput: true,
		Display:     ":99",
		ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	if opts.SessionID != "sess_123" {
		t.Errorf("expected session_id %q, got %q", "sess_123", opts.SessionID)
	}
	if opts.Type != StreamTypeDesktop {
		t.Errorf("expected type %q, got %q", StreamTypeDesktop, opts.Type)
	}
	if opts.Resolution.Width != 1920 || opts.Resolution.Height != 1080 {
		t.Errorf("expected resolution 1920x1080, got %dx%d", opts.Resolution.Width, opts.Resolution.Height)
	}
	if opts.FrameRate != 30 {
		t.Errorf("expected frame_rate 30, got %d", opts.FrameRate)
	}
	if !opts.EnableAudio {
		t.Error("expected enable_audio to be true")
	}
	if !opts.EnableInput {
		t.Error("expected enable_input to be true")
	}
	if len(opts.ICEServers) != 1 {
		t.Errorf("expected 1 ICE server, got %d", len(opts.ICEServers))
	}
}

func TestStreamInfo(t *testing.T) {
	info := StreamInfo{
		ID:           "strm_123",
		SessionID:    "sess_456",
		RunnerID:     "run_789",
		Type:         StreamTypeDesktop,
		State:        StreamStateActive,
		SignalingURL: "wss://signal.example.com/stream/strm_123",
		ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		Resolution:   Resolution{1920, 1080},
		FrameRate:    30,
		BitRate:      4000,
		AudioEnabled: true,
		InputEnabled: true,
	}

	if info.ID != "strm_123" {
		t.Errorf("expected id %q, got %q", "strm_123", info.ID)
	}
	if info.State != StreamStateActive {
		t.Errorf("expected state %q, got %q", StreamStateActive, info.State)
	}
	if info.SignalingURL != "wss://signal.example.com/stream/strm_123" {
		t.Errorf("unexpected signaling URL: %q", info.SignalingURL)
	}
}

func TestKeyEvent(t *testing.T) {
	event := KeyEvent{
		Key:  "KeyA",
		Code: "KeyA",
		Modifiers: KeyModifiers{
			Ctrl:  true,
			Shift: false,
			Alt:   false,
			Meta:  false,
		},
	}

	if event.Key != "KeyA" {
		t.Errorf("expected key %q, got %q", "KeyA", event.Key)
	}
	if !event.Modifiers.Ctrl {
		t.Error("expected Ctrl modifier to be true")
	}
	if event.Modifiers.Shift {
		t.Error("expected Shift modifier to be false")
	}
}

func TestMouseEvent(t *testing.T) {
	event := MouseEvent{
		X:      0.5,
		Y:      0.5,
		Button: 0,
		DeltaX: 0,
		DeltaY: -10,
	}

	if event.X != 0.5 || event.Y != 0.5 {
		t.Errorf("expected position (0.5, 0.5), got (%f, %f)", event.X, event.Y)
	}
	if event.Button != 0 {
		t.Errorf("expected button 0, got %d", event.Button)
	}
	if event.DeltaY != -10 {
		t.Errorf("expected deltaY -10, got %f", event.DeltaY)
	}
}

func TestICECandidate(t *testing.T) {
	candidate := ICECandidate{
		Candidate:     "candidate:123 1 udp 2130706431 192.168.1.1 12345 typ host",
		SDPMid:        "video",
		SDPMLineIndex: 0,
	}

	if candidate.SDPMid != "video" {
		t.Errorf("expected SDPMid %q, got %q", "video", candidate.SDPMid)
	}
	if candidate.SDPMLineIndex != 0 {
		t.Errorf("expected SDPMLineIndex 0, got %d", candidate.SDPMLineIndex)
	}
}

func TestSignalingMessage(t *testing.T) {
	// Test offer message
	offer := SignalingMessage{
		Type:      SignalingOffer,
		StreamID:  "strm_123",
		SessionID: "sess_456",
		SDP:       "v=0\r\no=...",
	}

	if offer.Type != SignalingOffer {
		t.Errorf("expected type %q, got %q", SignalingOffer, offer.Type)
	}
	if offer.SDP == "" {
		t.Error("expected SDP to be non-empty")
	}

	// Test candidate message
	candidate := SignalingMessage{
		Type:      SignalingCandidate,
		StreamID:  "strm_123",
		SessionID: "sess_456",
		Candidate: &ICECandidate{
			Candidate:     "candidate:123 1 udp 2130706431 192.168.1.1 12345 typ host",
			SDPMid:        "video",
			SDPMLineIndex: 0,
		},
	}

	if candidate.Candidate == nil {
		t.Error("expected Candidate to be non-nil")
	}
}

func TestInputEvent(t *testing.T) {
	// Test keyboard event
	keyEvent := InputEvent{
		Type: InputEventKeyDown,
		Key: &KeyEvent{
			Key:  "Enter",
			Code: "Enter",
			Modifiers: KeyModifiers{
				Ctrl: true,
			},
		},
	}

	if keyEvent.Type != InputEventKeyDown {
		t.Errorf("expected type %q, got %q", InputEventKeyDown, keyEvent.Type)
	}
	if keyEvent.Key == nil {
		t.Error("expected Key to be non-nil")
	}

	// Test mouse event
	mouseEvent := InputEvent{
		Type: InputEventMouseDown,
		Mouse: &MouseEvent{
			X:      0.25,
			Y:      0.75,
			Button: 0,
		},
	}

	if mouseEvent.Type != InputEventMouseDown {
		t.Errorf("expected type %q, got %q", InputEventMouseDown, mouseEvent.Type)
	}
	if mouseEvent.Mouse == nil {
		t.Error("expected Mouse to be non-nil")
	}
}

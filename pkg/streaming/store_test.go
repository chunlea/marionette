package streaming

import (
	"testing"
	"time"
)

func TestStreamModel(t *testing.T) {
	now := time.Now()
	stream := Stream{
		ID:               "strm_123",
		SessionID:        "sess_456",
		RunnerID:         "run_789",
		TenantID:         "tenant_abc",
		Type:             StreamTypeDesktop,
		State:            StreamStateActive,
		SignalingURL:     "wss://signal.example.com/stream/strm_123",
		ICEServers:       []ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
		Resolution:       Resolution{Width: 1920, Height: 1080},
		FrameRate:        30,
		BitRate:          4000,
		AudioEnabled:     true,
		InputEnabled:     true,
		ProviderName:     "selkies",
		ProviderStreamID: "provider_123",
		Error:            "",
		Metadata:         map[string]string{"key": "value"},
		CreatedAt:        now,
		UpdatedAt:        now,
		StartedAt:        &now,
		StoppedAt:        nil,
		ExpiresAt:        nil,
	}

	if stream.ID != "strm_123" {
		t.Errorf("expected id %q, got %q", "strm_123", stream.ID)
	}
	if stream.SessionID != "sess_456" {
		t.Errorf("expected session_id %q, got %q", "sess_456", stream.SessionID)
	}
	if stream.RunnerID != "run_789" {
		t.Errorf("expected runner_id %q, got %q", "run_789", stream.RunnerID)
	}
	if stream.TenantID != "tenant_abc" {
		t.Errorf("expected tenant_id %q, got %q", "tenant_abc", stream.TenantID)
	}
	if stream.Type != StreamTypeDesktop {
		t.Errorf("expected type %q, got %q", StreamTypeDesktop, stream.Type)
	}
	if stream.State != StreamStateActive {
		t.Errorf("expected state %q, got %q", StreamStateActive, stream.State)
	}
	if stream.ProviderName != "selkies" {
		t.Errorf("expected provider_name %q, got %q", "selkies", stream.ProviderName)
	}
	if stream.ProviderStreamID != "provider_123" {
		t.Errorf("expected provider_stream_id %q, got %q", "provider_123", stream.ProviderStreamID)
	}
	if stream.Resolution.Width != 1920 || stream.Resolution.Height != 1080 {
		t.Errorf("expected resolution 1920x1080, got %dx%d",
			stream.Resolution.Width, stream.Resolution.Height)
	}
	if stream.FrameRate != 30 {
		t.Errorf("expected frame_rate 30, got %d", stream.FrameRate)
	}
	if stream.BitRate != 4000 {
		t.Errorf("expected bitrate 4000, got %d", stream.BitRate)
	}
	if !stream.AudioEnabled {
		t.Error("expected audio_enabled to be true")
	}
	if !stream.InputEnabled {
		t.Error("expected input_enabled to be true")
	}
	if stream.StartedAt == nil {
		t.Error("expected started_at to be non-nil")
	}
	if stream.StoppedAt != nil {
		t.Error("expected stopped_at to be nil")
	}
	if len(stream.Metadata) != 1 || stream.Metadata["key"] != "value" {
		t.Errorf("unexpected metadata: %v", stream.Metadata)
	}
}

func TestCreateStreamParams(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour)
	params := CreateStreamParams{
		ID:           "strm_123",
		SessionID:    "sess_456",
		RunnerID:     "run_789",
		TenantID:     "tenant_abc",
		Type:         StreamTypeDesktop,
		ProviderName: "selkies",
		SignalingURL: "wss://signal.example.com",
		ICEServers:   []ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
		Resolution:   Resolution{Width: 1920, Height: 1080},
		FrameRate:    30,
		BitRate:      4000,
		AudioEnabled: true,
		InputEnabled: true,
		ExpiresAt:    &expiresAt,
		Metadata:     map[string]string{"key": "value"},
	}

	if params.ID != "strm_123" {
		t.Errorf("expected id %q, got %q", "strm_123", params.ID)
	}
	if params.ProviderName != "selkies" {
		t.Errorf("expected provider_name %q, got %q", "selkies", params.ProviderName)
	}
	if params.ExpiresAt == nil {
		t.Error("expected expires_at to be non-nil")
	}
}

func TestUpdateStreamParams(t *testing.T) {
	state := StreamStateActive
	signalingURL := "wss://new-signal.example.com"
	providerStreamID := "provider_456"
	resolution := Resolution{Width: 2560, Height: 1440}
	frameRate := 60
	bitRate := 8000
	errorStr := "test error"
	startedAt := time.Now()
	stoppedAt := time.Now()

	params := UpdateStreamParams{
		State:            &state,
		SignalingURL:     &signalingURL,
		ProviderStreamID: &providerStreamID,
		Resolution:       &resolution,
		FrameRate:        &frameRate,
		BitRate:          &bitRate,
		Error:            &errorStr,
		StartedAt:        &startedAt,
		StoppedAt:        &stoppedAt,
		Metadata:         map[string]string{"updated": "true"},
	}

	if params.State == nil || *params.State != StreamStateActive {
		t.Error("expected state to be set")
	}
	if params.SignalingURL == nil || *params.SignalingURL != signalingURL {
		t.Error("expected signaling_url to be set")
	}
	if params.ProviderStreamID == nil || *params.ProviderStreamID != providerStreamID {
		t.Error("expected provider_stream_id to be set")
	}
	if params.Resolution == nil || params.Resolution.Width != 2560 {
		t.Error("expected resolution to be set")
	}
	if params.FrameRate == nil || *params.FrameRate != 60 {
		t.Error("expected frame_rate to be set")
	}
	if params.BitRate == nil || *params.BitRate != 8000 {
		t.Error("expected bitrate to be set")
	}
	if params.Error == nil || *params.Error != errorStr {
		t.Error("expected error to be set")
	}
	if params.StartedAt == nil {
		t.Error("expected started_at to be set")
	}
	if params.StoppedAt == nil {
		t.Error("expected stopped_at to be set")
	}
	if params.Metadata == nil || params.Metadata["updated"] != "true" {
		t.Error("expected metadata to be set")
	}
}

func TestListStreamsParams(t *testing.T) {
	streamType := StreamTypeDesktop
	state := StreamStateActive

	params := ListStreamsParams{
		SessionID:  "sess_123",
		RunnerID:   "run_456",
		TenantID:   "tenant_abc",
		Type:       &streamType,
		State:      &state,
		ActiveOnly: true,
		Limit:      10,
		Offset:     0,
	}

	if params.SessionID != "sess_123" {
		t.Errorf("expected session_id %q, got %q", "sess_123", params.SessionID)
	}
	if params.RunnerID != "run_456" {
		t.Errorf("expected runner_id %q, got %q", "run_456", params.RunnerID)
	}
	if params.TenantID != "tenant_abc" {
		t.Errorf("expected tenant_id %q, got %q", "tenant_abc", params.TenantID)
	}
	if params.Type == nil || *params.Type != StreamTypeDesktop {
		t.Error("expected type to be set")
	}
	if params.State == nil || *params.State != StreamStateActive {
		t.Error("expected state to be set")
	}
	if !params.ActiveOnly {
		t.Error("expected active_only to be true")
	}
	if params.Limit != 10 {
		t.Errorf("expected limit 10, got %d", params.Limit)
	}
	if params.Offset != 0 {
		t.Errorf("expected offset 0, got %d", params.Offset)
	}
}

func TestListStreamsParamsDefaults(t *testing.T) {
	params := ListStreamsParams{}

	if params.SessionID != "" {
		t.Error("expected session_id to be empty")
	}
	if params.RunnerID != "" {
		t.Error("expected runner_id to be empty")
	}
	if params.TenantID != "" {
		t.Error("expected tenant_id to be empty")
	}
	if params.Type != nil {
		t.Error("expected type to be nil")
	}
	if params.State != nil {
		t.Error("expected state to be nil")
	}
	if params.ActiveOnly {
		t.Error("expected active_only to be false")
	}
	if params.Limit != 0 {
		t.Error("expected limit to be 0")
	}
	if params.Offset != 0 {
		t.Error("expected offset to be 0")
	}
}

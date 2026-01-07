package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/streaming/webrtc"
	"go.uber.org/zap"
)

// Android stream state constants.
const (
	AndroidStreamStateStarting = "starting"
	AndroidStreamStateActive   = "active"
	AndroidStreamStatePaused   = "paused"
	AndroidStreamStateClosing  = "closing"
	AndroidStreamStateClosed   = "closed"
	AndroidStreamStateFailed   = "failed"
)

// Android stream errors.
var (
	ErrStreamNotFound         = errors.New("android stream not found")
	ErrStreamAlreadyClosed    = errors.New("android stream is already closed")
	ErrStreamNotActive        = errors.New("android stream is not active")
	ErrStreamSessionRequired  = errors.New("session_id is required")
	ErrStreamDeviceRequired   = errors.New("device_serial is required")
	ErrStreamNoRunnerAttached = errors.New("session has no runner attached")
)

// AndroidStreamManagerInterface defines the interface for Android stream management.
type AndroidStreamManagerInterface interface {
	StartStream(ctx context.Context, opts StartStreamOptions) (*store.AndroidStream, error)
	GetStream(ctx context.Context, streamID string) (*store.AndroidStream, error)
	ListStreams(ctx context.Context, opts ListStreamsOptions) (*store.ListResult[store.AndroidStream], error)
	StopStream(ctx context.Context, streamID string) error
	UpdateStreamState(ctx context.Context, streamID string, state string, errorMsg *string) error
	UpdateStreamVideo(ctx context.Context, streamID string, width, height int, codec string) error
	ListDevices(ctx context.Context, sessionID string) ([]AndroidDevice, error)
	SendInput(ctx context.Context, streamID string, input AndroidInputEvent) error

	// WebRTC session management
	GetWebRTCSessionManager(ctx context.Context, streamID string) (*webrtc.SessionManager, error)
	GetRelay(ctx context.Context, streamID string) (*webrtc.Relay, error)
	GetStreamInfo(ctx context.Context, streamID string) (*webrtc.StreamInfoPayload, error)
}

// StartStreamOptions contains options for starting an Android stream.
type StartStreamOptions struct {
	SessionID    string
	DeviceSerial string
	MaxWidth     int
	MaxHeight    int
	MaxFPS       int
	Bitrate      int
	AudioEnabled bool
	TenantID     *string
}

// ListStreamsOptions contains options for listing Android streams.
type ListStreamsOptions struct {
	Limit         int
	Cursor        string
	SessionID     *string
	RunnerID      *string
	DeviceSerial  *string
	State         []string
	IncludeClosed bool
}

// AndroidDevice represents an Android device.
type AndroidDevice struct {
	Serial      string `json:"serial"`
	State       string `json:"state"`
	Product     string `json:"product,omitempty"`
	Model       string `json:"model,omitempty"`
	Device      string `json:"device,omitempty"`
	TransportID string `json:"transport_id,omitempty"`
}

// AndroidInputEvent represents an input event.
type AndroidInputEvent struct {
	Type      string  `json:"type"`
	Action    string  `json:"action,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	KeyCode   int     `json:"key_code,omitempty"`
	KeyAction string  `json:"key_action,omitempty"`
	Text      string  `json:"text,omitempty"`
}

// AndroidStreamManager handles Android stream lifecycle.
type AndroidStreamManager struct {
	store          store.Store
	sessionManager SessionManagerInterface
	logger         *zap.Logger

	// WebRTC session management (per stream)
	webrtcConfig          webrtc.Config
	webrtcRelays          map[string]*webrtc.Relay
	webrtcSessionManagers map[string]*webrtc.SessionManager
	relaysMu              sync.RWMutex
}

// AndroidStreamManagerOption is a functional option for AndroidStreamManager.
type AndroidStreamManagerOption func(*AndroidStreamManager)

// WithWebRTCConfig sets the WebRTC configuration.
func WithWebRTCConfig(cfg webrtc.Config) AndroidStreamManagerOption {
	return func(m *AndroidStreamManager) {
		m.webrtcConfig = cfg
	}
}

// NewAndroidStreamManager creates a new AndroidStreamManager.
func NewAndroidStreamManager(
	store store.Store,
	sessionManager SessionManagerInterface,
	logger *zap.Logger,
	opts ...AndroidStreamManagerOption,
) *AndroidStreamManager {
	m := &AndroidStreamManager{
		store:                 store,
		sessionManager:        sessionManager,
		logger:                logger.Named("android-stream-manager"),
		webrtcRelays:          make(map[string]*webrtc.Relay),
		webrtcSessionManagers: make(map[string]*webrtc.SessionManager),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Ensure WebRTC config has defaults
	m.webrtcConfig = m.webrtcConfig.WithDefaults()
	m.webrtcConfig.Logger = m.logger

	return m
}

// StartStream starts a new Android screen stream.
func (m *AndroidStreamManager) StartStream(ctx context.Context, opts StartStreamOptions) (*store.AndroidStream, error) {
	if opts.SessionID == "" {
		return nil, ErrStreamSessionRequired
	}
	if opts.DeviceSerial == "" {
		return nil, ErrStreamDeviceRequired
	}

	// Verify session exists and has a runner
	session, err := m.sessionManager.Get(ctx, opts.SessionID)
	if err != nil {
		return nil, err
	}
	if session.RunnerID == nil {
		return nil, ErrStreamNoRunnerAttached
	}

	// Generate stream ID
	streamID := id.New("astr")

	// Marshal options to JSON
	optionsJSON, err := json.Marshal(map[string]any{
		"max_width":     opts.MaxWidth,
		"max_height":    opts.MaxHeight,
		"max_fps":       opts.MaxFPS,
		"bitrate":       opts.Bitrate,
		"audio_enabled": opts.AudioEnabled,
	})
	if err != nil {
		return nil, err
	}

	// Create stream record
	stream := &store.AndroidStream{
		ID:           streamID,
		SessionID:    opts.SessionID,
		RunnerID:     session.RunnerID,
		DeviceSerial: opts.DeviceSerial,
		State:        AndroidStreamStateStarting,
		Options:      optionsJSON,
		TenantID:     opts.TenantID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := m.store.CreateAndroidStream(ctx, stream); err != nil {
		return nil, err
	}

	// Create WebRTC relay and session manager for this stream
	m.relaysMu.Lock()
	relay := webrtc.NewRelay(m.webrtcConfig)
	sessionMgr := webrtc.NewSessionManager(m.webrtcConfig, relay)
	m.webrtcRelays[streamID] = relay
	m.webrtcSessionManagers[streamID] = sessionMgr
	m.relaysMu.Unlock()

	m.logger.Info("android stream created",
		zap.String("stream_id", streamID),
		zap.String("session_id", opts.SessionID),
		zap.String("device", opts.DeviceSerial),
	)

	// Note: Actually starting the stream requires agent communication
	// which will be implemented in PR 4

	return stream, nil
}

// GetStream retrieves an Android stream by ID.
func (m *AndroidStreamManager) GetStream(ctx context.Context, streamID string) (*store.AndroidStream, error) {
	stream, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return nil, ErrStreamNotFound
	}
	return stream, nil
}

// ListStreams lists Android streams.
func (m *AndroidStreamManager) ListStreams(ctx context.Context, opts ListStreamsOptions) (*store.ListResult[store.AndroidStream], error) {
	storeOpts := store.ListAndroidStreamsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		SessionID:     opts.SessionID,
		RunnerID:      opts.RunnerID,
		DeviceSerial:  opts.DeviceSerial,
		State:         opts.State,
		IncludeClosed: opts.IncludeClosed,
	}

	return m.store.ListAndroidStreams(ctx, storeOpts)
}

// StopStream stops an active Android stream.
func (m *AndroidStreamManager) StopStream(ctx context.Context, streamID string) error {
	stream, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return ErrStreamNotFound
	}

	if stream.State == AndroidStreamStateClosed {
		return ErrStreamAlreadyClosed
	}

	// Update state to closing
	closingState := AndroidStreamStateClosing
	if err := m.store.UpdateAndroidStream(ctx, streamID, store.AndroidStreamUpdates{
		State: &closingState,
	}); err != nil {
		return err
	}

	// Close WebRTC relay and session manager
	m.relaysMu.Lock()
	if sessionMgr, ok := m.webrtcSessionManagers[streamID]; ok {
		_ = sessionMgr.Close()
		delete(m.webrtcSessionManagers, streamID)
	}
	if relay, ok := m.webrtcRelays[streamID]; ok {
		_ = relay.Close()
		delete(m.webrtcRelays, streamID)
	}
	m.relaysMu.Unlock()

	// Update state to closed
	now := time.Now()
	closedState := AndroidStreamStateClosed
	if err := m.store.UpdateAndroidStream(ctx, streamID, store.AndroidStreamUpdates{
		State:    &closedState,
		ClosedAt: &now,
	}); err != nil {
		return err
	}

	m.logger.Info("android stream stopped", zap.String("stream_id", streamID))

	// Note: Actually stopping the stream on the agent requires agent communication
	// which will be implemented in PR 4

	return nil
}

// UpdateStreamState updates the stream state.
func (m *AndroidStreamManager) UpdateStreamState(ctx context.Context, streamID string, state string, errorMsg *string) error {
	updates := store.AndroidStreamUpdates{
		State:        &state,
		ErrorMessage: errorMsg,
	}

	if state == AndroidStreamStateActive {
		now := time.Now()
		updates.StartedAt = &now
	}

	if state == AndroidStreamStateClosed || state == AndroidStreamStateFailed {
		now := time.Now()
		updates.ClosedAt = &now

		// Clean up WebRTC relay and session manager
		m.relaysMu.Lock()
		if sessionMgr, ok := m.webrtcSessionManagers[streamID]; ok {
			_ = sessionMgr.Close()
			delete(m.webrtcSessionManagers, streamID)
		}
		if relay, ok := m.webrtcRelays[streamID]; ok {
			_ = relay.Close()
			delete(m.webrtcRelays, streamID)
		}
		m.relaysMu.Unlock()
	}

	return m.store.UpdateAndroidStream(ctx, streamID, updates)
}

// UpdateStreamVideo updates the stream video configuration.
func (m *AndroidStreamManager) UpdateStreamVideo(ctx context.Context, streamID string, width, height int, codec string) error {
	return m.store.UpdateAndroidStream(ctx, streamID, store.AndroidStreamUpdates{
		Width:      &width,
		Height:     &height,
		VideoCodec: &codec,
	})
}

// ListDevices lists available Android devices for a session.
func (m *AndroidStreamManager) ListDevices(ctx context.Context, sessionID string) ([]AndroidDevice, error) {
	// Verify session exists
	_, err := m.sessionManager.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Note: Actually listing devices requires agent communication
	// which will be implemented in PR 4
	// For now, return empty list
	return []AndroidDevice{}, nil
}

// SendInput sends an input event to an Android device.
func (m *AndroidStreamManager) SendInput(ctx context.Context, streamID string, input AndroidInputEvent) error {
	stream, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return ErrStreamNotFound
	}

	if stream.State != AndroidStreamStateActive {
		return ErrStreamNotActive
	}

	// Note: Actually sending input requires agent communication
	// which will be implemented in PR 4

	m.logger.Debug("input event received",
		zap.String("stream_id", streamID),
		zap.String("type", input.Type),
	)

	return nil
}

// GetWebRTCSessionManager returns the WebRTC session manager for a stream.
func (m *AndroidStreamManager) GetWebRTCSessionManager(ctx context.Context, streamID string) (*webrtc.SessionManager, error) {
	// Verify stream exists
	_, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return nil, ErrStreamNotFound
	}

	m.relaysMu.RLock()
	sessionMgr, ok := m.webrtcSessionManagers[streamID]
	m.relaysMu.RUnlock()

	if !ok {
		// Create session manager if it doesn't exist
		m.relaysMu.Lock()
		relay := webrtc.NewRelay(m.webrtcConfig)
		sessionMgr = webrtc.NewSessionManager(m.webrtcConfig, relay)
		m.webrtcRelays[streamID] = relay
		m.webrtcSessionManagers[streamID] = sessionMgr
		m.relaysMu.Unlock()
	}

	return sessionMgr, nil
}

// GetRelay returns the WebRTC relay for a stream.
func (m *AndroidStreamManager) GetRelay(ctx context.Context, streamID string) (*webrtc.Relay, error) {
	// Verify stream exists
	_, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return nil, ErrStreamNotFound
	}

	m.relaysMu.RLock()
	relay, ok := m.webrtcRelays[streamID]
	m.relaysMu.RUnlock()

	if !ok {
		return nil, ErrStreamNotFound
	}

	return relay, nil
}

// GetStreamInfo returns stream info for WebRTC signaling.
func (m *AndroidStreamManager) GetStreamInfo(ctx context.Context, streamID string) (*webrtc.StreamInfoPayload, error) {
	stream, err := m.store.GetAndroidStream(ctx, streamID)
	if err != nil {
		return nil, ErrStreamNotFound
	}

	info := &webrtc.StreamInfoPayload{
		StreamID: streamID,
		HasAudio: false, // Will be updated from options
	}

	if stream.Width != nil {
		info.Width = *stream.Width
	}
	if stream.Height != nil {
		info.Height = *stream.Height
	}
	if stream.VideoCodec != nil {
		info.VideoCodec = *stream.VideoCodec
	}
	if stream.AudioCodec != nil {
		info.AudioCodec = *stream.AudioCodec
		info.HasAudio = true
	}

	// Parse options for audio_enabled
	var opts map[string]any
	if err := json.Unmarshal(stream.Options, &opts); err == nil {
		if audioEnabled, ok := opts["audio_enabled"].(bool); ok {
			info.HasAudio = audioEnabled
		}
	}

	return info, nil
}

// Close closes the manager and all WebRTC sessions.
func (m *AndroidStreamManager) Close() error {
	m.relaysMu.Lock()
	defer m.relaysMu.Unlock()

	for _, sessionMgr := range m.webrtcSessionManagers {
		_ = sessionMgr.Close()
	}
	for _, relay := range m.webrtcRelays {
		_ = relay.Close()
	}
	m.webrtcSessionManagers = make(map[string]*webrtc.SessionManager)
	m.webrtcRelays = make(map[string]*webrtc.Relay)

	return nil
}

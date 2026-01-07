// Package sfu implements a Selective Forwarding Unit (SFU) for WebRTC
// media streaming. It receives video/audio from publishers (like Selkies)
// and forwards to multiple subscribers (browsers).
package sfu

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

// Config contains SFU configuration options.
type Config struct {
	// ICEServers are STUN/TURN servers for WebRTC.
	ICEServers []webrtc.ICEServer

	// EnableTWCC enables Transport-Wide Congestion Control.
	EnableTWCC bool

	// EnableRTCPReports enables RTCP reports for congestion control.
	EnableRTCPReports bool

	// PLIInterval is the interval for sending PLI requests (in seconds).
	// Default: 3 seconds
	PLIInterval uint16

	// MaxBufferedAmount is the max bytes to buffer for data channels.
	// Default: 1MB
	MaxBufferedAmount uint64
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		EnableTWCC:        true,
		EnableRTCPReports: true,
		PLIInterval:       3,
		MaxBufferedAmount: 1024 * 1024, // 1MB
	}
}

// SFU manages WebRTC peer connections for selective forwarding.
type SFU struct {
	config Config
	logger *zap.Logger

	mu    sync.RWMutex
	rooms map[string]*Room // streamID -> Room

	// WebRTC API with interceptors
	api *webrtc.API
}

// New creates a new SFU instance.
func New(config Config, logger *zap.Logger) (*SFU, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create media engine
	mediaEngine := &webrtc.MediaEngine{}

	// Register default codecs
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("registering codecs: %w", err)
	}

	// Create interceptor registry
	interceptorRegistry := &interceptor.Registry{}

	// Register default interceptors
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, interceptorRegistry); err != nil {
		return nil, fmt.Errorf("registering interceptors: %w", err)
	}

	// Add PLI interval interceptor for better video quality
	if config.PLIInterval > 0 {
		pliFactory, err := intervalpli.NewReceiverInterceptor()
		if err != nil {
			return nil, fmt.Errorf("creating PLI interceptor: %w", err)
		}
		interceptorRegistry.Add(pliFactory)
	}

	// Create API with media engine and interceptors
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry),
	)

	return &SFU{
		config: config,
		logger: logger.Named("sfu"),
		rooms:  make(map[string]*Room),
		api:    api,
	}, nil
}

// CreateRoom creates a new room for a stream.
func (s *SFU) CreateRoom(streamID string) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[streamID]; exists {
		return nil, fmt.Errorf("room already exists for stream %s", streamID)
	}

	room := newRoom(streamID, s, s.logger)
	s.rooms[streamID] = room

	s.logger.Info("created room",
		zap.String("stream_id", streamID),
	)

	return room, nil
}

// GetRoom returns a room by stream ID.
func (s *SFU) GetRoom(streamID string) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, ok := s.rooms[streamID]
	return room, ok
}

// RemoveRoom removes a room and closes all connections.
func (s *SFU) RemoveRoom(ctx context.Context, streamID string) error {
	s.mu.Lock()
	room, ok := s.rooms[streamID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("room not found for stream %s", streamID)
	}
	delete(s.rooms, streamID)
	s.mu.Unlock()

	// Close the room
	if err := room.Close(ctx); err != nil {
		return fmt.Errorf("closing room: %w", err)
	}

	s.logger.Info("removed room",
		zap.String("stream_id", streamID),
	)

	return nil
}

// ListRooms returns all active room IDs.
func (s *SFU) ListRooms() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.rooms))
	for id := range s.rooms {
		ids = append(ids, id)
	}
	return ids
}

// Close closes the SFU and all rooms.
func (s *SFU) Close(ctx context.Context) error {
	s.mu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}
	s.rooms = make(map[string]*Room)
	s.mu.Unlock()

	var lastErr error
	for _, room := range rooms {
		if err := room.Close(ctx); err != nil {
			lastErr = err
			s.logger.Error("error closing room",
				zap.String("stream_id", room.streamID),
				zap.Error(err),
			)
		}
	}

	return lastErr
}

// webrtcConfig returns the WebRTC configuration.
func (s *SFU) webrtcConfig() webrtc.Configuration {
	return webrtc.Configuration{
		ICEServers: s.config.ICEServers,
	}
}

// Stats returns SFU statistics.
type Stats struct {
	RoomCount       int
	TotalPublishers int
	TotalSubscribers int
}

// GetStats returns current SFU statistics.
func (s *SFU) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{
		RoomCount: len(s.rooms),
	}

	for _, room := range s.rooms {
		if room.publisher != nil {
			stats.TotalPublishers++
		}
		stats.TotalSubscribers += len(room.subscribers)
	}

	return stats
}

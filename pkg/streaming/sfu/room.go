package sfu

import (
	"context"
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// Room represents a streaming room with one publisher and multiple subscribers.
type Room struct {
	streamID string
	sfu      *SFU
	logger   *zap.Logger

	mu          sync.RWMutex
	publisher   *Peer
	subscribers map[string]*Peer // peerID -> Peer

	// Tracks from publisher to be forwarded to subscribers
	router *TrackRouter

	// Data channel for input forwarding
	inputChannel *InputChannel

	closed bool
}

// newRoom creates a new Room.
func newRoom(streamID string, sfu *SFU, logger *zap.Logger) *Room {
	r := &Room{
		streamID:    streamID,
		sfu:         sfu,
		logger:      logger.Named("room").With(zap.String("stream_id", streamID)),
		subscribers: make(map[string]*Peer),
		router:      NewTrackRouter(logger),
	}

	r.inputChannel = NewInputChannel(r, logger)

	return r
}

// StreamID returns the room's stream ID.
func (r *Room) StreamID() string {
	return r.streamID
}

// SetPublisher sets the publisher for this room.
// Only one publisher is allowed per room.
func (r *Room) SetPublisher(ctx context.Context, peerID string) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, streaming.ErrRoomClosed
	}

	if r.publisher != nil {
		return nil, streaming.ErrPublisherExists
	}

	// Create peer connection
	conn, err := r.sfu.api.NewPeerConnection(r.sfu.config.WebRTCConfig())
	if err != nil {
		return nil, err
	}

	peer := NewPeer(PeerConfig{
		ID:     peerID,
		Role:   PeerRolePublisher,
		Logger: r.logger,
	}, conn)

	// Handle incoming tracks from publisher
	peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.handlePublisherTrack(track, receiver)
	})

	// Handle publisher state changes
	peer.OnStateChange(func(state PeerState) {
		r.handlePublisherStateChange(state)
	})

	// Handle data channel for input
	peer.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() == "input" {
			r.inputChannel.SetPublisherChannel(dc)
		}
	})

	r.publisher = peer

	r.logger.Info("publisher set",
		zap.String("peer_id", peerID),
	)

	return peer, nil
}

// GetPublisher returns the publisher peer.
func (r *Room) GetPublisher() *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.publisher
}

// HasPublisher returns whether the room has a publisher.
func (r *Room) HasPublisher() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.publisher != nil
}

// AddSubscriber adds a new subscriber to the room.
func (r *Room) AddSubscriber(ctx context.Context, peerID string) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, streaming.ErrRoomClosed
	}

	if _, exists := r.subscribers[peerID]; exists {
		return nil, streaming.ErrSubscriberExists
	}

	// Create peer connection
	conn, err := r.sfu.api.NewPeerConnection(r.sfu.config.WebRTCConfig())
	if err != nil {
		return nil, err
	}

	peer := NewPeer(PeerConfig{
		ID:     peerID,
		Role:   PeerRoleSubscriber,
		Logger: r.logger,
	}, conn)

	// Add existing tracks from publisher to subscriber
	if err := r.router.AddSubscriber(peer); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Create input data channel for this subscriber
	inputDC, err := peer.CreateDataChannel("input", nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Forward input from subscriber to publisher
	if err := r.inputChannel.AddSubscriberChannel(peerID, inputDC); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Handle subscriber state changes
	peer.OnStateChange(func(state PeerState) {
		r.handleSubscriberStateChange(peerID, state)
	})

	r.subscribers[peerID] = peer

	r.logger.Info("subscriber added",
		zap.String("peer_id", peerID),
		zap.Int("total_subscribers", len(r.subscribers)),
	)

	return peer, nil
}

// RemoveSubscriber removes a subscriber from the room.
func (r *Room) RemoveSubscriber(ctx context.Context, peerID string) error {
	r.mu.Lock()
	peer, ok := r.subscribers[peerID]
	if !ok {
		r.mu.Unlock()
		return streaming.ErrSubscriberNotFound
	}
	delete(r.subscribers, peerID)
	r.mu.Unlock()

	// Remove from track router
	r.router.RemoveSubscriber(peerID)

	// Remove from input channel
	r.inputChannel.RemoveSubscriberChannel(peerID)

	// Close peer connection
	if err := peer.Close(ctx); err != nil {
		return err
	}

	r.logger.Info("subscriber removed",
		zap.String("peer_id", peerID),
	)

	return nil
}

// GetSubscriber returns a subscriber by ID.
func (r *Room) GetSubscriber(peerID string) (*Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	peer, ok := r.subscribers[peerID]
	return peer, ok
}

// SubscriberCount returns the number of subscribers.
func (r *Room) SubscriberCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subscribers)
}

// SubscriberIDs returns the IDs of all subscribers.
func (r *Room) SubscriberIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.subscribers))
	for id := range r.subscribers {
		ids = append(ids, id)
	}
	return ids
}

// handlePublisherTrack handles a new track from the publisher.
func (r *Room) handlePublisherTrack(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	r.logger.Info("handling publisher track",
		zap.String("track_id", track.ID()),
		zap.String("kind", track.Kind().String()),
		zap.String("codec", track.Codec().MimeType),
	)

	// Add track to router which will forward to all subscribers
	if err := r.router.AddPublisherTrack(track, receiver); err != nil {
		r.logger.Error("failed to add publisher track",
			zap.Error(err),
		)
		return
	}

	// Add track to existing subscribers
	r.mu.RLock()
	subscribers := make([]*Peer, 0, len(r.subscribers))
	for _, sub := range r.subscribers {
		subscribers = append(subscribers, sub)
	}
	r.mu.RUnlock()

	for _, sub := range subscribers {
		if err := r.router.AddSubscriber(sub); err != nil {
			r.logger.Error("failed to add track to subscriber",
				zap.String("subscriber_id", sub.ID),
				zap.Error(err),
			)
		}
	}
}

// handlePublisherStateChange handles publisher state changes.
func (r *Room) handlePublisherStateChange(state PeerState) {
	r.logger.Info("publisher state changed",
		zap.String("state", string(state)),
	)

	switch state {
	case PeerStateFailed, PeerStateDisconnected, PeerStateClosed:
		// Publisher disconnected - notify subscribers or clean up
		r.mu.RLock()
		subCount := len(r.subscribers)
		r.mu.RUnlock()

		r.logger.Warn("publisher disconnected",
			zap.Int("subscribers_affected", subCount),
		)
	}
}

// handleSubscriberStateChange handles subscriber state changes.
func (r *Room) handleSubscriberStateChange(peerID string, state PeerState) {
	r.logger.Info("subscriber state changed",
		zap.String("peer_id", peerID),
		zap.String("state", string(state)),
	)

	switch state {
	case PeerStateFailed, PeerStateDisconnected, PeerStateClosed:
		// Clean up disconnected subscriber
		go func() {
			ctx := context.Background()
			if err := r.RemoveSubscriber(ctx, peerID); err != nil {
				r.logger.Debug("subscriber already removed",
					zap.String("peer_id", peerID),
				)
			}
		}()
	}
}

// Close closes the room and all connections.
func (r *Room) Close(ctx context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true

	publisher := r.publisher
	subscribers := make([]*Peer, 0, len(r.subscribers))
	for _, sub := range r.subscribers {
		subscribers = append(subscribers, sub)
	}
	r.publisher = nil
	r.subscribers = make(map[string]*Peer)
	r.mu.Unlock()

	var lastErr error

	// Close subscribers first
	for _, sub := range subscribers {
		if err := sub.Close(ctx); err != nil {
			lastErr = err
			r.logger.Error("error closing subscriber",
				zap.String("peer_id", sub.ID),
				zap.Error(err),
			)
		}
	}

	// Close publisher
	if publisher != nil {
		if err := publisher.Close(ctx); err != nil {
			lastErr = err
			r.logger.Error("error closing publisher",
				zap.Error(err),
			)
		}
	}

	// Close track router
	r.router.Close()

	// Close input channel
	r.inputChannel.Close()

	r.logger.Info("room closed")

	return lastErr
}

// IsClosed returns whether the room is closed.
func (r *Room) IsClosed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

// RoomStats contains room statistics.
type RoomStats struct {
	StreamID        string
	HasPublisher    bool
	SubscriberCount int
	TrackCount      int
}

// Stats returns room statistics.
func (r *Room) Stats() RoomStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return RoomStats{
		StreamID:        r.streamID,
		HasPublisher:    r.publisher != nil,
		SubscriberCount: len(r.subscribers),
		TrackCount:      r.router.TrackCount(),
	}
}

// GetTrackInfo returns information about all tracks in the room.
func (r *Room) GetTrackInfo() []TrackInfo {
	return r.router.GetTrackInfo()
}

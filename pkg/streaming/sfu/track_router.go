package sfu

import (
	"fmt"
	"io"
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming"
)

// TrackRouter handles forwarding tracks from publisher to subscribers.
type TrackRouter struct {
	logger *zap.Logger

	mu     sync.RWMutex
	tracks map[string]*routedTrack // trackID -> routedTrack
	closed bool
}

// routedTrack represents a track being routed from publisher to subscribers.
type routedTrack struct {
	remote      *webrtc.TrackRemote
	local       *webrtc.TrackLocalStaticRTP
	subscribers map[string]*webrtc.RTPSender // peerID -> sender
	stopCh      chan struct{}
}

// NewTrackRouter creates a new TrackRouter.
func NewTrackRouter(logger *zap.Logger) *TrackRouter {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TrackRouter{
		logger: logger.Named("track_router"),
		tracks: make(map[string]*routedTrack),
	}
}

// AddPublisherTrack adds a track from the publisher to be forwarded.
func (r *TrackRouter) AddPublisherTrack(remote *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return streaming.ErrTrackRouterClosed
	}

	trackID := remote.ID()
	if _, exists := r.tracks[trackID]; exists {
		return streaming.ErrTrackExists
	}

	// Create local track for forwarding
	local, err := webrtc.NewTrackLocalStaticRTP(
		remote.Codec().RTPCodecCapability,
		trackID,
		remote.StreamID(),
	)
	if err != nil {
		return fmt.Errorf("creating local track: %w", err)
	}

	track := &routedTrack{
		remote:      remote,
		local:       local,
		subscribers: make(map[string]*webrtc.RTPSender),
		stopCh:      make(chan struct{}),
	}

	r.tracks[trackID] = track

	// Start forwarding RTP packets
	go r.forwardTrack(track)

	r.logger.Info("added publisher track",
		zap.String("track_id", trackID),
		zap.String("kind", remote.Kind().String()),
		zap.String("codec", remote.Codec().MimeType),
	)

	return nil
}

// forwardTrack forwards RTP packets from remote to local track.
func (r *TrackRouter) forwardTrack(track *routedTrack) {
	defer func() {
		r.logger.Debug("track forwarding stopped",
			zap.String("track_id", track.remote.ID()),
		)
	}()

	buf := make([]byte, 1500) // MTU size
	for {
		select {
		case <-track.stopCh:
			return
		default:
		}

		n, _, err := track.remote.Read(buf)
		if err != nil {
			if err == io.EOF {
				r.logger.Debug("track EOF",
					zap.String("track_id", track.remote.ID()),
				)
				return
			}
			r.logger.Error("error reading from track",
				zap.String("track_id", track.remote.ID()),
				zap.Error(err),
			)
			return
		}

		if _, err := track.local.Write(buf[:n]); err != nil {
			if err == io.ErrClosedPipe {
				return
			}
			r.logger.Debug("error writing to local track",
				zap.String("track_id", track.remote.ID()),
				zap.Error(err),
			)
		}
	}
}

// AddSubscriber adds all current tracks to a subscriber.
func (r *TrackRouter) AddSubscriber(peer *Peer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return streaming.ErrTrackRouterClosed
	}

	for trackID, track := range r.tracks {
		// Check if already added
		if _, exists := track.subscribers[peer.ID]; exists {
			continue
		}

		sender, err := peer.AddTrack(track.local)
		if err != nil {
			return fmt.Errorf("adding track %s to subscriber: %w", trackID, err)
		}

		track.subscribers[peer.ID] = sender

		r.logger.Debug("added track to subscriber",
			zap.String("track_id", trackID),
			zap.String("peer_id", peer.ID),
		)
	}

	return nil
}

// RemoveSubscriber removes all tracks from a subscriber.
func (r *TrackRouter) RemoveSubscriber(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for trackID, track := range r.tracks {
		if _, exists := track.subscribers[peerID]; exists {
			delete(track.subscribers, peerID)
			r.logger.Debug("removed track from subscriber",
				zap.String("track_id", trackID),
				zap.String("peer_id", peerID),
			)
		}
	}
}

// TrackCount returns the number of tracks being routed.
func (r *TrackRouter) TrackCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tracks)
}

// GetLocalTrack returns the local track for a given track ID.
func (r *TrackRouter) GetLocalTrack(trackID string) (*webrtc.TrackLocalStaticRTP, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	track, ok := r.tracks[trackID]
	if !ok {
		return nil, false
	}
	return track.local, true
}

// Close stops all track forwarding.
func (r *TrackRouter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}
	r.closed = true

	for _, track := range r.tracks {
		close(track.stopCh)
	}
	r.tracks = make(map[string]*routedTrack)

	r.logger.Info("track router closed")
}

// IsClosed returns whether the router is closed.
func (r *TrackRouter) IsClosed() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.closed
}

// TrackInfo contains information about a routed track.
type TrackInfo struct {
	ID              string
	Kind            string
	Codec           string
	SubscriberCount int
}

// GetTrackInfo returns information about all tracks.
func (r *TrackRouter) GetTrackInfo() []TrackInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]TrackInfo, 0, len(r.tracks))
	for _, track := range r.tracks {
		infos = append(infos, TrackInfo{
			ID:              track.remote.ID(),
			Kind:            track.remote.Kind().String(),
			Codec:           track.remote.Codec().MimeType,
			SubscriberCount: len(track.subscribers),
		})
	}
	return infos
}

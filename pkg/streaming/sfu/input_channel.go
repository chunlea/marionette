package sfu

import (
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

// InputChannel handles forwarding input events from subscribers to the publisher.
// Subscribers send keyboard/mouse events via data channels, which are forwarded
// to the publisher (Selkies) for processing.
type InputChannel struct {
	room   *Room
	logger *zap.Logger

	mu                sync.RWMutex
	publisherChannel  *webrtc.DataChannel
	subscriberChannels map[string]*webrtc.DataChannel // peerID -> channel
	closed            bool
}

// newInputChannel creates a new InputChannel for a room.
func newInputChannel(room *Room, logger *zap.Logger) *InputChannel {
	return &InputChannel{
		room:               room,
		logger:             logger.Named("input_channel"),
		subscriberChannels: make(map[string]*webrtc.DataChannel),
	}
}

// SetPublisherChannel sets the data channel connected to the publisher (Selkies).
// Input events from subscribers will be forwarded to this channel.
func (ic *InputChannel) SetPublisherChannel(dc *webrtc.DataChannel) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.closed {
		return
	}

	ic.publisherChannel = dc

	dc.OnOpen(func() {
		ic.logger.Info("publisher input channel opened",
			zap.Uint16("id", *dc.ID()),
		)
	})

	dc.OnClose(func() {
		ic.logger.Info("publisher input channel closed")
		ic.mu.Lock()
		ic.publisherChannel = nil
		ic.mu.Unlock()
	})

	dc.OnError(func(err error) {
		ic.logger.Error("publisher input channel error",
			zap.Error(err),
		)
	})
}

// AddSubscriberChannel adds a subscriber's data channel for sending input events.
func (ic *InputChannel) AddSubscriberChannel(peerID string, dc *webrtc.DataChannel) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.closed {
		return
	}

	ic.subscriberChannels[peerID] = dc

	dc.OnOpen(func() {
		ic.logger.Debug("subscriber input channel opened",
			zap.String("peer_id", peerID),
			zap.Uint16("id", *dc.ID()),
		)
	})

	dc.OnClose(func() {
		ic.logger.Debug("subscriber input channel closed",
			zap.String("peer_id", peerID),
		)
		ic.mu.Lock()
		delete(ic.subscriberChannels, peerID)
		ic.mu.Unlock()
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		ic.forwardToPublisher(peerID, msg.Data)
	})

	dc.OnError(func(err error) {
		ic.logger.Error("subscriber input channel error",
			zap.String("peer_id", peerID),
			zap.Error(err),
		)
	})
}

// RemoveSubscriberChannel removes a subscriber's data channel.
func (ic *InputChannel) RemoveSubscriberChannel(peerID string) {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if dc, ok := ic.subscriberChannels[peerID]; ok {
		dc.Close()
		delete(ic.subscriberChannels, peerID)
		ic.logger.Debug("removed subscriber input channel",
			zap.String("peer_id", peerID),
		)
	}
}

// forwardToPublisher forwards input data from a subscriber to the publisher.
func (ic *InputChannel) forwardToPublisher(fromPeerID string, data []byte) {
	ic.mu.RLock()
	pubChannel := ic.publisherChannel
	ic.mu.RUnlock()

	if pubChannel == nil {
		ic.logger.Debug("no publisher channel, dropping input",
			zap.String("from_peer_id", fromPeerID),
			zap.Int("data_len", len(data)),
		)
		return
	}

	if pubChannel.ReadyState() != webrtc.DataChannelStateOpen {
		ic.logger.Debug("publisher channel not open, dropping input",
			zap.String("from_peer_id", fromPeerID),
			zap.String("state", pubChannel.ReadyState().String()),
		)
		return
	}

	if err := pubChannel.Send(data); err != nil {
		ic.logger.Error("failed to forward input to publisher",
			zap.String("from_peer_id", fromPeerID),
			zap.Error(err),
		)
	}
}

// Close closes all data channels.
func (ic *InputChannel) Close() {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ic.closed {
		return
	}
	ic.closed = true

	// Close subscriber channels
	for peerID, dc := range ic.subscriberChannels {
		dc.Close()
		ic.logger.Debug("closed subscriber input channel",
			zap.String("peer_id", peerID),
		)
	}
	ic.subscriberChannels = make(map[string]*webrtc.DataChannel)

	// Note: Don't close publisher channel here - it's managed by the peer connection

	ic.logger.Info("input channel closed")
}

// Stats returns input channel statistics.
type InputChannelStats struct {
	HasPublisherChannel  bool
	SubscriberCount      int
	PublisherChannelOpen bool
}

// GetStats returns current input channel statistics.
func (ic *InputChannel) GetStats() InputChannelStats {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	stats := InputChannelStats{
		HasPublisherChannel: ic.publisherChannel != nil,
		SubscriberCount:     len(ic.subscriberChannels),
	}

	if ic.publisherChannel != nil {
		stats.PublisherChannelOpen = ic.publisherChannel.ReadyState() == webrtc.DataChannelStateOpen
	}

	return stats
}

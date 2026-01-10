package sfu

import (
	"context"
	"fmt"
	"sync"

	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

// PeerRole defines the role of a peer connection.
type PeerRole string

const (
	// PeerRolePublisher indicates a peer that sends media.
	PeerRolePublisher PeerRole = "publisher"

	// PeerRoleSubscriber indicates a peer that receives media.
	PeerRoleSubscriber PeerRole = "subscriber"
)

// PeerState represents the connection state of a peer.
type PeerState string

const (
	PeerStateNew          PeerState = "new"
	PeerStateConnecting   PeerState = "connecting"
	PeerStateConnected    PeerState = "connected"
	PeerStateDisconnected PeerState = "disconnected"
	PeerStateFailed       PeerState = "failed"
	PeerStateClosed       PeerState = "closed"
)

// Peer wraps a WebRTC peer connection with SFU-specific functionality.
type Peer struct {
	ID         string
	Role       PeerRole
	Connection *webrtc.PeerConnection
	Logger     *zap.Logger

	mu           sync.RWMutex
	state        PeerState
	dataChannels map[string]*webrtc.DataChannel
	tracks       map[string]*webrtc.TrackLocalStaticRTP

	// Callbacks
	onTrack       func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	onICE         func(*webrtc.ICECandidate)
	onStateChange func(PeerState)
	onDataChannel func(*webrtc.DataChannel)
}

// PeerConfig contains configuration for creating a peer.
type PeerConfig struct {
	ID     string
	Role   PeerRole
	Logger *zap.Logger
}

// NewPeer creates a new Peer with the given WebRTC connection.
func NewPeer(config PeerConfig, conn *webrtc.PeerConnection) *Peer {
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	p := &Peer{
		ID:           config.ID,
		Role:         config.Role,
		Connection:   conn,
		Logger:       config.Logger.Named("peer").With(zap.String("peer_id", config.ID)),
		state:        PeerStateNew,
		dataChannels: make(map[string]*webrtc.DataChannel),
		tracks:       make(map[string]*webrtc.TrackLocalStaticRTP),
	}

	// Set up connection state handler
	conn.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		newState := mapConnectionState(state)
		p.setState(newState)
	})

	// Set up ICE candidate handler
	conn.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		p.mu.RLock()
		handler := p.onICE
		p.mu.RUnlock()
		if handler != nil {
			handler(candidate)
		}
	})

	// Set up track handler (for publishers)
	conn.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		p.Logger.Info("received track",
			zap.String("track_id", track.ID()),
			zap.String("kind", track.Kind().String()),
			zap.String("codec", track.Codec().MimeType),
		)
		p.mu.RLock()
		handler := p.onTrack
		p.mu.RUnlock()
		if handler != nil {
			handler(track, receiver)
		}
	})

	// Set up data channel handler
	conn.OnDataChannel(func(dc *webrtc.DataChannel) {
		p.Logger.Info("received data channel",
			zap.String("label", dc.Label()),
		)
		p.mu.Lock()
		p.dataChannels[dc.Label()] = dc
		handler := p.onDataChannel
		p.mu.Unlock()
		if handler != nil {
			handler(dc)
		}
	})

	return p
}

// mapConnectionState maps WebRTC connection state to PeerState.
func mapConnectionState(state webrtc.PeerConnectionState) PeerState {
	switch state {
	case webrtc.PeerConnectionStateNew:
		return PeerStateNew
	case webrtc.PeerConnectionStateConnecting:
		return PeerStateConnecting
	case webrtc.PeerConnectionStateConnected:
		return PeerStateConnected
	case webrtc.PeerConnectionStateDisconnected:
		return PeerStateDisconnected
	case webrtc.PeerConnectionStateFailed:
		return PeerStateFailed
	case webrtc.PeerConnectionStateClosed:
		return PeerStateClosed
	default:
		return PeerStateNew
	}
}

// setState updates the peer state and notifies listeners.
func (p *Peer) setState(state PeerState) {
	p.mu.Lock()
	oldState := p.state
	p.state = state
	handler := p.onStateChange
	p.mu.Unlock()

	if oldState != state {
		p.Logger.Info("connection state changed",
			zap.String("old_state", string(oldState)),
			zap.String("new_state", string(state)),
		)
		if handler != nil {
			handler(state)
		}
	}
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// OnTrack sets the callback for when a track is received.
func (p *Peer) OnTrack(handler func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onTrack = handler
}

// OnICECandidate sets the callback for ICE candidates.
func (p *Peer) OnICECandidate(handler func(*webrtc.ICECandidate)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onICE = handler
}

// OnStateChange sets the callback for state changes.
func (p *Peer) OnStateChange(handler func(PeerState)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStateChange = handler
}

// OnDataChannel sets the callback for data channels.
func (p *Peer) OnDataChannel(handler func(*webrtc.DataChannel)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onDataChannel = handler
}

// CreateOffer creates an SDP offer.
func (p *Peer) CreateOffer() (*webrtc.SessionDescription, error) {
	offer, err := p.Connection.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("creating offer: %w", err)
	}

	if err := p.Connection.SetLocalDescription(offer); err != nil {
		return nil, fmt.Errorf("setting local description: %w", err)
	}

	return &offer, nil
}

// CreateAnswer creates an SDP answer.
func (p *Peer) CreateAnswer() (*webrtc.SessionDescription, error) {
	answer, err := p.Connection.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("creating answer: %w", err)
	}

	if err := p.Connection.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("setting local description: %w", err)
	}

	return &answer, nil
}

// SetRemoteDescription sets the remote SDP.
func (p *Peer) SetRemoteDescription(sdp webrtc.SessionDescription) error {
	if err := p.Connection.SetRemoteDescription(sdp); err != nil {
		return fmt.Errorf("setting remote description: %w", err)
	}
	return nil
}

// AddICECandidate adds a remote ICE candidate.
func (p *Peer) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	if err := p.Connection.AddICECandidate(candidate); err != nil {
		return fmt.Errorf("adding ICE candidate: %w", err)
	}
	return nil
}

// AddTrack adds a local track to the peer connection.
func (p *Peer) AddTrack(track *webrtc.TrackLocalStaticRTP) (*webrtc.RTPSender, error) {
	sender, err := p.Connection.AddTrack(track)
	if err != nil {
		return nil, fmt.Errorf("adding track: %w", err)
	}

	p.mu.Lock()
	p.tracks[track.ID()] = track
	p.mu.Unlock()

	return sender, nil
}

// RemoveTrack removes a local track from the peer connection.
func (p *Peer) RemoveTrack(sender *webrtc.RTPSender) error {
	if err := p.Connection.RemoveTrack(sender); err != nil {
		return fmt.Errorf("removing track: %w", err)
	}
	return nil
}

// CreateDataChannel creates a new data channel.
func (p *Peer) CreateDataChannel(label string, opts *webrtc.DataChannelInit) (*webrtc.DataChannel, error) {
	dc, err := p.Connection.CreateDataChannel(label, opts)
	if err != nil {
		return nil, fmt.Errorf("creating data channel: %w", err)
	}

	p.mu.Lock()
	p.dataChannels[label] = dc
	p.mu.Unlock()

	return dc, nil
}

// GetDataChannel returns a data channel by label.
func (p *Peer) GetDataChannel(label string) (*webrtc.DataChannel, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	dc, ok := p.dataChannels[label]
	return dc, ok
}

// Close closes the peer connection.
func (p *Peer) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Close data channels
	for _, dc := range p.dataChannels {
		dc.Close()
	}
	p.dataChannels = make(map[string]*webrtc.DataChannel)

	// Close connection
	if err := p.Connection.Close(); err != nil {
		return fmt.Errorf("closing connection: %w", err)
	}

	p.state = PeerStateClosed
	return nil
}

// LocalDescription returns the local SDP.
func (p *Peer) LocalDescription() *webrtc.SessionDescription {
	return p.Connection.LocalDescription()
}

// RemoteDescription returns the remote SDP.
func (p *Peer) RemoteDescription() *webrtc.SessionDescription {
	return p.Connection.RemoteDescription()
}

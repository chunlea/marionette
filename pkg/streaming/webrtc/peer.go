package webrtc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// PeerState represents the state of a WebRTC peer connection.
type PeerState string

const (
	// PeerStateNew indicates a new peer that hasn't started connecting.
	PeerStateNew PeerState = "new"
	// PeerStateConnecting indicates the peer is in the process of connecting.
	PeerStateConnecting PeerState = "connecting"
	// PeerStateConnected indicates the peer is fully connected.
	PeerStateConnected PeerState = "connected"
	// PeerStateDisconnected indicates the peer has disconnected.
	PeerStateDisconnected PeerState = "disconnected"
	// PeerStateFailed indicates the peer connection failed.
	PeerStateFailed PeerState = "failed"
	// PeerStateClosed indicates the peer connection has been closed.
	PeerStateClosed PeerState = "closed"
)

// Peer wraps a pion WebRTC PeerConnection with additional state management.
type Peer struct {
	id     string
	config Config
	pc     *webrtc.PeerConnection
	logger *zap.Logger

	// Tracks
	videoTrack *webrtc.TrackLocalStaticRTP
	audioTrack *webrtc.TrackLocalStaticRTP

	// State
	state   PeerState
	stateMu sync.RWMutex

	// Callbacks
	onStateChange  func(PeerState)
	onICECandidate func(*webrtc.ICECandidate)

	// Lifecycle
	closed  bool
	closeMu sync.RWMutex
	doneCh  chan struct{}
}

// PeerOption is a function that configures a Peer.
type PeerOption func(*Peer)

// WithOnStateChange sets the state change callback.
func WithOnStateChange(fn func(PeerState)) PeerOption {
	return func(p *Peer) {
		p.onStateChange = fn
	}
}

// WithOnICECandidate sets the ICE candidate callback.
func WithOnICECandidate(fn func(*webrtc.ICECandidate)) PeerOption {
	return func(p *Peer) {
		p.onICECandidate = fn
	}
}

// NewPeer creates a new WebRTC peer connection.
func NewPeer(id string, config Config, opts ...PeerOption) (*Peer, error) {
	config = config.WithDefaults()

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create MediaEngine with codec support
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register codecs: %w", err)
	}

	// Create API with MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))

	// Create peer connection
	pc, err := api.NewPeerConnection(config.ToWebRTCConfiguration())
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peer := &Peer{
		id:     id,
		config: config,
		pc:     pc,
		logger: config.Logger.Named("peer").With(zap.String("peer_id", id)),
		state:  PeerStateNew,
		doneCh: make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(peer)
	}

	// Setup connection state handler
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		peer.handleConnectionStateChange(state)
	})

	// Setup ICE connection state handler
	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		peer.logger.Debug("ICE connection state changed",
			zap.String("state", state.String()),
		)
	})

	// Setup ICE candidate handler
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil && peer.onICECandidate != nil {
			peer.onICECandidate(candidate)
		}
	})

	// Setup ICE gathering state handler
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGathererState) {
		peer.logger.Debug("ICE gathering state changed",
			zap.String("state", state.String()),
		)
	})

	return peer, nil
}

// ID returns the peer ID.
func (p *Peer) ID() string {
	return p.id
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	return p.state
}

// AddVideoTrack adds a video track to the peer connection.
func (p *Peer) AddVideoTrack(codec string) (*webrtc.TrackLocalStaticRTP, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	codecCap := VideoCodecCapability(codec)
	track, err := webrtc.NewTrackLocalStaticRTP(codecCap, "video", "marionette-video")
	if err != nil {
		return nil, fmt.Errorf("failed to create video track: %w", err)
	}

	rtpSender, err := p.pc.AddTrack(track)
	if err != nil {
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}

	// Handle RTCP packets (PLI, NACK, etc.)
	go p.handleRTCP(rtpSender)

	p.videoTrack = track
	p.logger.Debug("video track added", zap.String("codec", codec))

	return track, nil
}

// AddAudioTrack adds an audio track to the peer connection.
func (p *Peer) AddAudioTrack(codec string) (*webrtc.TrackLocalStaticRTP, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	codecCap := AudioCodecCapability(codec)
	track, err := webrtc.NewTrackLocalStaticRTP(codecCap, "audio", "marionette-audio")
	if err != nil {
		return nil, fmt.Errorf("failed to create audio track: %w", err)
	}

	rtpSender, err := p.pc.AddTrack(track)
	if err != nil {
		return nil, fmt.Errorf("failed to add audio track: %w", err)
	}

	// Handle RTCP packets
	go p.handleRTCP(rtpSender)

	p.audioTrack = track
	p.logger.Debug("audio track added", zap.String("codec", codec))

	return track, nil
}

// VideoTrack returns the video track, or nil if not added.
func (p *Peer) VideoTrack() *webrtc.TrackLocalStaticRTP {
	return p.videoTrack
}

// AudioTrack returns the audio track, or nil if not added.
func (p *Peer) AudioTrack() *webrtc.TrackLocalStaticRTP {
	return p.audioTrack
}

// SetRemoteDescription sets the remote SDP description (offer or answer).
func (p *Peer) SetRemoteDescription(sdp webrtc.SessionDescription) error {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	if err := p.pc.SetRemoteDescription(sdp); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	p.logger.Debug("remote description set", zap.String("type", sdp.Type.String()))
	return nil
}

// CreateOffer creates an SDP offer.
func (p *Peer) CreateOffer() (*webrtc.SessionDescription, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create offer: %w", err)
	}

	if err := p.pc.SetLocalDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	p.logger.Debug("offer created")
	return &offer, nil
}

// CreateAnswer creates an SDP answer.
func (p *Peer) CreateAnswer() (*webrtc.SessionDescription, error) {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return nil, errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	if err := p.pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	p.logger.Debug("answer created")
	return &answer, nil
}

// AddICECandidate adds a remote ICE candidate.
func (p *Peer) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return errors.New("peer is closed")
	}
	p.closeMu.RUnlock()

	if err := p.pc.AddICECandidate(candidate); err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	p.logger.Debug("ICE candidate added")
	return nil
}

// LocalDescription returns the local SDP description.
func (p *Peer) LocalDescription() *webrtc.SessionDescription {
	return p.pc.LocalDescription()
}

// WaitForConnection waits for the peer to reach connected state.
func (p *Peer) WaitForConnection(ctx context.Context) error {
	timeout := p.config.PeerConnectionTimeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("connection timeout after %v", timeout)
		case <-p.doneCh:
			return errors.New("peer closed")
		default:
		}

		state := p.State()
		switch state {
		case PeerStateConnected:
			return nil
		case PeerStateFailed:
			return errors.New("peer connection failed")
		case PeerStateClosed:
			return errors.New("peer closed")
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// Close closes the peer connection.
func (p *Peer) Close() error {
	p.closeMu.Lock()
	if p.closed {
		p.closeMu.Unlock()
		return nil
	}
	p.closed = true
	close(p.doneCh)
	p.closeMu.Unlock()

	p.setState(PeerStateClosed)

	if err := p.pc.Close(); err != nil {
		p.logger.Error("error closing peer connection", zap.Error(err))
		return err
	}

	p.logger.Info("peer closed")
	return nil
}

// Done returns a channel that's closed when the peer is closed.
func (p *Peer) Done() <-chan struct{} {
	return p.doneCh
}

// handleConnectionStateChange handles PeerConnection state changes.
func (p *Peer) handleConnectionStateChange(state webrtc.PeerConnectionState) {
	p.logger.Info("connection state changed",
		zap.String("state", state.String()),
	)

	var peerState PeerState
	switch state {
	case webrtc.PeerConnectionStateNew:
		peerState = PeerStateNew
	case webrtc.PeerConnectionStateConnecting:
		peerState = PeerStateConnecting
	case webrtc.PeerConnectionStateConnected:
		peerState = PeerStateConnected
	case webrtc.PeerConnectionStateDisconnected:
		peerState = PeerStateDisconnected
	case webrtc.PeerConnectionStateFailed:
		peerState = PeerStateFailed
	case webrtc.PeerConnectionStateClosed:
		peerState = PeerStateClosed
	default:
		return
	}

	p.setState(peerState)
}

// setState updates the peer state and calls the callback.
func (p *Peer) setState(state PeerState) {
	p.stateMu.Lock()
	if p.state == state {
		p.stateMu.Unlock()
		return
	}
	p.state = state
	p.stateMu.Unlock()

	if p.onStateChange != nil {
		p.onStateChange(state)
	}
}

// handleRTCP handles incoming RTCP packets (PLI, NACK, etc.).
func (p *Peer) handleRTCP(sender *webrtc.RTPSender) {
	for {
		select {
		case <-p.doneCh:
			return
		default:
		}

		packets, _, err := sender.ReadRTCP()
		if err != nil {
			// Connection closed
			return
		}

		for _, pkt := range packets {
			p.logger.Debug("received RTCP packet",
				zap.String("type", fmt.Sprintf("%T", pkt)),
			)
		}
	}
}

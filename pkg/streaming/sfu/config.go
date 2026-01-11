// Package sfu implements a Selective Forwarding Unit (SFU) for WebRTC
// media streaming. It receives video/audio from publishers and forwards
// to multiple subscribers with minimal latency.
package sfu

import "github.com/pion/webrtc/v4"

// Config contains SFU configuration options.
type Config struct {
	// ICEServers are STUN/TURN servers for WebRTC.
	ICEServers []webrtc.ICEServer

	// EnableTWCC enables Transport-Wide Congestion Control.
	EnableTWCC bool

	// EnableRTCPReports enables RTCP reports for congestion control.
	EnableRTCPReports bool

	// PLIInterval is the interval for sending PLI requests (in seconds).
	// PLI (Picture Loss Indication) requests keyframes from the publisher.
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

// Validate validates the configuration.
func (c *Config) Validate() error {
	// PLIInterval of 0 disables PLI
	// No other required validations for now
	return nil
}

// WebRTCConfig returns a webrtc.Configuration from this Config.
func (c *Config) WebRTCConfig() webrtc.Configuration {
	return webrtc.Configuration{
		ICEServers: c.ICEServers,
	}
}

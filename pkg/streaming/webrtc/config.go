package webrtc

import (
	"errors"
	"time"

	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"
)

// Default configuration values.
const (
	// DefaultICEConnectionTimeout is the default timeout for ICE connection.
	DefaultICEConnectionTimeout = 30 * time.Second

	// DefaultPeerConnectionTimeout is the default timeout for peer connection setup.
	DefaultPeerConnectionTimeout = 60 * time.Second

	// DefaultVideoMTU is the default MTU for video RTP packets.
	DefaultVideoMTU = 1400

	// DefaultAudioMTU is the default MTU for audio RTP packets.
	DefaultAudioMTU = 1200

	// DefaultVideoClockRate is the standard H.264 clock rate.
	DefaultVideoClockRate = 90000

	// DefaultAudioClockRate is the standard Opus clock rate.
	DefaultAudioClockRate = 48000

	// DefaultAudioChannels is the default number of audio channels.
	DefaultAudioChannels = 2
)

// Default STUN servers (Google's public STUN servers).
var DefaultSTUNServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
}

// Config holds WebRTC relay configuration.
type Config struct {
	// STUN servers for NAT traversal.
	STUNServers []string

	// TURN servers for relay (optional).
	TURNServers []TURNServer

	// ICE connection timeout.
	ICEConnectionTimeout time.Duration

	// Peer connection setup timeout.
	PeerConnectionTimeout time.Duration

	// Video MTU for RTP packetization.
	VideoMTU int

	// Audio MTU for RTP packetization.
	AudioMTU int

	// Video clock rate (default: 90000 for H.264).
	VideoClockRate uint32

	// Audio clock rate (default: 48000 for Opus).
	AudioClockRate uint32

	// Audio channels (default: 2 for stereo).
	AudioChannels int

	// Logger for WebRTC operations.
	Logger *zap.Logger
}

// TURNServer represents TURN server configuration.
type TURNServer struct {
	// URLs is the TURN server URL (e.g., "turn:turn.example.com:3478").
	URLs []string

	// Username for TURN authentication.
	Username string

	// Credential for TURN authentication.
	Credential string

	// CredentialType specifies the credential type (default: password).
	CredentialType webrtc.ICECredentialType
}

// WithDefaults returns a copy of the config with default values for unset fields.
func (c Config) WithDefaults() Config {
	if len(c.STUNServers) == 0 {
		c.STUNServers = DefaultSTUNServers
	}
	if c.ICEConnectionTimeout == 0 {
		c.ICEConnectionTimeout = DefaultICEConnectionTimeout
	}
	if c.PeerConnectionTimeout == 0 {
		c.PeerConnectionTimeout = DefaultPeerConnectionTimeout
	}
	if c.VideoMTU == 0 {
		c.VideoMTU = DefaultVideoMTU
	}
	if c.AudioMTU == 0 {
		c.AudioMTU = DefaultAudioMTU
	}
	if c.VideoClockRate == 0 {
		c.VideoClockRate = DefaultVideoClockRate
	}
	if c.AudioClockRate == 0 {
		c.AudioClockRate = DefaultAudioClockRate
	}
	if c.AudioChannels == 0 {
		c.AudioChannels = DefaultAudioChannels
	}
	if c.Logger == nil {
		c.Logger = zap.NewNop()
	}
	return c
}

// Validate checks if the configuration is valid.
func (c Config) Validate() error {
	if c.ICEConnectionTimeout < 0 {
		return errors.New("ICE connection timeout cannot be negative")
	}
	if c.PeerConnectionTimeout < 0 {
		return errors.New("peer connection timeout cannot be negative")
	}
	if c.VideoMTU < 500 || c.VideoMTU > 65535 {
		return errors.New("video MTU must be between 500 and 65535")
	}
	if c.AudioMTU < 200 || c.AudioMTU > 65535 {
		return errors.New("audio MTU must be between 200 and 65535")
	}
	if c.VideoClockRate == 0 {
		return errors.New("video clock rate cannot be zero")
	}
	if c.AudioClockRate == 0 {
		return errors.New("audio clock rate cannot be zero")
	}
	return nil
}

// ToWebRTCConfiguration converts the config to a pion WebRTC configuration.
func (c Config) ToWebRTCConfiguration() webrtc.Configuration {
	var iceServers []webrtc.ICEServer

	// Add STUN servers
	if len(c.STUNServers) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: c.STUNServers,
		})
	}

	// Add TURN servers
	for _, turn := range c.TURNServers {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:           turn.URLs,
			Username:       turn.Username,
			Credential:     turn.Credential,
			CredentialType: turn.CredentialType,
		})
	}

	return webrtc.Configuration{
		ICEServers: iceServers,
	}
}

// VideoCodecCapability returns the RTP codec capability for the specified video codec.
func VideoCodecCapability(codec string) webrtc.RTPCodecCapability {
	switch codec {
	case "h264", "H264":
		return webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   DefaultVideoClockRate,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		}
	case "h265", "H265", "hevc", "HEVC":
		// H.265/HEVC (not widely supported in browsers yet)
		return webrtc.RTPCodecCapability{
			MimeType:  "video/H265",
			ClockRate: DefaultVideoClockRate,
		}
	case "vp8", "VP8":
		return webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: DefaultVideoClockRate,
		}
	case "vp9", "VP9":
		return webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP9,
			ClockRate: DefaultVideoClockRate,
		}
	case "av1", "AV1":
		return webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeAV1,
			ClockRate: DefaultVideoClockRate,
		}
	default:
		// Default to H.264
		return webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   DefaultVideoClockRate,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		}
	}
}

// AudioCodecCapability returns the RTP codec capability for the specified audio codec.
func AudioCodecCapability(codec string) webrtc.RTPCodecCapability {
	switch codec {
	case "opus", "OPUS":
		return webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   DefaultAudioClockRate,
			Channels:    DefaultAudioChannels,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		}
	case "aac", "AAC":
		// AAC is not natively supported in WebRTC, typically needs transcoding
		return webrtc.RTPCodecCapability{
			MimeType:  "audio/AAC",
			ClockRate: DefaultAudioClockRate,
			Channels:  DefaultAudioChannels,
		}
	case "pcmu", "PCMU":
		return webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMU,
			ClockRate: 8000,
			Channels:  1,
		}
	case "pcma", "PCMA":
		return webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypePCMA,
			ClockRate: 8000,
			Channels:  1,
		}
	default:
		// Default to Opus
		return webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   DefaultAudioClockRate,
			Channels:    DefaultAudioChannels,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		}
	}
}

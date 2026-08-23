// Frozen subsystem. Excluded from the default build (decision D1):
// build with -tags streaming_extra to compile it.
//go:build streaming_extra

// Package scrcpy provides an Android streaming provider using scrcpy.
// scrcpy is a free and open-source screen mirroring application that
// provides high-performance, low-latency video streaming from Android devices.
package scrcpy

import (
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/streaming/android"
)

// Config contains configuration for the scrcpy provider.
type Config struct {
	// ScrcpyServerPath is the path to scrcpy-server binary/jar.
	// If empty, attempts to find it in standard locations or download it.
	ScrcpyServerPath string

	// ScrcpyServerVersion is the version of the scrcpy-server.
	// If empty, will be auto-detected from the binary.
	ScrcpyServerVersion string

	// ADBPath is the path to the adb binary.
	// If empty, uses "adb" from PATH.
	ADBPath string

	// ADBClient is an existing ADB client to use.
	// If nil, a new one will be created with ADBPath.
	ADBClient android.ADBClient

	// BasePort is the starting port for scrcpy server connections.
	// Each stream gets a unique port starting from BasePort.
	// Default: 27183 (scrcpy default)
	BasePort int

	// ServerStartTimeout is the timeout for starting the scrcpy server on the device.
	// Default: 30 seconds
	ServerStartTimeout time.Duration

	// ConnectionTimeout is the timeout for connecting to the scrcpy server.
	// Default: 10 seconds
	ConnectionTimeout time.Duration

	// VideoBitrate is the default video bitrate in bits per second.
	// Default: 8 Mbps
	VideoBitrate int

	// MaxFPS is the default maximum frames per second.
	// Default: 60
	MaxFPS int

	// MaxSize is the default maximum dimension (width or height).
	// Default: 0 (no limit, use device resolution)
	MaxSize int

	// VideoCodec is the default video codec.
	// Options: "h264", "h265", "av1"
	// Default: "h264"
	VideoCodec string

	// AudioCodec is the default audio codec.
	// Options: "opus", "aac", "flac", "raw"
	// Default: "opus"
	AudioCodec string

	// AudioEnabled enables audio streaming by default.
	// Requires scrcpy 2.0+
	// Default: false
	AudioEnabled bool

	// Logger is the logger for the provider.
	// If nil, a no-op logger is used.
	Logger *zap.Logger
}

// Default configuration values.
const (
	DefaultBasePort           = 27183
	DefaultServerStartTimeout = 30 * time.Second
	DefaultConnectionTimeout  = 10 * time.Second
	DefaultVideoBitrate       = 8_000_000 // 8 Mbps
	DefaultMaxFPS             = 60
	DefaultVideoCodec         = "h264"
	DefaultAudioCodec         = "opus"
)

// WithDefaults returns a copy of Config with default values applied.
func (c Config) WithDefaults() Config {
	if c.BasePort == 0 {
		c.BasePort = DefaultBasePort
	}
	if c.ServerStartTimeout == 0 {
		c.ServerStartTimeout = DefaultServerStartTimeout
	}
	if c.ConnectionTimeout == 0 {
		c.ConnectionTimeout = DefaultConnectionTimeout
	}
	if c.VideoBitrate == 0 {
		c.VideoBitrate = DefaultVideoBitrate
	}
	if c.MaxFPS == 0 {
		c.MaxFPS = DefaultMaxFPS
	}
	if c.VideoCodec == "" {
		c.VideoCodec = DefaultVideoCodec
	}
	if c.AudioCodec == "" {
		c.AudioCodec = DefaultAudioCodec
	}
	if c.Logger == nil {
		c.Logger = zap.NewNop()
	}
	return c
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.BasePort < 1 || c.BasePort > 65535 {
		return &android.InvalidOptionsError{
			Field:   "base_port",
			Message: "base port must be between 1 and 65535",
		}
	}
	if c.ServerStartTimeout < 0 {
		return &android.InvalidOptionsError{
			Field:   "server_start_timeout",
			Message: "server start timeout must be non-negative",
		}
	}
	if c.ConnectionTimeout < 0 {
		return &android.InvalidOptionsError{
			Field:   "connection_timeout",
			Message: "connection timeout must be non-negative",
		}
	}
	validCodecs := map[string]bool{"h264": true, "h265": true, "av1": true}
	if c.VideoCodec != "" && !validCodecs[c.VideoCodec] {
		return &android.InvalidOptionsError{
			Field:   "video_codec",
			Message: "video codec must be h264, h265, or av1",
		}
	}
	validAudioCodecs := map[string]bool{"opus": true, "aac": true, "flac": true, "raw": true}
	if c.AudioCodec != "" && !validAudioCodecs[c.AudioCodec] {
		return &android.InvalidOptionsError{
			Field:   "audio_codec",
			Message: "audio codec must be opus, aac, flac, or raw",
		}
	}
	return nil
}

// ScrcpyVersion represents the minimum scrcpy version requirements.
type ScrcpyVersion struct {
	Major int
	Minor int
	Patch int
}

// MinVersionForAudio is the minimum scrcpy version required for audio streaming.
var MinVersionForAudio = ScrcpyVersion{Major: 2, Minor: 0, Patch: 0}

// MinVersionForAV1 is the minimum scrcpy version required for AV1 codec.
var MinVersionForAV1 = ScrcpyVersion{Major: 2, Minor: 0, Patch: 0}

// Compare compares two versions.
// Returns -1 if v < other, 0 if v == other, 1 if v > other.
func (v ScrcpyVersion) Compare(other ScrcpyVersion) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}
	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// SupportsAudio returns true if this version supports audio streaming.
func (v ScrcpyVersion) SupportsAudio() bool {
	return v.Compare(MinVersionForAudio) >= 0
}

// SupportsAV1 returns true if this version supports AV1 codec.
func (v ScrcpyVersion) SupportsAV1() bool {
	return v.Compare(MinVersionForAV1) >= 0
}

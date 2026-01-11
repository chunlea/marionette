// Package manager provides the streaming Manager which coordinates
// providers, the SFU, and stream persistence.
package manager

import (
	"time"

	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/sfu"
)

// Config contains configuration for the streaming Manager.
type Config struct {
	// DefaultProvider is the name of the default provider to use when none is specified.
	DefaultProvider string

	// DefaultTimeout is the default timeout for stream operations.
	DefaultTimeout time.Duration

	// DefaultICEServers is the default list of STUN/TURN servers.
	DefaultICEServers []streaming.ICEServer

	// DefaultResolution is the default video resolution.
	DefaultResolution streaming.Resolution

	// DefaultFrameRate is the default frame rate in FPS.
	DefaultFrameRate int

	// DefaultBitRate is the default bit rate in bps.
	DefaultBitRate int

	// CleanupInterval is how often to run the cleanup job for expired streams.
	CleanupInterval time.Duration

	// StreamExpiry is the default stream expiry duration (0 = no expiry).
	StreamExpiry time.Duration

	// SFU is the SFU configuration.
	SFU sfu.Config
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultTimeout: 30 * time.Second,
		DefaultICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
		DefaultResolution: streaming.Resolution{Width: 1920, Height: 1080},
		DefaultFrameRate:  30,
		DefaultBitRate:    4_000_000, // 4 Mbps
		CleanupInterval:   5 * time.Minute,
		StreamExpiry:      0, // No expiry by default
		SFU:               sfu.DefaultConfig(),
	}
}

// Validate validates the manager configuration.
func (c Config) Validate() error {
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 30 * time.Second
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 5 * time.Minute
	}
	return c.SFU.Validate()
}

// WithDefaultProvider sets the default provider name.
func (c Config) WithDefaultProvider(name string) Config {
	c.DefaultProvider = name
	return c
}

// WithDefaultTimeout sets the default timeout.
func (c Config) WithDefaultTimeout(d time.Duration) Config {
	c.DefaultTimeout = d
	return c
}

// WithDefaultICEServers sets the default ICE servers.
func (c Config) WithDefaultICEServers(servers []streaming.ICEServer) Config {
	c.DefaultICEServers = servers
	return c
}

// WithDefaultResolution sets the default resolution.
func (c Config) WithDefaultResolution(r streaming.Resolution) Config {
	c.DefaultResolution = r
	return c
}

// WithDefaultFrameRate sets the default frame rate.
func (c Config) WithDefaultFrameRate(fps int) Config {
	c.DefaultFrameRate = fps
	return c
}

// WithDefaultBitRate sets the default bit rate.
func (c Config) WithDefaultBitRate(bps int) Config {
	c.DefaultBitRate = bps
	return c
}

// WithCleanupInterval sets the cleanup interval.
func (c Config) WithCleanupInterval(d time.Duration) Config {
	c.CleanupInterval = d
	return c
}

// WithStreamExpiry sets the default stream expiry.
func (c Config) WithStreamExpiry(d time.Duration) Config {
	c.StreamExpiry = d
	return c
}

// WithSFUConfig sets the SFU configuration.
func (c Config) WithSFUConfig(sfuCfg sfu.Config) Config {
	c.SFU = sfuCfg
	return c
}

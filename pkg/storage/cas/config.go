package cas

// ChunkerConfig defines content-defined chunking parameters.
type ChunkerConfig struct {
	// MinSize is the minimum chunk size in bytes.
	// Chunks will never be smaller than this (default: 512 KB).
	MinSize uint

	// MaxSize is the maximum chunk size in bytes.
	// Chunks will never be larger than this (default: 8 MB).
	MaxSize uint

	// TargetSize is the target average chunk size in bytes.
	// This affects the Rabin fingerprint mask (default: 1 MB).
	TargetSize uint
}

// DefaultChunkerConfig provides sensible defaults for content-defined chunking.
var DefaultChunkerConfig = ChunkerConfig{
	MinSize:    512 * 1024,      // 512 KB
	MaxSize:    8 * 1024 * 1024, // 8 MB
	TargetSize: 1 * 1024 * 1024, // 1 MB
}

// Config defines overall CAS settings.
type Config struct {
	// Chunker contains content-defined chunking parameters.
	Chunker ChunkerConfig

	// SingleChunkThreshold is the workspace size threshold for single-chunk mode.
	// Workspaces smaller than this are stored as a single tar.zst chunk.
	// Default: 100 MB.
	SingleChunkThreshold int64

	// TempDir is the directory for temporary files during sync/restore.
	// Default: /tmp/marionette-cas
	TempDir string

	// MaxConcurrency is the maximum number of concurrent uploads/downloads.
	// Default: 10.
	MaxConcurrency int

	// CompressLevel is the zstd compression level (1-19).
	// Higher levels provide better compression but are slower.
	// Default: 3 (SpeedDefault).
	CompressLevel int
}

// DefaultConfig provides sensible defaults for CAS operations.
var DefaultConfig = Config{
	Chunker:              DefaultChunkerConfig,
	SingleChunkThreshold: 100 * 1024 * 1024, // 100 MB
	TempDir:              "/tmp/marionette-cas",
	MaxConcurrency:       10,
	CompressLevel:        3, // zstd.SpeedDefault
}

// WithDefaults returns a config with default values for any unset fields.
func (c Config) WithDefaults() Config {
	if c.Chunker.MinSize == 0 {
		c.Chunker.MinSize = DefaultChunkerConfig.MinSize
	}
	if c.Chunker.MaxSize == 0 {
		c.Chunker.MaxSize = DefaultChunkerConfig.MaxSize
	}
	if c.Chunker.TargetSize == 0 {
		c.Chunker.TargetSize = DefaultChunkerConfig.TargetSize
	}
	if c.SingleChunkThreshold == 0 {
		c.SingleChunkThreshold = DefaultConfig.SingleChunkThreshold
	}
	if c.TempDir == "" {
		c.TempDir = DefaultConfig.TempDir
	}
	if c.MaxConcurrency == 0 {
		c.MaxConcurrency = DefaultConfig.MaxConcurrency
	}
	if c.CompressLevel == 0 {
		c.CompressLevel = DefaultConfig.CompressLevel
	}
	return c
}

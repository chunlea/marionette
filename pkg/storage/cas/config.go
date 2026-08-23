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

// CDC mode selectors. A deployment normally leaves this on auto and lets the
// threshold decide; the overrides exist so a test can exercise either path on
// a tree small enough to write in a few lines.
const (
	// CDCModeAuto picks the mode from the workspace size. Default.
	CDCModeAuto = "auto"

	// CDCModeAlways forces content-defined chunking at any size.
	CDCModeAlways = "always"

	// CDCModeNever forces single-chunk mode at any size.
	CDCModeNever = "never"
)

// Config defines overall CAS settings.
type Config struct {
	// Chunker contains content-defined chunking parameters.
	Chunker ChunkerConfig

	// CDCThreshold is the workspace size at which storage switches from a
	// single tar.zst chunk to content-defined chunking. Workspaces smaller
	// than this are stored as one chunk.
	//
	// Below the threshold a whole workspace fits in memory and one chunk is
	// cheaper than thousands; above it, neither holds. Default: 100 MB.
	CDCThreshold int64

	// CDCMode overrides the threshold: CDCModeAuto (default), CDCModeAlways
	// or CDCModeNever.
	CDCMode string

	// MaxSeenChunks caps how many chunk hashes a single sync remembers for
	// deduplication.
	//
	// The set is the one part of a sync that grows with the workspace rather
	// than with a chunk, so it is bounded rather than left to grow: past the
	// cap deduplication falls back to asking storage whether a chunk is
	// already there, which is slower and just as correct.
	// Default: 1,048,576 hashes, about 100 MB of workspace per 1 GB remembered.
	MaxSeenChunks int

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
	Chunker:        DefaultChunkerConfig,
	CDCThreshold:   100 * 1024 * 1024, // 100 MB
	CDCMode:        CDCModeAuto,
	MaxSeenChunks:  1 << 20,
	TempDir:        "/tmp/marionette-cas",
	MaxConcurrency: 10,
	CompressLevel:  3, // zstd.SpeedDefault
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
	if c.CDCThreshold == 0 {
		c.CDCThreshold = DefaultConfig.CDCThreshold
	}
	if c.CDCMode == "" {
		c.CDCMode = DefaultConfig.CDCMode
	}
	if c.MaxSeenChunks == 0 {
		c.MaxSeenChunks = DefaultConfig.MaxSeenChunks
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

// useCDC reports whether a workspace of totalSize is stored with
// content-defined chunking.
func (c Config) useCDC(totalSize int64) bool {
	switch c.CDCMode {
	case CDCModeAlways:
		return true
	case CDCModeNever:
		return false
	default:
		return totalSize >= c.CDCThreshold
	}
}

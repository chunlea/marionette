package cas

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_WithDefaults(t *testing.T) {
	// Empty config should get defaults
	c := Config{}.WithDefaults()

	assert.Equal(t, DefaultChunkerConfig.MinSize, c.Chunker.MinSize)
	assert.Equal(t, DefaultChunkerConfig.MaxSize, c.Chunker.MaxSize)
	assert.Equal(t, DefaultChunkerConfig.TargetSize, c.Chunker.TargetSize)
	assert.Equal(t, DefaultConfig.SingleChunkThreshold, c.SingleChunkThreshold)
	assert.Equal(t, DefaultConfig.TempDir, c.TempDir)
	assert.Equal(t, DefaultConfig.MaxConcurrency, c.MaxConcurrency)
	assert.Equal(t, DefaultConfig.CompressLevel, c.CompressLevel)
}

func TestConfig_WithDefaults_Partial(t *testing.T) {
	// Partial config should only fill missing values
	c := Config{
		SingleChunkThreshold: 50 * 1024 * 1024, // 50 MB
		MaxConcurrency:       20,
	}.WithDefaults()

	// Custom values preserved
	assert.Equal(t, int64(50*1024*1024), c.SingleChunkThreshold)
	assert.Equal(t, 20, c.MaxConcurrency)

	// Defaults filled
	assert.Equal(t, DefaultChunkerConfig.MinSize, c.Chunker.MinSize)
	assert.Equal(t, DefaultConfig.TempDir, c.TempDir)
}

func TestDefaultChunkerConfig(t *testing.T) {
	// Verify defaults are sensible
	assert.Equal(t, uint(512*1024), DefaultChunkerConfig.MinSize)       // 512 KB
	assert.Equal(t, uint(8*1024*1024), DefaultChunkerConfig.MaxSize)    // 8 MB
	assert.Equal(t, uint(1*1024*1024), DefaultChunkerConfig.TargetSize) // 1 MB

	// Max > Target > Min
	assert.Greater(t, DefaultChunkerConfig.MaxSize, DefaultChunkerConfig.TargetSize)
	assert.Greater(t, DefaultChunkerConfig.TargetSize, DefaultChunkerConfig.MinSize)
}

func TestDefaultConfig(t *testing.T) {
	// Verify defaults are sensible
	assert.Equal(t, int64(100*1024*1024), DefaultConfig.SingleChunkThreshold) // 100 MB
	assert.Equal(t, 10, DefaultConfig.MaxConcurrency)
	assert.Equal(t, 3, DefaultConfig.CompressLevel)
}

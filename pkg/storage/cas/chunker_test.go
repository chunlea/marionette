package cas

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultChunker(t *testing.T) {
	c := NewDefaultChunker()
	assert.NotNil(t, c)
	assert.Equal(t, DefaultChunkerConfig.MinSize, c.config.MinSize)
	assert.Equal(t, DefaultChunkerConfig.MaxSize, c.config.MaxSize)
	assert.Equal(t, DefaultChunkerConfig.TargetSize, c.config.TargetSize)
}

func TestChunker_ChunkData_Small(t *testing.T) {
	c := NewDefaultChunker()

	// Small data should result in a single chunk
	data := []byte("hello world")
	chunks, err := c.ChunkData(data)
	require.NoError(t, err)

	assert.Len(t, chunks, 1)
	assert.Equal(t, data, chunks[0].Data)
	assert.Equal(t, int64(len(data)), chunks[0].Size)
	assert.NotEmpty(t, chunks[0].Hash)
}

func TestChunker_ChunkData_Large(t *testing.T) {
	c := NewDefaultChunker()

	// Generate data larger than max chunk size
	data := make([]byte, 10*1024*1024) // 10 MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := c.ChunkData(data)
	require.NoError(t, err)

	// Should have multiple chunks
	assert.Greater(t, len(chunks), 1)

	// Verify chunks can reconstruct original data
	var reconstructed bytes.Buffer
	for _, chunk := range chunks {
		reconstructed.Write(chunk.Data)
	}
	assert.Equal(t, data, reconstructed.Bytes())
}

func TestChunker_ChunkData_Deterministic(t *testing.T) {
	c := NewDefaultChunker()

	// Same data should produce same chunks
	data := make([]byte, 2*1024*1024) // 2 MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks1, err := c.ChunkData(data)
	require.NoError(t, err)

	chunks2, err := c.ChunkData(data)
	require.NoError(t, err)

	require.Equal(t, len(chunks1), len(chunks2))
	for i := range chunks1 {
		assert.Equal(t, chunks1[i].Hash, chunks2[i].Hash)
		assert.Equal(t, chunks1[i].Size, chunks2[i].Size)
	}
}

func TestChunker_ChunkData_ContentDefined(t *testing.T) {
	c := NewDefaultChunker()

	// Generate base data
	base := make([]byte, 5*1024*1024) // 5 MB
	_, err := rand.Read(base)
	require.NoError(t, err)

	chunksOriginal, err := c.ChunkData(base)
	require.NoError(t, err)

	// Modify a small part in the middle
	modified := make([]byte, len(base))
	copy(modified, base)
	modified[len(modified)/2] = ^modified[len(modified)/2] // Flip bits

	chunksModified, err := c.ChunkData(modified)
	require.NoError(t, err)

	// Count matching chunks (content-defined chunking should reuse most boundaries)
	originalHashes := make(map[string]bool)
	for _, c := range chunksOriginal {
		originalHashes[c.Hash] = true
	}

	matchCount := 0
	for _, c := range chunksModified {
		if originalHashes[c.Hash] {
			matchCount++
		}
	}

	// Some chunks should still match (though not all due to the modification)
	// This is a key property of content-defined chunking
	t.Logf("Original chunks: %d, Modified chunks: %d, Matching: %d",
		len(chunksOriginal), len(chunksModified), matchCount)
}

func TestChunker_ChunkData_Empty(t *testing.T) {
	c := NewDefaultChunker()

	chunks, err := c.ChunkData([]byte{})
	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunker_ChunkReader(t *testing.T) {
	c := NewDefaultChunker()

	data := make([]byte, 3*1024*1024) // 3 MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunkCh, errCh := c.ChunkReader(bytes.NewReader(data))

	var chunks []ChunkInfo
	for chunk := range chunkCh {
		chunks = append(chunks, chunk)
	}

	// Check for errors
	select {
	case err := <-errCh:
		require.NoError(t, err)
	default:
	}

	// Verify reconstruction
	var reconstructed bytes.Buffer
	for _, chunk := range chunks {
		reconstructed.Write(chunk.Data)
	}
	assert.Equal(t, data, reconstructed.Bytes())
}

func TestHashData(t *testing.T) {
	data := []byte("hello world")
	hash := HashData(data)

	// SHA-256 should produce 64 hex characters
	assert.Len(t, hash, 64)

	// Same data should produce same hash
	assert.Equal(t, hash, HashData(data))

	// Different data should produce different hash
	assert.NotEqual(t, hash, HashData([]byte("goodbye world")))
}

func TestChunker_ChunkSizeConstraints(t *testing.T) {
	c := NewDefaultChunker()

	// Generate large random data
	data := make([]byte, 20*1024*1024) // 20 MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := c.ChunkData(data)
	require.NoError(t, err)

	for i, chunk := range chunks {
		// All chunks except possibly the last should be >= MinSize
		if i < len(chunks)-1 {
			assert.GreaterOrEqual(t, chunk.Size, int64(c.config.MinSize),
				"Chunk %d size %d is less than MinSize %d", i, chunk.Size, c.config.MinSize)
		}
		// All chunks should be <= MaxSize
		assert.LessOrEqual(t, chunk.Size, int64(c.config.MaxSize),
			"Chunk %d size %d exceeds MaxSize %d", i, chunk.Size, c.config.MaxSize)
	}
}

func TestChunker_UniqueHashes(t *testing.T) {
	c := NewDefaultChunker()

	// Generate random data
	data := make([]byte, 10*1024*1024) // 10 MB
	_, err := rand.Read(data)
	require.NoError(t, err)

	chunks, err := c.ChunkData(data)
	require.NoError(t, err)

	// All chunk hashes should match their actual content hash
	for _, chunk := range chunks {
		expectedHash := HashData(chunk.Data)
		assert.Equal(t, expectedHash, chunk.Hash)
	}
}

package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/restic/chunker"
)

// RabinPolynomial is the polynomial used for Rabin fingerprinting.
// This is the same polynomial used by restic for compatibility.
const RabinPolynomial = chunker.Pol(0x3DA3358B4DC173)

// Chunker provides content-defined chunking using Rabin fingerprinting.
type Chunker struct {
	config ChunkerConfig
	pol    chunker.Pol
}

// NewChunker creates a new chunker with the given configuration.
func NewChunker(config ChunkerConfig) *Chunker {
	return &Chunker{
		config: config,
		pol:    RabinPolynomial,
	}
}

// NewDefaultChunker creates a new chunker with default configuration.
func NewDefaultChunker() *Chunker {
	return NewChunker(DefaultChunkerConfig)
}

// ChunkReader streams chunks from a reader.
// Returns two channels: one for chunks and one for errors.
// The chunks channel is closed when the reader is exhausted.
// The error channel will receive at most one error and then be closed.
func (c *Chunker) ChunkReader(r io.Reader) (<-chan ChunkInfo, <-chan error) {
	chunks := make(chan ChunkInfo, 10)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		chnkr := chunker.New(r, c.pol)
		buf := make([]byte, c.config.MaxSize)

		for {
			chunk, err := chnkr.Next(buf)
			if err == io.EOF {
				return
			}
			if err != nil {
				errs <- err
				return
			}

			// Copy chunk data (buffer is reused by Next())
			data := make([]byte, chunk.Length)
			copy(data, chunk.Data)

			// Compute SHA-256 hash
			hash := sha256.Sum256(data)

			chunks <- ChunkInfo{
				Hash: hex.EncodeToString(hash[:]),
				Size: int64(len(data)),
				Data: data,
			}
		}
	}()

	return chunks, errs
}

// ChunkData chunks a byte slice and returns all chunks.
// This is a convenience method for small data that fits in memory.
func (c *Chunker) ChunkData(data []byte) ([]ChunkInfo, error) {
	chunks, errs := c.ChunkReader(&byteReader{data: data})

	var result []ChunkInfo
	for chunk := range chunks {
		result = append(result, chunk)
	}

	// Check for errors
	select {
	case err := <-errs:
		if err != nil {
			return nil, err
		}
	default:
	}

	return result, nil
}

// HashData computes the SHA-256 hash of data.
func HashData(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// byteReader is a simple io.Reader over a byte slice.
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math/bits"

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

		// The configured boundaries were being dropped here: chunker.New uses
		// the library's own min/max, so a caller that narrowed them got the
		// defaults and never knew.
		chnkr := chunker.NewWithBoundaries(r, c.pol, c.config.MinSize, c.config.MaxSize)
		if avg := c.averageBits(); avg > 0 {
			chnkr.SetAverageBits(avg)
		}
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

// averageBits is the log2 of the target chunk size, which is what the Rabin
// mask is derived from. The chunker takes the exponent rather than the size, so
// a target that is not a power of two rounds down to the nearest one.
func (c *Chunker) averageBits() int {
	if c.config.TargetSize == 0 {
		return 0
	}
	return bits.Len(c.config.TargetSize) - 1
}

// Iterator chunks a sequence of readers while reusing its buffers.
//
// ChunkReader and ChunkData both hand back a fresh copy of every chunk, which
// is what made CDC sync proportional to the size of the workspace rather than
// to the size of a chunk: a 4 GB file became 4 GB of live slices plus whatever
// the channel had buffered. An Iterator holds one chunk at a time and lends it
// to the callback, so the caller decides what is worth copying.
//
// An Iterator is not safe for concurrent use.
type Iterator struct {
	chnkr *chunker.Chunker
	buf   []byte
	bits  int
	pol   chunker.Pol
	min   uint
	max   uint
}

// NewIterator creates an iterator bound to this chunker's parameters.
func (c *Chunker) NewIterator() *Iterator {
	return &Iterator{
		buf:  make([]byte, 0, c.config.MaxSize),
		bits: c.averageBits(),
		pol:  c.pol,
		min:  c.config.MinSize,
		max:  c.config.MaxSize,
	}
}

// Iterate splits r into content-defined chunks and calls fn for each one, in
// order.
//
// The slice passed to fn is only valid until fn returns: it is the iterator's
// single buffer. A callback that needs to keep the bytes must copy them, and
// only when it actually needs to - which is the point, since a chunk that is
// already stored needs nothing but its hash.
func (it *Iterator) Iterate(r io.Reader, fn func(hash string, data []byte) error) error {
	if it.chnkr == nil {
		it.chnkr = chunker.NewWithBoundaries(r, it.pol, it.min, it.max)
		if it.bits > 0 {
			it.chnkr.SetAverageBits(it.bits)
		}
	} else {
		it.chnkr.ResetWithBoundaries(r, it.pol, it.min, it.max)
		if it.bits > 0 {
			it.chnkr.SetAverageBits(it.bits)
		}
	}

	for {
		chunk, err := it.chnkr.Next(it.buf)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		// Keep whatever capacity Next grew so the next chunk reuses it.
		it.buf = chunk.Data

		sum := sha256.Sum256(chunk.Data)
		if err := fn(hex.EncodeToString(sum[:]), chunk.Data); err != nil {
			return err
		}
	}
}

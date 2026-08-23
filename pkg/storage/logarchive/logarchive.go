// Package logarchive reads and writes the objects the log archiver produces.
//
// One object holds one session's logs. The payload is newline-delimited JSON of
// store.Log values in archive order - (created_at, sequence, id) - so a record
// that comes back out is the record that went in, field for field.
//
// The object is a sequence of frames rather than one compressed stream:
//
//	"MLA1" magic
//	repeat: uvarint payload length, then payload
//	payload: zstd(NDJSON lines), then AES-256-GCM if the deployment encrypts
//
// Framing buys three things a single stream does not. Writing is memory-bounded
// - one frame at a time, never the whole session. Reading is too, which matters
// because the retrieval endpoint pages through an archive it did not size.
//
// And appending is cheap. A session archived while idle can produce more logs,
// and the next pass has to extend its archive; with frames that is a byte copy
// of what is already there followed by new frames, with no need to decrypt and
// re-encrypt a history that may be far larger than the addition. Blob stores
// have no append, so the copy still rewrites the object - but it rewrites it
// without ever holding it in memory or touching the key material.
package logarchive

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/store"
)

const (
	// Format is the container version. It is stamped on log_archives.format so
	// a reader consults the row rather than assuming the current encoding.
	Format = "ndjson+zstd/frames1"

	// magic prefixes every object. It is not a security control; it is what
	// turns "this key holds something else entirely" into an error at the first
	// four bytes instead of a zstd failure halfway through.
	magic = "MLA1"

	// contentType is advisory metadata for the blob store.
	contentType = "application/x-marionette-logarchive"

	// DefaultFrameRecords is how many log lines share a frame. Large enough
	// that zstd has something to work with, small enough that a reader paging
	// an archive decompresses kilobytes rather than megabytes per step.
	DefaultFrameRecords = 1000

	// maxFrameBytes bounds a single frame on read. A corrupt or hostile length
	// prefix would otherwise ask for an allocation the size of the number it
	// happens to contain.
	maxFrameBytes = 64 << 20
)

// ErrBadFormat is returned when an object is not a log archive container.
var ErrBadFormat = errors.New("logarchive: not a log archive object")

// Encryptor is the subset of the crypto service this package needs.
//
// It is an interface rather than *cryptoutil.Service so the codec can be tested
// - and run - without a key management stack, and so encryption stays a
// deployment decision rather than a compile-time one.
type Encryptor interface {
	Encrypt(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error)
}

// Store reads and writes archive objects on a blob backend.
type Store struct {
	blobs        storage.StorageProvider
	encryptor    Encryptor
	frameRecords int
}

// Option configures a Store.
type Option func(*Store)

// WithEncryptor turns on per-frame encryption. Without it frames are compressed
// and nothing more, which is the correct default: encrypting without a
// configured key management story stores ciphertext nobody can read back.
func WithEncryptor(e Encryptor) Option {
	return func(s *Store) { s.encryptor = e }
}

// WithFrameRecords overrides how many records share a frame.
func WithFrameRecords(n int) Option {
	return func(s *Store) {
		if n > 0 {
			s.frameRecords = n
		}
	}
}

// New returns a Store over a blob backend.
func New(blobs storage.StorageProvider, opts ...Option) *Store {
	s := &Store{blobs: blobs, frameRecords: DefaultFrameRecords}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Encrypts reports whether this Store writes encrypted frames. The archiver
// records the answer on the row, because the answer can change between passes.
func (s *Store) Encrypts() bool { return s.encryptor != nil }

// Delete removes an archive object. It is idempotent, like the blob layer.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.blobs.Delete(ctx, key)
}

// =============================================================================
// Writing
// =============================================================================

// Writer builds one archive object and uploads it as it is built.
//
// Nothing is buffered beyond the frame in hand: the upload runs concurrently,
// consuming a pipe, so a session with a million log lines costs one frame of
// memory rather than a million records of it.
type Writer struct {
	store    *Store
	tenantID string

	pw    *io.PipeWriter
	done  chan error
	count *countingWriter

	pending []*store.Log
	closed  bool
	err     error
}

// NewWriter starts an upload to key. Every Writer must be closed; a Writer that
// is dropped leaks the upload goroutine and the half-written object.
func (s *Store) NewWriter(ctx context.Context, key, tenantID string) (*Writer, error) {
	pr, pw := io.Pipe()
	done := make(chan error, 1)

	go func() {
		err := s.blobs.Upload(ctx, key, pr, storage.UploadOptions{ContentType: contentType})
		// Fail the producer rather than let it block on a pipe nobody reads:
		// an upload that dies early must surface as a write error, not a hang.
		_ = pr.CloseWithError(err)
		done <- err
	}()

	w := &Writer{
		store:    s,
		tenantID: tenantID,
		pw:       pw,
		done:     done,
		count:    &countingWriter{w: pw},
	}

	if _, err := w.count.Write([]byte(magic)); err != nil {
		w.fail(err)
		return nil, err
	}
	return w, nil
}

// Append adds records to the object, flushing whole frames as they fill.
func (w *Writer) Append(ctx context.Context, logs []*store.Log) error {
	if w.err != nil {
		return w.err
	}
	for _, l := range logs {
		w.pending = append(w.pending, l)
		if len(w.pending) >= w.store.frameRecords {
			if err := w.flushFrame(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// CopyFrames appends the frames of an existing archive to this object.
//
// When the source and this Store agree on encryption the frames are copied as
// opaque bytes, which is the whole point of the framed container: extending a
// session's archive never has to decrypt what is already in it. When they
// disagree - encryption was switched on or off between passes - each frame is
// decoded and re-encoded, so the resulting object is uniform and the `encrypted`
// column stays a fact rather than a guess.
func (w *Writer) CopyFrames(ctx context.Context, src *Reader) error {
	if w.err != nil {
		return w.err
	}

	sameEncoding := src.encrypted == w.store.Encrypts()
	for {
		payload, err := src.frames.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if sameEncoding {
			if err := w.writeFrame(payload); err != nil {
				return err
			}
			continue
		}

		records, err := src.decodeFrame(ctx, payload)
		if err != nil {
			return err
		}
		if err := w.Append(ctx, records); err != nil {
			return err
		}
	}
}

// Close flushes the last frame, finishes the upload, and reports how many bytes
// the object holds.
func (w *Writer) Close(ctx context.Context) (int64, error) {
	if w.closed {
		return w.count.n, w.err
	}
	w.closed = true

	if w.err == nil {
		if err := w.flushFrame(ctx); err != nil {
			w.err = err
		}
	}

	if w.err != nil {
		// Poison the pipe so Upload aborts instead of storing a truncated
		// object that would read back as a short but plausible archive.
		_ = w.pw.CloseWithError(w.err)
		<-w.done
		return w.count.n, w.err
	}

	if err := w.pw.Close(); err != nil {
		<-w.done
		return w.count.n, err
	}
	if err := <-w.done; err != nil {
		return w.count.n, fmt.Errorf("uploading log archive: %w", err)
	}
	return w.count.n, nil
}

func (w *Writer) fail(err error) {
	w.err = err
	_ = w.pw.CloseWithError(err)
	<-w.done
	w.closed = true
}

func (w *Writer) flushFrame(ctx context.Context) error {
	if len(w.pending) == 0 {
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, l := range w.pending {
		if err := enc.Encode(l); err != nil {
			w.err = fmt.Errorf("encoding log %s: %w", l.ID, err)
			return w.err
		}
	}
	w.pending = w.pending[:0]

	payload, err := compress(buf.Bytes())
	if err != nil {
		w.err = err
		return err
	}

	if w.store.encryptor != nil {
		payload, err = w.store.encryptor.Encrypt(ctx, w.tenantID, payload)
		if err != nil {
			w.err = fmt.Errorf("encrypting log archive frame: %w", err)
			return w.err
		}
	}

	return w.writeFrame(payload)
}

func (w *Writer) writeFrame(payload []byte) error {
	var header [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(header[:], uint64(len(payload)))
	if _, err := w.count.Write(header[:n]); err != nil {
		w.err = err
		return err
	}
	if _, err := w.count.Write(payload); err != nil {
		w.err = err
		return err
	}
	return nil
}

// countingWriter tracks the object size so the archive row can record it
// without asking the blob store to stat an object it just wrote.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// =============================================================================
// Reading
// =============================================================================

// Reader streams records back out of an archive object.
type Reader struct {
	store     *Store
	rc        io.ReadCloser
	frames    *frameReader
	tenantID  string
	encrypted bool

	buf []*store.Log
	pos int
}

// Open streams the object an archive row points at.
//
// Encryption and format come from the row rather than from configuration:
// objects outlive the settings that produced them, and a deployment that
// switches encryption on must still be able to read what it wrote before.
func (s *Store) Open(ctx context.Context, archive *store.LogArchive) (*Reader, error) {
	if archive == nil {
		return nil, fmt.Errorf("logarchive: nil archive")
	}
	if archive.Format != "" && archive.Format != Format {
		return nil, fmt.Errorf("%w: unsupported format %q", ErrBadFormat, archive.Format)
	}
	if archive.Encrypted && s.encryptor == nil {
		return nil, fmt.Errorf("logarchive: archive %s is encrypted but no encryptor is configured", archive.ID)
	}

	rc, _, err := s.blobs.Download(ctx, archive.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("downloading log archive %s: %w", archive.StorageKey, err)
	}

	br := bufio.NewReaderSize(rc, 64<<10)
	header := make([]byte, len(magic))
	if _, err := io.ReadFull(br, header); err != nil {
		_ = rc.Close()
		return nil, fmt.Errorf("%w: %v", ErrBadFormat, err)
	}
	if string(header) != magic {
		_ = rc.Close()
		return nil, ErrBadFormat
	}

	tenantID := ""
	if archive.TenantID != nil {
		tenantID = *archive.TenantID
	}

	return &Reader{
		store:     s,
		rc:        rc,
		frames:    &frameReader{r: br},
		tenantID:  tenantID,
		encrypted: archive.Encrypted,
	}, nil
}

// Next returns the next record, or io.EOF at the end of the object.
func (r *Reader) Next(ctx context.Context) (*store.Log, error) {
	for r.pos >= len(r.buf) {
		payload, err := r.frames.next()
		if err != nil {
			return nil, err
		}
		records, err := r.decodeFrame(ctx, payload)
		if err != nil {
			return nil, err
		}
		r.buf, r.pos = records, 0
	}
	rec := r.buf[r.pos]
	r.pos++
	return rec, nil
}

// Close releases the underlying object stream.
func (r *Reader) Close() error { return r.rc.Close() }

func (r *Reader) decodeFrame(ctx context.Context, payload []byte) ([]*store.Log, error) {
	if r.encrypted {
		plain, err := r.store.encryptor.Decrypt(ctx, r.tenantID, payload)
		if err != nil {
			return nil, fmt.Errorf("decrypting log archive frame: %w", err)
		}
		payload = plain
	}

	raw, err := decompress(payload)
	if err != nil {
		return nil, err
	}

	var records []*store.Log
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var l store.Log
		if err := dec.Decode(&l); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}
			return nil, fmt.Errorf("decoding archived log: %w", err)
		}
		records = append(records, &l)
	}
}

// frameReader walks the length-prefixed frames of an object.
type frameReader struct {
	r *bufio.Reader
}

func (f *frameReader) next() ([]byte, error) {
	size, err := binary.ReadUvarint(f.r)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("%w: reading frame length: %v", ErrBadFormat, err)
	}
	if size == 0 || size > maxFrameBytes {
		return nil, fmt.Errorf("%w: frame length %d out of range", ErrBadFormat, size)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(f.r, payload); err != nil {
		return nil, fmt.Errorf("%w: reading frame: %v", ErrBadFormat, err)
	}
	return payload, nil
}

// =============================================================================
// Compression
// =============================================================================

func compress(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("creating zstd encoder: %w", err)
	}
	out := enc.EncodeAll(data, nil)
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("closing zstd encoder: %w", err)
	}
	return out, nil
}

func decompress(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("creating zstd decoder: %w", err)
	}
	defer dec.Close()

	out, err := dec.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing log archive frame: %w", err)
	}
	return out, nil
}

// =============================================================================
// Keys
// =============================================================================

// Key returns the object key for a session's archive at a given record count.
//
// The count is in the key so that extending an archive writes a new object and
// leaves the old one untouched until the row has been updated to point at the
// new one. Nothing ever overwrites a live archive; a crashed pass leaves an
// orphan the expiry sweep does not know about, which is a wasted object rather
// than a lost log.
//
// It is also deterministic: re-running an interrupted pass over the same rows
// produces the same key and the same bytes, so the retry overwrites its own
// abandoned object instead of accumulating one per attempt.
func Key(tenantID, sessionID string, records int64) string {
	tenant := tenantID
	if tenant == "" {
		// Single-tenant deployments have no tenant id, and an empty path
		// segment would collapse the prefix and make the listing unreadable.
		tenant = "_"
	}
	return fmt.Sprintf("logs/%s/%s/%d.mla", tenant, sessionID, records)
}

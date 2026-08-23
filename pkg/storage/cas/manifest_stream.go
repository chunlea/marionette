package cas

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/chunlea/marionette/pkg/storage"
)

// A manifest lists every file in a workspace, so its size is proportional to
// the workspace, not to a constant. The original encoding required the whole
// thing in memory three times over - as JSONL, compressed, and encrypted - to
// write it and again to read it, which is the same mistake CDC mode exists to
// avoid.
//
// The framed encoding stores the JSONL as a sequence of independently encrypted
// segments:
//
//	"MFSTF1\n"
//	uvarint len | frame   <- the header, exactly one JSON line
//	uvarint len | frame   <- entries, whole lines, DefaultManifestFrameSize each
//	...
//
// A writer holds one frame; so does a reader. Envelope encryption is unchanged:
// each frame goes through the same Encryptor, which compresses and then seals
// it, so the object is still JSONL, still zstd, still encrypted, still at the
// documented key.
//
// Objects that do not begin with the magic are the original single-buffer
// encoding and are read the way they always were. Small manifests are still
// written that way, so the small-workspace path produces byte-identical
// objects.
var framedMagic = []byte("MFSTF1\n")

// DefaultManifestFrameSize is how much raw JSONL a frame holds before it is
// sealed. It bounds a writer's and a reader's memory: one frame plus whatever
// the encryptor allocates for it.
const DefaultManifestFrameSize = 4 << 20

// maxFrameCiphertext is the largest frame a reader will allocate for.
// Manifest objects are written by this package, so anything larger is a
// corrupt or hostile object rather than a large workspace.
const maxFrameCiphertext = 256 << 20

// ErrManifestCorrupt is returned when a manifest object cannot be framed.
var ErrManifestCorrupt = errors.New("manifest object is corrupt")

// =============================================================================
// Writing
// =============================================================================

// ManifestObjectWriter writes a manifest without holding its entries.
//
// Entries are appended as they are discovered and sealed a frame at a time into
// a spool file; Commit prepends the header and uploads. The header goes last
// because the counts it carries - how many chunks, how many entries - are only
// known once the walk is over, and a header that has to be truthful cannot be
// written before the truth is known.
type ManifestObjectWriter struct {
	store    *BlobManifestStore
	manifest *Manifest

	frameSize int
	spool     *os.File
	pending   bytes.Buffer
	entries   int
	committed bool
}

// OpenManifestWriter begins a streaming write of a manifest.
// The caller must call Commit or Abort.
func (s *BlobManifestStore) OpenManifestWriter(_ context.Context, manifest *Manifest) (*ManifestObjectWriter, error) {
	if manifest == nil {
		return nil, errors.New("manifest must not be nil")
	}

	dir := s.tempDir
	if dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("failed to create manifest spool directory: %w", err)
		}
	}

	spool, err := os.CreateTemp(dir, "marionette-manifest-*.frames")
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest spool: %w", err)
	}
	// The spool only has to outlive this writer, and unlinking it now means a
	// crashed sync leaves nothing behind for anyone to clean up.
	_ = os.Remove(spool.Name())

	return &ManifestObjectWriter{
		store:     s,
		manifest:  manifest,
		frameSize: DefaultManifestFrameSize,
		spool:     spool,
	}, nil
}

// Append records one entry.
func (w *ManifestObjectWriter) Append(ctx context.Context, entry ManifestFile) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest entry %s: %w", entry.Path, err)
	}

	w.pending.Write(line)
	w.pending.WriteByte('\n')
	w.entries++

	if w.pending.Len() >= w.frameSize {
		return w.flushFrame(ctx)
	}
	return nil
}

// Entries returns how many entries have been appended.
func (w *ManifestObjectWriter) Entries() int { return w.entries }

// flushFrame seals whatever raw JSONL is pending into one frame.
func (w *ManifestObjectWriter) flushFrame(ctx context.Context) error {
	if w.pending.Len() == 0 {
		return nil
	}

	frame, err := w.sealFrame(ctx, w.pending.Bytes())
	if err != nil {
		return err
	}
	w.pending.Reset()

	if _, err := w.spool.Write(frame); err != nil {
		return fmt.Errorf("failed to spool manifest frame: %w", err)
	}
	return nil
}

// sealFrame encrypts a segment and prefixes it with its length.
func (w *ManifestObjectWriter) sealFrame(ctx context.Context, plain []byte) ([]byte, error) {
	sealed, err := w.store.encryptor.Encrypt(ctx, w.manifest.TenantID, plain)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt manifest frame: %w", err)
	}

	out := make([]byte, 0, binary.MaxVarintLen64+len(sealed))
	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], uint64(len(sealed)))
	out = append(out, hdr[:n]...)
	out = append(out, sealed...)
	return out, nil
}

// Commit writes the header and uploads the object.
//
// The upload is the durability point: nothing else may record this manifest
// until it returns. An object with no database row is an orphan the collector
// can reclaim; a row pointing at an object that was never written is a restore
// that fails with no way back.
func (w *ManifestObjectWriter) Commit(ctx context.Context) error {
	if w.committed {
		return errors.New("manifest writer already committed")
	}
	defer func() { _ = w.spool.Close() }()

	if err := w.flushFrame(ctx); err != nil {
		return err
	}

	header := w.manifest.ToHeader()
	header.Ordered = w.manifest.Ordered
	header.FileCount = w.entries

	headerLine, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest header: %w", err)
	}
	headerFrame, err := w.sealFrame(ctx, append(headerLine, '\n'))
	if err != nil {
		return err
	}

	if _, err := w.spool.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind manifest spool: %w", err)
	}

	body := io.MultiReader(
		bytes.NewReader(framedMagic),
		bytes.NewReader(headerFrame),
		w.spool,
	)

	key := manifestKey(w.manifest.TenantID, w.manifest.WorkspaceID, w.manifest.ID)
	if err := w.store.storage.Upload(ctx, key, body, storage.UploadOptions{
		ContentType: "application/x-ndjson+zstd",
	}); err != nil {
		return fmt.Errorf("failed to upload manifest: %w", err)
	}

	w.manifest.FileCount = w.entries
	w.committed = true
	return nil
}

// Abort discards the spool. Safe to call after Commit.
func (w *ManifestObjectWriter) Abort() {
	_ = w.spool.Close()
}

// =============================================================================
// Reading
// =============================================================================

// ManifestEntries is a cursor over a manifest's entries.
//
// It holds the header and at most one frame, so the reader's memory does not
// grow with the number of files in the workspace. Close must be called.
type ManifestEntries struct {
	header ManifestHeader

	src    io.ReadCloser
	br     *bufio.Reader
	framed bool

	// frame holds the undelivered lines of the current frame.
	frame []byte

	// ctx belongs to the open call. A cursor decrypts lazily on Next, so it
	// has to carry the context that authorised the read.
	ctx    context.Context
	store  *BlobManifestStore
	tenant string

	err error
}

// OpenManifest opens a manifest for streaming reads.
func (s *BlobManifestStore) OpenManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*ManifestEntries, error) {
	key := manifestKey(tenantID, workspaceID, manifestID)

	reader, _, err := s.storage.Download(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrManifestNotFound
		}
		return nil, fmt.Errorf("failed to download manifest: %w", err)
	}

	entries := &ManifestEntries{
		src:    reader,
		br:     bufio.NewReaderSize(reader, 64*1024),
		ctx:    ctx,
		store:  s,
		tenant: tenantID,
	}

	magic, err := entries.br.Peek(len(framedMagic))
	switch {
	case err == nil && bytes.Equal(magic, framedMagic):
		entries.framed = true
		if _, err := entries.br.Discard(len(framedMagic)); err != nil {
			_ = entries.Close()
			return nil, fmt.Errorf("failed to read manifest magic: %w", err)
		}
	default:
		// Either the object predates framing or it is shorter than the magic.
		// Both are handled by the original decoder, which will report a bad
		// object as one.
		if err := entries.loadLegacy(); err != nil {
			_ = entries.Close()
			return nil, err
		}
	}

	line, err := entries.nextLine()
	if err != nil {
		_ = entries.Close()
		if errors.Is(err, io.EOF) {
			return nil, ErrInvalidManifest
		}
		return nil, err
	}
	if err := json.Unmarshal(line, &entries.header); err != nil {
		_ = entries.Close()
		return nil, fmt.Errorf("failed to parse manifest header: %w", err)
	}

	return entries, nil
}

// Header returns the manifest metadata.
func (e *ManifestEntries) Header() ManifestHeader { return e.header }

// Manifest returns the metadata as a Manifest with no entries loaded.
func (e *ManifestEntries) Manifest() *Manifest { return FromHeader(e.header) }

// Next returns the next entry, or io.EOF when the manifest is exhausted.
func (e *ManifestEntries) Next() (ManifestFile, error) {
	line, err := e.nextLine()
	if err != nil {
		return ManifestFile{}, err
	}

	var entry ManifestFile
	if err := json.Unmarshal(line, &entry); err != nil {
		return ManifestFile{}, fmt.Errorf("failed to parse manifest entry: %w", err)
	}
	return entry, nil
}

// Close releases the underlying object reader.
func (e *ManifestEntries) Close() error {
	e.frame = nil
	if e.src == nil {
		return nil
	}
	err := e.src.Close()
	e.src = nil
	return err
}

// nextLine returns the next JSONL line, decrypting another frame if needed.
func (e *ManifestEntries) nextLine() ([]byte, error) {
	for {
		if len(e.frame) > 0 {
			idx := bytes.IndexByte(e.frame, '\n')
			if idx < 0 {
				line := e.frame
				e.frame = nil
				return line, nil
			}
			line := e.frame[:idx]
			e.frame = e.frame[idx+1:]
			if len(line) == 0 {
				continue
			}
			return line, nil
		}

		if !e.framed {
			return nil, io.EOF
		}
		if err := e.readFrame(); err != nil {
			return nil, err
		}
	}
}

// readFrame decrypts one frame into e.frame.
func (e *ManifestEntries) readFrame() error {
	size, err := binary.ReadUvarint(e.br)
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil {
		return fmt.Errorf("%w: frame length: %w", ErrManifestCorrupt, err)
	}
	if size == 0 || size > maxFrameCiphertext {
		return fmt.Errorf("%w: frame length %d", ErrManifestCorrupt, size)
	}

	sealed := make([]byte, size)
	if _, err := io.ReadFull(e.br, sealed); err != nil {
		return fmt.Errorf("%w: short frame: %w", ErrManifestCorrupt, err)
	}

	plain, err := e.store.encryptor.Decrypt(e.ctx, e.tenant, sealed)
	if err != nil {
		return fmt.Errorf("failed to decrypt manifest frame: %w", err)
	}
	e.frame = plain
	return nil
}

// loadLegacy decodes the original single-buffer encoding into one frame.
func (e *ManifestEntries) loadLegacy() error {
	sealed, err := io.ReadAll(e.br)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	compressed, err := e.store.encryptor.Decrypt(e.ctx, e.tenant, sealed)
	if err != nil {
		return fmt.Errorf("failed to decrypt manifest: %w", err)
	}

	decoder, err := newZstdDecoder()
	if err != nil {
		return err
	}
	defer decoder.Close()

	plain, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return fmt.Errorf("failed to decompress manifest: %w", err)
	}

	e.frame = plain
	return nil
}

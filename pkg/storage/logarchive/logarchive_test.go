package logarchive_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store"
)

// memoryBlobs is a blob backend that keeps objects in memory.
//
// The package's own double rather than cas.MemoryProvider: the codec's tests
// should fail when the codec breaks, not when the content-addressed store next
// to it changes shape.
type memoryBlobs struct {
	mu      sync.Mutex
	objects map[string][]byte

	// failUploadAfter aborts an upload once it has consumed this many bytes,
	// which is how the tests reach the half-written-object path.
	failUploadAfter int
}

func newMemoryBlobs() *memoryBlobs {
	return &memoryBlobs{objects: map[string][]byte{}}
}

func (m *memoryBlobs) Name() string { return "memory" }

func (m *memoryBlobs) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	var buf bytes.Buffer
	if m.failUploadAfter > 0 {
		if _, err := io.CopyN(&buf, r, int64(m.failUploadAfter)); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		return errors.New("upload failed")
	}
	if _, err := io.Copy(&buf, r); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = buf.Bytes()
	return nil
}

func (m *memoryBlobs) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (m *memoryBlobs) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memoryBlobs) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok, nil
}

// xorEncryptor stands in for envelope encryption: it is not a cipher, it is a
// transformation that proves the frames went through the encryptor and came
// back, and that a reader without one refuses to guess.
type xorEncryptor struct{ key byte }

func (e xorEncryptor) Encrypt(_ context.Context, tenantID string, plaintext []byte) ([]byte, error) {
	return e.apply(tenantID, plaintext), nil
}

func (e xorEncryptor) Decrypt(_ context.Context, tenantID string, ciphertext []byte) ([]byte, error) {
	return e.apply(tenantID, ciphertext), nil
}

func (e xorEncryptor) apply(tenantID string, data []byte) []byte {
	k := e.key
	for i := 0; i < len(tenantID); i++ {
		k ^= tenantID[i]
	}
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ k
	}
	return out
}

func testLogs(n int, sessionID string) []*store.Log {
	base := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	logs := make([]*store.Log, 0, n)
	for i := 0; i < n; i++ {
		logs = append(logs, &store.Log{
			ID:        fmt.Sprintf("log_%04d", i),
			SessionID: sessionID,
			TaskID:    "task_1",
			RunID:     "run_1",
			RunnerID:  "runner_1",
			Stream:    "stdout",
			Level:     "info",
			Content:   fmt.Sprintf("line %d", i),
			Sequence:  int64(i),
			Metadata:  json.RawMessage(`{}`),
			CreatedAt: base.Add(time.Duration(i) * time.Millisecond),
		})
	}
	return logs
}

func writeArchive(t *testing.T, s *logarchive.Store, key, tenantID string, logs []*store.Log) int64 {
	t.Helper()
	ctx := context.Background()

	w, err := s.NewWriter(ctx, key, tenantID)
	require.NoError(t, err)
	require.NoError(t, w.Append(ctx, logs))
	size, err := w.Close(ctx)
	require.NoError(t, err)
	return size
}

func readAll(t *testing.T, s *logarchive.Store, archive *store.LogArchive) []*store.Log {
	t.Helper()
	ctx := context.Background()

	r, err := s.Open(ctx, archive)
	require.NoError(t, err)
	defer func() { assert.NoError(t, r.Close()) }()

	var out []*store.Log
	for {
		rec, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, rec)
	}
}

func archiveRow(key string, tenantID *string, encrypted bool) *store.LogArchive {
	return &store.LogArchive{
		ID:         "arch_test",
		SessionID:  "sess_test",
		TenantID:   tenantID,
		StorageKey: key,
		Format:     logarchive.Format,
		Encrypted:  encrypted,
	}
}

func TestRoundTripPreservesEveryField(t *testing.T) {
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs, logarchive.WithFrameRecords(7))

	logs := testLogs(20, "sess_test")
	key := logarchive.Key("", "sess_test", int64(len(logs)))
	size := writeArchive(t, s, key, "", logs)
	assert.Positive(t, size)

	got := readAll(t, s, archiveRow(key, nil, false))
	require.Len(t, got, len(logs))
	for i := range logs {
		assert.Equal(t, *logs[i], *got[i], "record %d changed in the archive", i)
	}
}

// A record count that is not a whole number of frames is the normal case; the
// tail frame is where an off-by-one silently drops the last lines.
func TestRoundTripWithPartialFinalFrame(t *testing.T) {
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs, logarchive.WithFrameRecords(10))

	logs := testLogs(25, "sess_test")
	key := "logs/_/sess_test/25.mla"
	writeArchive(t, s, key, "", logs)

	got := readAll(t, s, archiveRow(key, nil, false))
	require.Len(t, got, 25)
	assert.Equal(t, "line 24", got[24].Content)
}

func TestEmptyArchiveReadsBackEmpty(t *testing.T) {
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	key := "logs/_/sess_test/0.mla"
	writeArchive(t, s, key, "", nil)

	assert.Empty(t, readAll(t, s, archiveRow(key, nil, false)))
}

func TestEncryptedRoundTrip(t *testing.T) {
	blobs := newMemoryBlobs()
	tenant := "tenant_a"
	s := logarchive.New(blobs,
		logarchive.WithEncryptor(xorEncryptor{key: 0x5a}),
		logarchive.WithFrameRecords(5))
	assert.True(t, s.Encrypts())

	logs := testLogs(12, "sess_test")
	key := logarchive.Key(tenant, "sess_test", 12)
	writeArchive(t, s, key, tenant, logs)

	// The plaintext must not be sitting in the object.
	raw, _, err := blobs.Download(context.Background(), key)
	require.NoError(t, err)
	body, err := io.ReadAll(raw)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "line 0")

	got := readAll(t, s, archiveRow(key, &tenant, true))
	require.Len(t, got, 12)
	assert.Equal(t, "line 11", got[11].Content)
}

// An archive written before encryption was switched on has to stay readable
// after, which is why `encrypted` lives on the row and not in the config.
func TestReaderFollowsTheRowNotTheConfig(t *testing.T) {
	blobs := newMemoryBlobs()
	plain := logarchive.New(blobs)

	key := "logs/_/sess_test/3.mla"
	writeArchive(t, plain, key, "", testLogs(3, "sess_test"))

	// Same object, now read by a deployment that has encryption turned on.
	encrypting := logarchive.New(blobs, logarchive.WithEncryptor(xorEncryptor{key: 0x11}))
	got := readAll(t, encrypting, archiveRow(key, nil, false))
	assert.Len(t, got, 3)
}

func TestOpenRejectsEncryptedArchiveWithoutEncryptor(t *testing.T) {
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	key := "logs/_/sess_test/1.mla"
	writeArchive(t, s, key, "", testLogs(1, "sess_test"))

	_, err := s.Open(context.Background(), archiveRow(key, nil, true))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no encryptor is configured")
}

func TestOpenRejectsUnknownFormat(t *testing.T) {
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	row := archiveRow("logs/_/sess_test/1.mla", nil, false)
	row.Format = "tar.gz"
	_, err := s.Open(context.Background(), row)
	require.ErrorIs(t, err, logarchive.ErrBadFormat)
}

func TestOpenRejectsForeignObject(t *testing.T) {
	blobs := newMemoryBlobs()
	require.NoError(t, blobs.Upload(context.Background(), "logs/_/x/1.mla",
		bytes.NewReader([]byte("not an archive")), storage.UploadOptions{}))

	s := logarchive.New(blobs)
	_, err := s.Open(context.Background(), archiveRow("logs/_/x/1.mla", nil, false))
	require.ErrorIs(t, err, logarchive.ErrBadFormat)
}

// Appending is what a session archived while idle needs when it produces more
// logs: the existing frames are copied through and the new records follow.
func TestCopyFramesExtendsAnArchive(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs, logarchive.WithFrameRecords(4))

	first := testLogs(10, "sess_test")
	firstKey := logarchive.Key("", "sess_test", 10)
	writeArchive(t, s, firstKey, "", first)

	more := testLogs(15, "sess_test")[10:]
	secondKey := logarchive.Key("", "sess_test", 15)

	src, err := s.Open(ctx, archiveRow(firstKey, nil, false))
	require.NoError(t, err)
	w, err := s.NewWriter(ctx, secondKey, "")
	require.NoError(t, err)
	require.NoError(t, w.CopyFrames(ctx, src))
	require.NoError(t, src.Close())
	require.NoError(t, w.Append(ctx, more))
	_, err = w.Close(ctx)
	require.NoError(t, err)

	got := readAll(t, s, archiveRow(secondKey, nil, false))
	require.Len(t, got, 15)
	for i := range got {
		assert.Equal(t, fmt.Sprintf("line %d", i), got[i].Content)
	}

	// The original object is untouched, which is what lets the row be updated
	// after the new object is durable rather than before.
	assert.Len(t, readAll(t, s, archiveRow(firstKey, nil, false)), 10)
}

// Switching encryption on between passes must not leave an object whose frames
// disagree with its `encrypted` column.
func TestCopyFramesReEncodesWhenEncryptionChanges(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()

	plain := logarchive.New(blobs, logarchive.WithFrameRecords(3))
	firstKey := "logs/_/sess_test/6.mla"
	writeArchive(t, plain, firstKey, "", testLogs(6, "sess_test"))

	encrypting := logarchive.New(blobs,
		logarchive.WithEncryptor(xorEncryptor{key: 0x33}),
		logarchive.WithFrameRecords(3))

	src, err := plain.Open(ctx, archiveRow(firstKey, nil, false))
	require.NoError(t, err)
	secondKey := "logs/_/sess_test/9.mla"
	w, err := encrypting.NewWriter(ctx, secondKey, "")
	require.NoError(t, err)
	require.NoError(t, w.CopyFrames(ctx, src))
	require.NoError(t, src.Close())
	require.NoError(t, w.Append(ctx, testLogs(9, "sess_test")[6:]))
	_, err = w.Close(ctx)
	require.NoError(t, err)

	// Every frame is now encrypted, so the row says encrypted and the whole
	// object reads back through the encryptor.
	got := readAll(t, encrypting, archiveRow(secondKey, nil, true))
	require.Len(t, got, 9)
	assert.Equal(t, "line 8", got[8].Content)
}

func TestCloseReportsUploadFailure(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	blobs.failUploadAfter = 2
	s := logarchive.New(blobs, logarchive.WithFrameRecords(2))

	w, err := s.NewWriter(ctx, "logs/_/sess_test/4.mla", "")
	if err != nil {
		// The upload can fail before the magic is written; that is still a
		// reported failure, which is the point.
		return
	}
	_ = w.Append(ctx, testLogs(4, "sess_test"))
	_, err = w.Close(ctx)
	require.Error(t, err)

	// Nothing was stored, so no half-object can be mistaken for an archive.
	exists, err := blobs.Exists(ctx, "logs/_/sess_test/4.mla")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestKeyIsDeterministicAndTenantScoped(t *testing.T) {
	assert.Equal(t, "logs/_/sess_a/12.mla", logarchive.Key("", "sess_a", 12))
	assert.Equal(t, "logs/ten_1/sess_a/12.mla", logarchive.Key("ten_1", "sess_a", 12))
	assert.Equal(t, logarchive.Key("ten_1", "sess_a", 12), logarchive.Key("ten_1", "sess_a", 12))
	assert.NotEqual(t, logarchive.Key("ten_1", "sess_a", 12), logarchive.Key("ten_1", "sess_a", 13))
}

// failingEncryptor lets the tests reach the paths where key material is
// unavailable - a rotated KEK, a DEK the reader cannot see - which are the
// realistic encryption failures, not corrupt ciphertext.
type failingEncryptor struct{ onEncrypt, onDecrypt bool }

func (f failingEncryptor) Encrypt(_ context.Context, _ string, plaintext []byte) ([]byte, error) {
	if f.onEncrypt {
		return nil, errors.New("no data key")
	}
	return plaintext, nil
}

func (f failingEncryptor) Decrypt(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	if f.onDecrypt {
		return nil, errors.New("no data key")
	}
	return ciphertext, nil
}

func TestDeleteRemovesTheObject(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	key := "logs/_/sess_test/2.mla"
	writeArchive(t, s, key, "", testLogs(2, "sess_test"))

	require.NoError(t, s.Delete(ctx, key))
	exists, err := blobs.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)

	// Idempotent, like the blob layer underneath it.
	require.NoError(t, s.Delete(ctx, key))
}

func TestOpenReportsAMissingObject(t *testing.T) {
	s := logarchive.New(newMemoryBlobs())
	_, err := s.Open(context.Background(), archiveRow("logs/_/gone/1.mla", nil, false))
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestOpenRejectsNilArchive(t *testing.T) {
	s := logarchive.New(newMemoryBlobs())
	_, err := s.Open(context.Background(), nil)
	require.Error(t, err)
}

func TestWriteFailsWhenEncryptionFails(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs, logarchive.WithEncryptor(failingEncryptor{onEncrypt: true}))

	w, err := s.NewWriter(ctx, "logs/_/sess_test/1.mla", "")
	require.NoError(t, err)
	require.NoError(t, w.Append(ctx, testLogs(1, "sess_test")))
	_, err = w.Close(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypting log archive frame")

	// A frame that could not be encrypted must not reach the blob store as an
	// object: a truncated archive reads back as a short but plausible one.
	exists, err := blobs.Exists(ctx, "logs/_/sess_test/1.mla")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestReadFailsWhenDecryptionFails(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs, logarchive.WithEncryptor(failingEncryptor{}))

	key := "logs/_/sess_test/2.mla"
	writeArchive(t, s, key, "", testLogs(2, "sess_test"))

	broken := logarchive.New(blobs, logarchive.WithEncryptor(failingEncryptor{onDecrypt: true}))
	r, err := broken.Open(ctx, archiveRow(key, nil, true))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = r.Next(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypting log archive frame")
}

// A truncated object is the shape a crashed uploader leaves behind on a backend
// that stores partial writes. It must be an error, never a short read that
// looks like a complete archive.
func TestTruncatedObjectIsAnError(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	key := "logs/_/sess_test/5.mla"
	writeArchive(t, s, key, "", testLogs(5, "sess_test"))

	full, _, err := blobs.Download(ctx, key)
	require.NoError(t, err)
	body, err := io.ReadAll(full)
	require.NoError(t, err)

	truncated := "logs/_/sess_test/trunc.mla"
	require.NoError(t, blobs.Upload(ctx, truncated,
		bytes.NewReader(body[:len(body)-4]), storage.UploadOptions{}))

	r, err := s.Open(ctx, archiveRow(truncated, nil, false))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	var readErr error
	for readErr == nil {
		_, readErr = r.Next(ctx)
	}
	require.Error(t, readErr)
	assert.NotErrorIs(t, readErr, io.EOF)
}

func TestFrameLengthOutOfRangeIsRejected(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	// Magic followed by a frame claiming to be a gigabyte.
	var body bytes.Buffer
	body.WriteString("MLA1")
	var header [10]byte
	n := binary.PutUvarint(header[:], 1<<30)
	body.Write(header[:n])

	key := "logs/_/sess_test/huge.mla"
	require.NoError(t, blobs.Upload(ctx, key, bytes.NewReader(body.Bytes()), storage.UploadOptions{}))

	r, err := s.Open(ctx, archiveRow(key, nil, false))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = r.Next(ctx)
	require.ErrorIs(t, err, logarchive.ErrBadFormat)
}

func TestCorruptFramePayloadIsAnError(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	var body bytes.Buffer
	body.WriteString("MLA1")
	payload := []byte("this is not zstd")
	var header [10]byte
	n := binary.PutUvarint(header[:], uint64(len(payload)))
	body.Write(header[:n])
	body.Write(payload)

	key := "logs/_/sess_test/corrupt.mla"
	require.NoError(t, blobs.Upload(ctx, key, bytes.NewReader(body.Bytes()), storage.UploadOptions{}))

	r, err := s.Open(ctx, archiveRow(key, nil, false))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	_, err = r.Next(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decompressing log archive frame")
}

func TestCloseIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := logarchive.New(newMemoryBlobs())

	w, err := s.NewWriter(ctx, "logs/_/sess_test/1.mla", "")
	require.NoError(t, err)
	require.NoError(t, w.Append(ctx, testLogs(1, "sess_test")))

	size, err := w.Close(ctx)
	require.NoError(t, err)
	again, err := w.Close(ctx)
	require.NoError(t, err)
	assert.Equal(t, size, again)
}

// Once a Writer has failed it must stay failed. A caller that keeps appending
// after an upload died would otherwise get silent successes and a Close that
// reports the original error long after the records were dropped.
func TestWriterStaysFailed(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	blobs.failUploadAfter = 4 // consume the magic, then abort
	s := logarchive.New(blobs, logarchive.WithFrameRecords(1))

	w, err := s.NewWriter(ctx, "logs/_/sess_test/9.mla", "")
	require.NoError(t, err)

	// The first append that flushes a frame meets the dead pipe.
	var appendErr error
	for i := 0; i < 5 && appendErr == nil; i++ {
		appendErr = w.Append(ctx, testLogs(1, "sess_test"))
	}
	require.Error(t, appendErr)

	// Every subsequent call reports the same failure rather than pretending.
	require.Error(t, w.Append(ctx, testLogs(1, "sess_test")))

	src, err := logarchive.New(newMemoryBlobs()).Open(ctx, archiveRow("missing", nil, false))
	require.Error(t, err)
	assert.Nil(t, src)

	_, closeErr := w.Close(ctx)
	require.Error(t, closeErr)
}

func TestCopyFramesRefusesAFailedWriter(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	s := logarchive.New(blobs)

	key := "logs/_/sess_test/3.mla"
	writeArchive(t, s, key, "", testLogs(3, "sess_test"))
	src, err := s.Open(ctx, archiveRow(key, nil, false))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	broken := logarchive.New(blobs, logarchive.WithEncryptor(failingEncryptor{onEncrypt: true}),
		logarchive.WithFrameRecords(1))
	w, err := broken.NewWriter(ctx, "logs/_/sess_test/6.mla", "")
	require.NoError(t, err)
	require.Error(t, w.Append(ctx, testLogs(1, "sess_test")))
	require.Error(t, w.CopyFrames(ctx, src))
	_, err = w.Close(ctx)
	require.Error(t, err)
}

// An object too short to hold the magic is the shape of an upload that died
// immediately. Open must say so rather than treat it as an empty archive.
func TestOpenRejectsShortObject(t *testing.T) {
	ctx := context.Background()
	blobs := newMemoryBlobs()
	require.NoError(t, blobs.Upload(ctx, "logs/_/short/1.mla",
		bytes.NewReader([]byte("ML")), storage.UploadOptions{}))

	s := logarchive.New(blobs)
	_, err := s.Open(ctx, archiveRow("logs/_/short/1.mla", nil, false))
	require.ErrorIs(t, err, logarchive.ErrBadFormat)
}

// Copying frames into an object whose upload has died must fail on the copy,
// not on the Close that happens to notice later.
func TestCopyFramesReportsAWriteFailure(t *testing.T) {
	ctx := context.Background()
	source := newMemoryBlobs()
	s := logarchive.New(source, logarchive.WithFrameRecords(2))

	key := "logs/_/sess_test/20.mla"
	writeArchive(t, s, key, "", testLogs(20, "sess_test"))
	src, err := s.Open(ctx, archiveRow(key, nil, false))
	require.NoError(t, err)
	defer func() { _ = src.Close() }()

	dead := newMemoryBlobs()
	dead.failUploadAfter = 8
	w, err := logarchive.New(dead).NewWriter(ctx, "logs/_/sess_test/copy.mla", "")
	require.NoError(t, err)

	require.Error(t, w.CopyFrames(ctx, src))
	_, err = w.Close(ctx)
	require.Error(t, err)
}

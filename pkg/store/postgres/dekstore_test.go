package postgres_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

func testKEK(t *testing.T) string {
	t.Helper()

	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	return hex.EncodeToString(kek)
}

func newDEKService(t *testing.T) *cryptoutil.Service {
	t.Helper()

	svc, err := cryptoutil.NewService(testKEK(t), pgstore.NewDEKStore(testStore), id.DataKey)
	require.NoError(t, err)
	return svc
}

func newResourceID(t *testing.T) string {
	t.Helper()

	resourceID := "dek-resource-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = testStore.Pool().Exec(context.Background(),
			"DELETE FROM data_keys WHERE resource_id = $1", resourceID)
	})
	return resourceID
}

// TestDEKStoreRoundTrip is the case that never worked: the first encryption for
// a resource. The service asked the store for a DEK, got the store's own
// not-found error, compared it with != against ErrDEKNotFound, and failed
// instead of creating one.
func TestDEKStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newDEKService(t)
	resourceID := newResourceID(t)

	plaintext := []byte("workspace chunk contents")

	ciphertext, err := svc.Encrypt(ctx, "tenant", resourceID, plaintext)
	require.NoError(t, err, "first encryption for a resource must create its DEK")
	assert.NotEqual(t, plaintext, ciphertext)

	got, err := svc.Decrypt(ctx, "tenant", resourceID, ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)

	// The key was actually persisted, not just held in the cache.
	key, err := testStore.GetDataKeyByResource(ctx, "tenant", resourceID)
	require.NoError(t, err)
	assert.NotEmpty(t, key.DEKEncrypted)
	assert.Equal(t, cryptoutil.AlgorithmAES256GCM, key.Algorithm)
}

// TestDEKStoreSurvivesProcessRestart proves the DEK is read back from the
// database rather than remembered: a fresh service with a cold cache must
// decrypt what the previous one wrote.
func TestDEKStoreSurvivesProcessRestart(t *testing.T) {
	ctx := context.Background()
	resourceID := newResourceID(t)
	plaintext := []byte("survives a restart")

	ciphertext, err := newDEKService(t).Encrypt(ctx, "tenant", resourceID, plaintext)
	require.NoError(t, err)

	got, err := newDEKService(t).Decrypt(ctx, "tenant", resourceID, ciphertext)
	require.NoError(t, err, "a cold service must load the stored DEK")
	assert.Equal(t, plaintext, got)
}

func TestDEKStoreGetMissingIsTranslated(t *testing.T) {
	ctx := context.Background()
	dekStore := pgstore.NewDEKStore(testStore)

	_, err := dekStore.GetDEK(ctx, "tenant", "dek-does-not-exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, cryptoutil.ErrDEKNotFound,
		"the store's not-found error must be translated for the service")
}

func TestDEKStoreCreateConflictIsReported(t *testing.T) {
	ctx := context.Background()
	dekStore := pgstore.NewDEKStore(testStore)
	resourceID := newResourceID(t)

	first := &cryptoutil.DataKey{
		ResourceType: "tenant",
		ResourceID:   resourceID,
		DEKEncrypted: "Zmlyc3Q=",
		Algorithm:    cryptoutil.AlgorithmAES256GCM,
	}
	require.NoError(t, dekStore.CreateDEK(ctx, first))

	second := &cryptoutil.DataKey{
		ResourceType: "tenant",
		ResourceID:   resourceID,
		DEKEncrypted: "c2Vjb25k",
		Algorithm:    cryptoutil.AlgorithmAES256GCM,
	}
	err := dekStore.CreateDEK(ctx, second)
	require.Error(t, err, "a second DEK for one resource must be refused")
	assert.ErrorIs(t, err, cryptoutil.ErrDEKExists)

	// The first key stands; the second was not written over it.
	stored, err := dekStore.GetDEK(ctx, "tenant", resourceID)
	require.NoError(t, err)
	assert.Equal(t, "Zmlyc3Q=", stored.DEKEncrypted)
}

// TestDEKConcurrentFirstEncryptAgreesOnOneKey is the race the review flagged:
// several services encrypting for a resource that has no DEK yet. If they each
// keep their own generated key, whatever the losers encrypted can never be read
// back.
func TestDEKConcurrentFirstEncryptAgreesOnOneKey(t *testing.T) {
	ctx := context.Background()
	resourceID := newResourceID(t)

	const services = 6

	// Separate services, so each has its own cache and its own single-flight
	// group — the cross-process race, not the in-process one.
	svcs := make([]*cryptoutil.Service, services)
	for i := range svcs {
		svcs[i] = newDEKService(t)
	}

	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		ciphertexts [][]byte
		failures    []error
	)

	start := make(chan struct{})
	for i := 0; i < services; i++ {
		wg.Add(1)
		go func(svc *cryptoutil.Service, n int) {
			defer wg.Done()
			<-start

			ciphertext, err := svc.Encrypt(ctx, "tenant", resourceID, []byte(fmt.Sprintf("payload %d", n)))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			ciphertexts = append(ciphertexts, ciphertext)
		}(svcs[i], i)
	}

	close(start)
	wg.Wait()

	require.Emptyf(t, failures, "concurrent first encryption failed: %v", failures)
	require.Len(t, ciphertexts, services)

	// Exactly one DEK row exists.
	var rows int
	require.NoError(t, testStore.Pool().QueryRow(ctx,
		"SELECT count(*) FROM data_keys WHERE resource_type = 'tenant' AND resource_id = $1",
		resourceID).Scan(&rows))
	assert.Equal(t, 1, rows, "the race produced more than one DEK for one resource")

	// And every ciphertext is readable by a service that knows nothing about
	// who wrote it — which is only true if they all agreed on the same key.
	reader := newDEKService(t)
	for i, ciphertext := range ciphertexts {
		got, err := reader.Decrypt(ctx, "tenant", resourceID, ciphertext)
		require.NoErrorf(t, err, "ciphertext %d was encrypted under a discarded DEK", i)
		assert.Contains(t, string(got), "payload")
	}
}

// TestDEKConcurrentFirstEncryptWithinOneService covers the in-process half:
// one service, many goroutines, one DEK creation.
func TestDEKConcurrentFirstEncryptWithinOneService(t *testing.T) {
	ctx := context.Background()
	svc := newDEKService(t)
	resourceID := newResourceID(t)

	const goroutines = 16

	var wg sync.WaitGroup
	results := make([][]byte, goroutines)
	errs := make([]error, goroutines)

	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			results[n], errs[n] = svc.Encrypt(ctx, "tenant", resourceID, []byte("same plaintext"))
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "goroutine %d failed", i)
	}

	var rows int
	require.NoError(t, testStore.Pool().QueryRow(ctx,
		"SELECT count(*) FROM data_keys WHERE resource_type = 'tenant' AND resource_id = $1",
		resourceID).Scan(&rows))
	assert.Equal(t, 1, rows, "one service created more than one DEK for one resource")

	reader := newDEKService(t)
	for i, ciphertext := range results {
		got, err := reader.Decrypt(ctx, "tenant", resourceID, ciphertext)
		require.NoErrorf(t, err, "ciphertext %d is unreadable", i)
		assert.Equal(t, "same plaintext", string(got))
	}
}

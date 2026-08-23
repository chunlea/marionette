package cryptoutil

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// scriptedDEKStore is a DEKStore whose responses a test controls exactly, so
// the races the real store only reaches occasionally can be reproduced every
// run.
type scriptedDEKStore struct {
	mu sync.Mutex

	// stored is what a successful GetDEK returns once set.
	stored *DataKey

	// notFoundErr is returned while stored is nil. It is deliberately wrapped:
	// a real store adds context, and an equality check against the sentinel
	// silently stops matching the moment anyone does.
	notFoundErr error

	// conflictOnCreate makes every CreateDEK report ErrDEKExists, as a store
	// does when another process inserted the row first. The row the winner
	// inserted becomes visible at that moment, not before — reads before the
	// conflict must still miss, or the conflict path is never reached.
	conflictOnCreate bool
	onConflictStored *DataKey

	creates atomic.Int64
	gets    atomic.Int64
}

func (s *scriptedDEKStore) GetDEK(_ context.Context, _, _ string) (*DataKey, error) {
	s.gets.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stored == nil {
		return nil, s.notFoundErr
	}
	return s.stored, nil
}

func (s *scriptedDEKStore) CreateDEK(_ context.Context, dk *DataKey) error {
	s.creates.Add(1)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conflictOnCreate || s.stored != nil {
		if s.onConflictStored != nil {
			s.stored = s.onConflictStored
		}
		return fmt.Errorf("inserting data key: %w", ErrDEKExists)
	}

	clone := *dk
	s.stored = &clone
	return nil
}

func (s *scriptedDEKStore) UpdateDEK(_ context.Context, dk *DataKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	clone := *dk
	s.stored = &clone
	return nil
}

// setStoredAfterConflict arms the row the winner created: invisible to reads
// until CreateDEK conflicts, which is when it really came into existence.
func (s *scriptedDEKStore) setStoredAfterConflict(dk *DataKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onConflictStored = dk
}

// storedKey returns what the store currently holds.
func (s *scriptedDEKStore) storedKey() *DataKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stored
}

func newScriptedService(t *testing.T, store *scriptedDEKStore) *Service {
	t.Helper()

	kek, err := GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK: %v", err)
	}

	svc, err := NewService(kek, store, func() string { return "dek_test" })
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// TestFirstEncryptToleratesWrappedNotFound is the shape of the original bug: a
// store that wraps its not-found error. The service compared with != and so
// read "no key yet" as "the store is broken", failing the very first
// encryption for every resource.
func TestFirstEncryptToleratesWrappedNotFound(t *testing.T) {
	store := &scriptedDEKStore{
		notFoundErr: fmt.Errorf("data key for tenant/acme: %w", ErrDEKNotFound),
	}
	svc := newScriptedService(t, store)

	ciphertext, err := svc.Encrypt(context.Background(), "tenant", "acme", []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v, want a DEK to be created", err)
	}
	if store.creates.Load() != 1 {
		t.Errorf("CreateDEK called %d times, want 1", store.creates.Load())
	}

	got, err := svc.Decrypt(context.Background(), "tenant", "acme", ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(got) != "secret" {
		t.Errorf("Decrypt() = %q, want %q", got, "secret")
	}
}

// TestFirstEncryptPropagatesRealStoreFailures is the other half: an error that
// is not "missing" must not be mistaken for one, or a broken store quietly
// produces a second DEK for a resource that already has one.
func TestFirstEncryptPropagatesRealStoreFailures(t *testing.T) {
	boom := errors.New("connection refused")
	store := &scriptedDEKStore{notFoundErr: boom}
	svc := newScriptedService(t, store)

	_, err := svc.Encrypt(context.Background(), "tenant", "acme", []byte("secret"))
	if !errors.Is(err, boom) {
		t.Fatalf("Encrypt() error = %v, want it to surface %v", err, boom)
	}
	if store.creates.Load() != 0 {
		t.Errorf("a DEK was created despite the store failing")
	}
}

// TestLoserOfCreateRaceAdoptsTheStoredKey covers the cross-process race
// deterministically: this service generates a key, loses the insert, and must
// then use the winner's key. Keeping its own would make everything it encrypts
// unreadable to every other process.
func TestLoserOfCreateRaceAdoptsTheStoredKey(t *testing.T) {
	ctx := context.Background()

	// Both services share a KEK, as two processes of one deployment would.
	kek, err := GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK: %v", err)
	}

	// The winner: creates and stores a DEK.
	winnerStore := &scriptedDEKStore{notFoundErr: ErrDEKNotFound}
	winner, err := NewService(kek, winnerStore, func() string { return "dek_winner" })
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	ciphertext, err := winner.Encrypt(ctx, "tenant", "acme", []byte("written by the winner"))
	if err != nil {
		t.Fatalf("winner Encrypt: %v", err)
	}

	// The loser: sees no key when it reads, then loses the insert.
	loserStore := &scriptedDEKStore{notFoundErr: ErrDEKNotFound, conflictOnCreate: true}
	loser, err := NewService(kek, loserStore, func() string { return "dek_loser" })
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// After the failed insert its next read finds the winner's row.
	loserStore.setStoredAfterConflict(winnerStore.storedKey())

	loserCiphertext, err := loser.Encrypt(ctx, "tenant", "acme", []byte("written by the loser"))
	if err != nil {
		t.Fatalf("loser Encrypt: %v", err)
	}

	// Each must be able to read the other: they agreed on one key.
	got, err := winner.Decrypt(ctx, "tenant", "acme", loserCiphertext)
	if err != nil {
		t.Fatalf("the loser encrypted under a key it did not persist: %v", err)
	}
	if string(got) != "written by the loser" {
		t.Errorf("Decrypt() = %q", got)
	}

	if _, err := loser.Decrypt(ctx, "tenant", "acme", ciphertext); err != nil {
		t.Fatalf("the loser cannot read the winner's ciphertext: %v", err)
	}
}

// TestConcurrentFirstEncryptCreatesOneKey pins the single-flight behaviour: N
// goroutines hitting a resource with no DEK must produce one creation, not N.
// Correctness survives without it — the losers adopt the winner — but every
// extra creation is a wasted key generation and a wasted insert.
func TestConcurrentFirstEncryptCreatesOneKey(t *testing.T) {
	store := &scriptedDEKStore{notFoundErr: fmt.Errorf("missing: %w", ErrDEKNotFound)}
	svc := newScriptedService(t, store)

	const goroutines = 32

	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, errs[n] = svc.Encrypt(context.Background(), "tenant", "acme", []byte("payload"))
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	if got := store.creates.Load(); got != 1 {
		t.Errorf("CreateDEK called %d times, want 1: concurrent first use is not being collapsed", got)
	}
}

func TestDEKCacheIsBounded(t *testing.T) {
	cache := newDEKCache(4)

	for i := 0; i < 50; i++ {
		cache.put(fmt.Sprintf("tenant:%d", i), []byte("key"))
	}

	if got := cache.len(); got > 4 {
		t.Errorf("cache holds %d entries, want at most 4", got)
	}
}

func TestInvalidateDropsCachedKey(t *testing.T) {
	ctx := context.Background()
	store := &scriptedDEKStore{notFoundErr: ErrDEKNotFound}
	svc := newScriptedService(t, store)

	if _, err := svc.Encrypt(ctx, "tenant", "acme", []byte("payload")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	before := store.gets.Load()
	if _, err := svc.Encrypt(ctx, "tenant", "acme", []byte("payload")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if store.gets.Load() != before {
		t.Errorf("a cached DEK was re-read from the store")
	}

	svc.InvalidateDEK("tenant", "acme")

	if _, err := svc.Encrypt(ctx, "tenant", "acme", []byte("payload")); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if store.gets.Load() == before {
		t.Errorf("InvalidateDEK did not force a re-read")
	}
}

package cryptoutil

import "sync"

// defaultDEKCacheSize bounds how many decrypted DEKs are held in memory.
//
// The previous cache was a sync.Map with no bound and no invalidation: one
// entry per resource, held forever. In a multi-tenant deployment that grows
// with the tenant count, it is plaintext key material, and a rotated key stayed
// in use until the process restarted.
const defaultDEKCacheSize = 1024

// dekCache is a bounded cache of decrypted DEKs.
//
// Eviction is arbitrary rather than least-recently-used: a DEK is cheap to
// re-read and the cache exists to avoid a round trip per encryption, not to
// guarantee a hit. Keeping it simple avoids a second data structure to keep
// consistent under concurrency.
type dekCache struct {
	mu      sync.RWMutex
	entries map[string][]byte
	maxSize int
}

func newDEKCache(maxSize int) *dekCache {
	if maxSize <= 0 {
		maxSize = defaultDEKCacheSize
	}
	return &dekCache{
		entries: make(map[string][]byte, maxSize),
		maxSize: maxSize,
	}
}

func (c *dekCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	dek, ok := c.entries[key]
	return dek, ok
}

func (c *dekCache) put(key string, dek []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		// Drop one entry to make room. Map iteration order is unspecified,
		// which is exactly the arbitrary choice wanted here.
		for evict := range c.entries {
			delete(c.entries, evict)
			break
		}
	}

	c.entries[key] = dek
}

func (c *dekCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

func (c *dekCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string][]byte, c.maxSize)
}

func (c *dekCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

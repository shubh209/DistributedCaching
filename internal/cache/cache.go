package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/user/distributed-caching-go/internal/shared"
)

// InMemoryCache is a thread-safe, capacity-bounded, TTL-aware cache backed by
// a Go map. It delegates eviction decisions to a pluggable EvictionPolicy so
// that LRU, LFU, TTL-only, and Random can all be swapped in without changing
// any of the cache logic.
type InMemoryCache struct {
	mu       sync.RWMutex              // allows many concurrent readers, one writer
	entries  map[string]*CacheEntry    // the actual data store: key → entry
	capacity int                       // maximum number of entries allowed
	policy   shared.EvictionPolicy     // pluggable: LRU, LFU, TTL-only, or Random
}

// New creates a new InMemoryCache with the given capacity and eviction policy.
// capacity must be between 1 and 1,000,000 (enforced by config loader).
func New(capacity int, policy shared.EvictionPolicy) *InMemoryCache {
	return &InMemoryCache{
		entries:  make(map[string]*CacheEntry, capacity),
		capacity: capacity,
		policy:   policy,
	}
}

// Get retrieves a value by key.
//
// On a hit:  returns (value, true, nil)
// On a miss: returns (nil, false, nil)  ← a miss is NOT an error
// On expiry: deletes the entry lazily and returns (nil, false, nil)
//
// Lazy expiry means we only check TTL when a key is actually accessed,
// rather than running a background goroutine scanning all entries.
// This is the same approach Redis uses.
func (c *InMemoryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	// Optimistic read lock — allows concurrent reads without blocking each other
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false, nil // clean miss
	}

	// Check TTL expiry. If expired, upgrade to a write lock and delete.
	if entry.IsExpired(time.Now()) {
		c.mu.Lock()
		// Re-check after acquiring write lock (another goroutine may have already deleted it)
		if e, ok := c.entries[key]; ok && e.IsExpired(time.Now()) {
			delete(c.entries, key)
			c.policy.OnEvict(key)
		}
		c.mu.Unlock()
		return nil, false, nil // expired = miss
	}

	// Cache hit — update access metadata and notify the eviction policy
	c.mu.Lock()
	entry.AccessedAt = time.Now()
	entry.AccessCount++
	c.policy.OnAccess(key)
	c.mu.Unlock()

	return entry.Value, true, nil
}

// Set stores a key-value pair with a TTL.
//
// TTL rules (from requirements):
//   - TTL must be >= 1 second (enforced here)
//   - TTL = 0 is treated as immediately expired
//   - TTL > 86400 seconds (24h) is rejected
//
// If the key already exists, Set upserts: replaces the value and resets the TTL.
// If the cache is at capacity and the key is new, one entry is evicted first.
func (c *InMemoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	// Validate TTL bounds
	if ttl < 0 {
		return fmt.Errorf("cache: negative TTL %v is not allowed", ttl)
	}
	if ttl > 86400*time.Second {
		return fmt.Errorf("cache: TTL %v exceeds maximum of 86400 seconds", ttl)
	}

	now := time.Now()

	// Calculate absolute expiry time.
	// TTL=0 means immediately expired — we set ExpiresAt to now so IsExpired returns true.
	var expiresAt time.Time
	if ttl == 0 {
		expiresAt = now // immediately expired
	} else {
		expiresAt = now.Add(ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	existing, isUpdate := c.entries[key]

	if isUpdate {
		// Upsert: update value and reset TTL on the existing entry
		existing.Value = value
		existing.ExpiresAt = expiresAt
		existing.AccessedAt = now
		existing.AccessCount++
		c.policy.OnAccess(key) // treat upsert as an access for LRU/LFU tracking
		return nil
	}

	// New key: check capacity and evict if needed
	if len(c.entries) >= c.capacity {
		evictKey, ok := c.policy.Evict()
		if ok {
			delete(c.entries, evictKey)
			c.policy.OnEvict(evictKey)
		}
	}

	// Insert the new entry
	entry := &CacheEntry{
		Key:         key,
		Value:       value,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		AccessedAt:  now,
		AccessCount: 1,
	}
	c.entries[key] = entry
	c.policy.OnInsert(key, expiresAt)

	return nil
}

// Delete removes a key from the cache.
// Deleting a non-existent key is a no-op — not an error.
func (c *InMemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		delete(c.entries, key)
		c.policy.OnEvict(key) // notify policy to clean up its internal tracking
	}
	return nil
}

// Len returns the current number of entries in the cache.
// Note: this includes entries that may have expired but haven't been lazily cleaned up yet.
func (c *InMemoryCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

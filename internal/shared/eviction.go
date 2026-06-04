package shared

import "time"

// EvictionPolicy determines which cache entry is removed when the cache is full.
// All four policies (LRU, LFU, TTL-only, Random) implement this interface.
// The cache calls these methods to keep the policy's internal tracking in sync.
type EvictionPolicy interface {
	// OnAccess is called on every Get or Set for a key that already exists.
	// Policies like LRU use this to update the "last used" timestamp.
	OnAccess(key string)

	// OnInsert is called when a brand new key is added to the cache.
	// Policies use this to start tracking the new entry.
	OnInsert(key string, expiresAt time.Time)

	// OnEvict is called when an entry is removed for any reason (TTL expiry,
	// explicit delete, or capacity eviction). Policies use this to clean up
	// their internal tracking data so they don't leak memory.
	OnEvict(key string)

	// Evict selects and returns the key that should be removed when the cache
	// is at capacity. The cache will then delete that entry and call OnEvict.
	// Returns ok=false only if the policy has no entries to evict.
	Evict() (key string, ok bool)

	// Name returns the policy's identifier, used as a Prometheus metric label
	// so all four policies can be compared on the same Grafana dashboard.
	Name() string
}

package cache

import "time"

// CacheEntry holds a single cached key-value pair along with the metadata
// needed by eviction policies to make their decisions.
type CacheEntry struct {
	Key   string
	Value []byte // JSON-encoded payload — the actual cached data

	ExpiresAt   time.Time // absolute time when this entry becomes invalid; zero = no expiry
	CreatedAt   time.Time // when this entry was first inserted
	AccessedAt  time.Time // when this entry was last read or written — used by LRU
	AccessCount int64     // how many times this entry has been accessed — used by LFU
}

// IsExpired returns true if the entry has passed its TTL.
// A zero ExpiresAt means the entry never expires.
func (e *CacheEntry) IsExpired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
}

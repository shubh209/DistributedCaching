package shared

import (
	"context"
	"time"
)

// Cache is the interface exposed by any cache implementation to its consumers.
// Both the standalone in-memory cache and the cluster-backed cache implement
// this interface, so the API layer doesn't need to know which one it's talking to.
type Cache interface {
	// Get retrieves a value by key.
	// Returns (value, true, nil) on a cache hit.
	// Returns (nil, false, nil) on a cache miss or expired entry — never an error
	// for a simple miss, because a miss is expected behaviour, not a failure.
	Get(ctx context.Context, key string) (value []byte, found bool, err error)

	// Set stores a key-value pair with a TTL.
	// TTL must be between 1 and 86400 seconds inclusive.
	// A TTL of 0 is treated as immediately expired.
	// If the key already exists, Set upserts: replaces the value and resets the TTL.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a key from the cache.
	// Deleting a non-existent key is a no-op (not an error).
	Delete(ctx context.Context, key string) error

	// Len returns the current number of entries in the cache.
	Len() int
}

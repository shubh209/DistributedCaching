package shared

import "context"

// InvalidationStrategy determines how the cache is kept consistent with the
// source of truth (PostgreSQL) after a write operation.
// All three strategies (TTL, write-through, write-behind) implement this interface.
type InvalidationStrategy interface {
	// OnWrite is called after a successful write to the source of truth.
	// It receives the cache instance, the affected key, and the new serialised value.
	//
	// TTL strategy:       no-op — the cache entry expires naturally via TTL.
	// Write-through:      synchronously updates the cache before returning.
	// Write-behind:       schedules an async cache invalidation and returns immediately.
	//
	// Returning an error from OnWrite does NOT roll back the database write —
	// the DB write has already committed. The caller treats a cache error as
	// non-fatal: the next read will be a cache miss and will re-populate from DB.
	OnWrite(ctx context.Context, cache Cache, key string, value []byte) error

	// Name returns the strategy's identifier, used as a Prometheus metric label
	// so all three strategies can be compared on the same Grafana dashboard.
	Name() string
}

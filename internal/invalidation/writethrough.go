package invalidation

import (
	"context"
	"fmt"
	"time"

	"github.com/user/distributed-caching-go/internal/shared"
)

const writeThroughTTL = 5 * time.Minute // same TTL as the original cache population

// WriteThrough implements InvalidationStrategy with synchronous cache update.
// When a product is updated, the cache is updated in the same request before
// returning to the caller — cache and DB are always in sync.
//
// Trade-off: strong consistency (readers never see stale data after a write),
// but slightly higher write latency because two writes happen synchronously.
//
// IMPORTANT: if the cache update fails, we log and return nil — the DB write
// already committed and is the source of truth. The next read will be a cache
// miss and will re-populate from DB. (Requirement 6.2)
type WriteThrough struct{}

// NewWriteThrough creates a write-through invalidation strategy.
func NewWriteThrough() *WriteThrough { return &WriteThrough{} }

// OnWrite synchronously updates the cache with the new value.
// Called after a successful DB write — both DB and cache end up with
// the same value before this function returns.
func (w *WriteThrough) OnWrite(ctx context.Context, c shared.Cache, key string, value []byte) error {
	if err := c.Set(ctx, key, value, writeThroughTTL); err != nil {
		// Cache update failed — log but don't propagate.
		// DB write already committed; next read will re-populate cache.
		return fmt.Errorf("write-through: cache update failed (non-fatal): %w", err)
	}
	return nil
}

// Name returns the strategy identifier for Prometheus labels.
func (w *WriteThrough) Name() string { return "write-through" }

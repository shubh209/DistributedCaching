package invalidation

import "context"

import "github.com/user/distributed-caching-go/internal/shared"

// TTL implements InvalidationStrategy with a no-op OnWrite.
// The cache entry expires naturally via its TTL — no explicit invalidation
// happens when a product is updated.
//
// Trade-off: simplest and fastest write path, but readers may see stale
// data until the TTL fires. Acceptable when staleness tolerance is high.
type TTL struct{}

// NewTTL creates a TTL-based invalidation strategy.
func NewTTL() *TTL { return &TTL{} }

// OnWrite is a no-op — the cached value stays until its TTL expires.
func (t *TTL) OnWrite(_ context.Context, _ shared.Cache, _ string, _ []byte) error {
	return nil
}

// Name returns the strategy identifier for Prometheus labels.
func (t *TTL) Name() string { return "ttl" }

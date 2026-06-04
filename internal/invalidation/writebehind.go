package invalidation

import (
	"context"
	"log"
	"time"

	"github.com/user/distributed-caching-go/internal/shared"
)

const (
	// writeBehindDeadline is the maximum time the async deletion can take.
	// Per requirement 6.3: must complete within 5 seconds.
	writeBehindDeadline = 5 * time.Second

	// writeBehindDelay is how long to wait before deleting.
	// A small delay lets any in-flight reads complete before we invalidate.
	writeBehindDelay = 100 * time.Millisecond
)

// WriteBehind implements InvalidationStrategy with asynchronous cache invalidation.
// OnWrite returns immediately (fast write path), then a background goroutine
// deletes the cache entry within writeBehindDeadline.
//
// We DELETE rather than SET because by the time the goroutine runs, a new
// read may have already re-populated the cache with fresh data from DB.
// Deleting forces the next read to fetch the latest value — safer than
// potentially overwriting a fresher value.
//
// Trade-off: fast writes, but readers may see stale data for up to 5 seconds.
// If async deletion fails, the entry expires via its natural TTL. (Requirement 6.3)
type WriteBehind struct{}

// NewWriteBehind creates a write-behind invalidation strategy.
func NewWriteBehind() *WriteBehind { return &WriteBehind{} }

// OnWrite schedules an async cache deletion and returns immediately.
// The caller gets a fast response — cache invalidation happens in the background.
func (w *WriteBehind) OnWrite(_ context.Context, c shared.Cache, key string, _ []byte) error {
	go func() {
		// Small delay to let in-flight reads complete
		time.Sleep(writeBehindDelay)

		// Create a fresh context with the deadline — the original request
		// context may already be cancelled by the time this goroutine runs.
		ctx, cancel := context.WithTimeout(context.Background(), writeBehindDeadline)
		defer cancel()

		if err := c.Delete(ctx, key); err != nil {
			// Async deletion failed — log and let natural TTL handle expiry.
			// Per requirement 6.3: no panic, no retry storm.
			log.Printf("write-behind: async delete failed for key %q (will expire via TTL): %v", key, err)
		}
	}()

	// Return immediately — don't wait for the goroutine
	return nil
}

// Name returns the strategy identifier for Prometheus labels.
func (w *WriteBehind) Name() string { return "write-behind" }

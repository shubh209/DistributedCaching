package invalidation

import (
	"context"
	"testing"
	"time"

	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/eviction"
	"github.com/user/distributed-caching-go/internal/shared"
)

// Compile-time interface assertions — if any strategy stops satisfying
// InvalidationStrategy, this file will fail to compile immediately.
var (
	_ shared.InvalidationStrategy = (*TTL)(nil)
	_ shared.InvalidationStrategy = (*WriteThrough)(nil)
	_ shared.InvalidationStrategy = (*WriteBehind)(nil)
)

// newTestCache creates a small in-memory cache for testing.
func newTestCache() shared.Cache {
	return cache.New(100, eviction.NewLRU())
}

func ctx() context.Context { return context.Background() }

// --- TTL strategy tests ---

func TestTTL_OnWrite_IsNoOp(t *testing.T) {
	c := newTestCache()
	key := "product:1"
	original := []byte(`{"id":"1","name":"Original"}`)
	updated := []byte(`{"id":"1","name":"Updated"}`)

	// Pre-populate the cache
	c.Set(ctx(), key, original, 60*time.Second)

	// OnWrite should do nothing
	strategy := NewTTL()
	if err := strategy.OnWrite(ctx(), c, key, updated); err != nil {
		t.Fatalf("TTL.OnWrite returned unexpected error: %v", err)
	}

	// Cache should still contain the original value (no invalidation happened)
	val, found, _ := c.Get(ctx(), key)
	if !found {
		t.Fatal("expected cache entry to still exist after TTL.OnWrite")
	}
	if string(val) != string(original) {
		t.Errorf("expected original value in cache, got: %s", val)
	}
}

func TestTTL_Name(t *testing.T) {
	if NewTTL().Name() != "ttl" {
		t.Error("expected Name()='ttl'")
	}
}

// --- Write-through strategy tests ---

func TestWriteThrough_OnWrite_UpdatesCache(t *testing.T) {
	c := newTestCache()
	key := "product:2"
	original := []byte(`{"id":"2","name":"Original"}`)
	updated := []byte(`{"id":"2","name":"Updated"}`)

	// Pre-populate the cache with old value
	c.Set(ctx(), key, original, 60*time.Second)

	strategy := NewWriteThrough()
	if err := strategy.OnWrite(ctx(), c, key, updated); err != nil {
		// Write-through returns an error on cache failure but it's non-fatal
		// In this test the cache is healthy so no error expected
		t.Fatalf("WriteThrough.OnWrite returned unexpected error: %v", err)
	}

	// Cache should now contain the updated value
	val, found, _ := c.Get(ctx(), key)
	if !found {
		t.Fatal("expected cache entry to exist after write-through")
	}
	if string(val) != string(updated) {
		t.Errorf("expected updated value in cache, got: %s", val)
	}
}

func TestWriteThrough_OnWrite_NewKey_PopulatesCache(t *testing.T) {
	c := newTestCache()
	key := "product:3"
	value := []byte(`{"id":"3","name":"New Product"}`)

	strategy := NewWriteThrough()
	strategy.OnWrite(ctx(), c, key, value)

	val, found, _ := c.Get(ctx(), key)
	if !found {
		t.Fatal("expected cache to be populated for new key via write-through")
	}
	if string(val) != string(value) {
		t.Errorf("expected %s, got %s", value, val)
	}
}

func TestWriteThrough_Name(t *testing.T) {
	if NewWriteThrough().Name() != "write-through" {
		t.Error("expected Name()='write-through'")
	}
}

// --- Write-behind strategy tests ---

func TestWriteBehind_OnWrite_ReturnsImmediately(t *testing.T) {
	c := newTestCache()
	key := "product:4"
	value := []byte(`{"id":"4","price":99.99}`)

	c.Set(ctx(), key, value, 60*time.Second)

	strategy := NewWriteBehind()
	start := time.Now()
	strategy.OnWrite(ctx(), c, key, value)
	elapsed := time.Since(start)

	// OnWrite should return in well under 1ms — it just launches a goroutine
	if elapsed > 50*time.Millisecond {
		t.Errorf("write-behind OnWrite took %v — should return immediately", elapsed)
	}

	// Cache entry should still be present immediately after OnWrite
	// (the async goroutine hasn't fired yet)
	_, found, _ := c.Get(ctx(), key)
	if !found {
		t.Error("cache entry should still exist immediately after write-behind OnWrite")
	}
}

func TestWriteBehind_OnWrite_DeletesEntryAsync(t *testing.T) {
	c := newTestCache()
	key := "product:5"
	value := []byte(`{"id":"5","price":49.99}`)

	c.Set(ctx(), key, value, 60*time.Second)

	strategy := NewWriteBehind()
	strategy.OnWrite(ctx(), c, key, value)

	// Wait for the async goroutine to complete (delay + some buffer)
	// writeBehindDelay=100ms + buffer=500ms = well within 5s requirement
	time.Sleep(writeBehindDelay + 500*time.Millisecond)

	_, found, _ := c.Get(ctx(), key)
	if found {
		t.Error("expected cache entry to be deleted by write-behind goroutine")
	}
}

func TestWriteBehind_Name(t *testing.T) {
	if NewWriteBehind().Name() != "write-behind" {
		t.Error("expected Name()='write-behind'")
	}
}

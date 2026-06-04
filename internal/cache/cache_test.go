package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/user/distributed-caching-go/internal/eviction"
)

// --- helpers ---

func newTestCache(capacity int) *InMemoryCache {
	return New(capacity, eviction.NewLRU())
}

func ctx() context.Context { return context.Background() }

// --- TTL tests ---

func TestGet_MissingKey_ReturnsFalse(t *testing.T) {
	c := newTestCache(10)
	val, found, err := c.Get(ctx(), "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for missing key")
	}
	if val != nil {
		t.Error("expected nil value for missing key")
	}
}

func TestSet_ThenGet_ReturnsValue(t *testing.T) {
	c := newTestCache(10)
	want := []byte(`{"id":"1"}`)
	if err := c.Set(ctx(), "product:1", want, 60*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, found, err := c.Get(ctx(), "product:1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if string(got) != string(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestGet_ExpiredKey_ReturnsFalse(t *testing.T) {
	c := newTestCache(10)
	// Set a very short TTL and wait for it to expire
	if err := c.Set(ctx(), "key", []byte("val"), 1*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // wait for expiry
	_, found, err := c.Get(ctx(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected found=false for expired key")
	}
}

func TestSet_ZeroTTL_TreatedAsExpired(t *testing.T) {
	c := newTestCache(10)
	if err := c.Set(ctx(), "key", []byte("val"), 0); err != nil {
		t.Fatal(err)
	}
	_, found, _ := c.Get(ctx(), "key")
	if found {
		t.Error("TTL=0 should be treated as immediately expired")
	}
}

func TestSet_NegativeTTL_ReturnsError(t *testing.T) {
	c := newTestCache(10)
	err := c.Set(ctx(), "key", []byte("val"), -1*time.Second)
	if err == nil {
		t.Error("expected error for negative TTL")
	}
}

func TestSet_ExceedsMaxTTL_ReturnsError(t *testing.T) {
	c := newTestCache(10)
	err := c.Set(ctx(), "key", []byte("val"), 86401*time.Second)
	if err == nil {
		t.Error("expected error for TTL > 86400s")
	}
}

// --- Upsert tests ---

func TestSet_ExistingKey_UpdatesValueAndTTL(t *testing.T) {
	c := newTestCache(10)
	c.Set(ctx(), "k", []byte("old"), 10*time.Second)
	c.Set(ctx(), "k", []byte("new"), 60*time.Second) // upsert
	val, found, _ := c.Get(ctx(), "k")
	if !found {
		t.Fatal("expected found=true after upsert")
	}
	if string(val) != "new" {
		t.Errorf("expected 'new', got '%s'", val)
	}
}

// --- Capacity tests ---

func TestCache_CapacityNeverExceeded(t *testing.T) {
	capacity := 5
	c := newTestCache(capacity)
	for i := 0; i < 10; i++ {
		key := string(rune('a' + i))
		c.Set(ctx(), key, []byte("v"), 60*time.Second)
	}
	if c.Len() > capacity {
		t.Errorf("cache len %d exceeds capacity %d", c.Len(), capacity)
	}
}

// --- Delete tests ---

func TestDelete_ExistingKey_RemovesIt(t *testing.T) {
	c := newTestCache(10)
	c.Set(ctx(), "k", []byte("v"), 60*time.Second)
	c.Delete(ctx(), "k")
	_, found, _ := c.Get(ctx(), "k")
	if found {
		t.Error("expected found=false after delete")
	}
}

func TestDelete_MissingKey_NoError(t *testing.T) {
	c := newTestCache(10)
	if err := c.Delete(ctx(), "nonexistent"); err != nil {
		t.Errorf("expected no error deleting missing key, got: %v", err)
	}
}

// --- Concurrency test ---

func TestCache_ConcurrentAccess_NoRace(t *testing.T) {
	// Run with: go test -race ./internal/cache/...
	c := newTestCache(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + (n % 26)))
			c.Set(ctx(), key, []byte("v"), 60*time.Second)
			c.Get(ctx(), key)
			c.Delete(ctx(), key)
		}(i)
	}
	wg.Wait()
}

package eviction

import (
	"testing"
	"time"
)

// --- LRU tests ---

func TestLRU_Evict_ReturnsLeastRecentlyUsed(t *testing.T) {
	lru := NewLRU()
	now := time.Now()

	// Insert A, B, C in order
	lru.OnInsert("A", now.Add(60*time.Second))
	lru.OnInsert("B", now.Add(60*time.Second))
	lru.OnInsert("C", now.Add(60*time.Second))
	// Order: head ↔ C ↔ B ↔ A ↔ tail  (A is least recently used)

	key, ok := lru.Evict()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if key != "A" {
		t.Errorf("expected LRU to evict 'A', got '%s'", key)
	}
}

func TestLRU_OnAccess_MovesKeyToFront(t *testing.T) {
	lru := NewLRU()
	now := time.Now().Add(60 * time.Second)

	lru.OnInsert("A", now)
	lru.OnInsert("B", now)
	lru.OnInsert("C", now)
	// Order: C(front) ↔ B ↔ A(back=LRU)

	// Access A — it should move to front, making B the new LRU
	lru.OnAccess("A")
	// Order: A(front) ↔ C ↔ B(back=LRU)

	key, _ := lru.Evict()
	if key != "B" {
		t.Errorf("expected LRU to evict 'B' after accessing 'A', got '%s'", key)
	}
}

func TestLRU_OnEvict_RemovesKey(t *testing.T) {
	lru := NewLRU()
	lru.OnInsert("A", time.Now().Add(time.Minute))
	lru.OnEvict("A")

	_, ok := lru.Evict()
	if ok {
		t.Error("expected empty LRU after evicting only entry")
	}
}

func TestLRU_EmptyEvict_ReturnsFalse(t *testing.T) {
	lru := NewLRU()
	_, ok := lru.Evict()
	if ok {
		t.Error("expected ok=false for empty LRU")
	}
}

func TestLRU_Name(t *testing.T) {
	if NewLRU().Name() != "lru" {
		t.Error("expected Name()='lru'")
	}
}

// --- LFU tests ---

func TestLFU_Evict_ReturnsLowestFrequency(t *testing.T) {
	lfu := NewLFU()

	lfu.OnInsert("A", time.Now().Add(time.Minute)) // freq=1
	lfu.OnInsert("B", time.Now().Add(time.Minute)) // freq=1
	lfu.OnInsert("C", time.Now().Add(time.Minute)) // freq=1

	// Access B and C extra times
	lfu.OnAccess("B") // B freq=2
	lfu.OnAccess("C") // C freq=2
	lfu.OnAccess("C") // C freq=3

	// A has freq=1 — should be evicted
	key, ok := lfu.Evict()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if key != "A" {
		t.Errorf("expected LFU to evict 'A' (freq=1), got '%s'", key)
	}
}

func TestLFU_TieBreak_EvictsOldestAccess(t *testing.T) {
	lfu := NewLFU()

	// Insert A first (older timestamp), then B
	lfu.OnInsert("A", time.Now().Add(time.Minute))
	time.Sleep(2 * time.Millisecond)
	lfu.OnInsert("B", time.Now().Add(time.Minute))
	// Both have freq=1, A has older accessedAt

	key, _ := lfu.Evict()
	if key != "A" {
		t.Errorf("expected LFU to evict 'A' (older access) on tie, got '%s'", key)
	}
}

func TestLFU_Name(t *testing.T) {
	if NewLFU().Name() != "lfu" {
		t.Error("expected Name()='lfu'")
	}
}

// --- TTL-only tests ---

func TestTTLOnly_Evict_ReturnsSoonestExpiry(t *testing.T) {
	ttl := NewTTLOnly()
	now := time.Now()

	ttl.OnInsert("long",  now.Add(10*time.Minute))
	ttl.OnInsert("short", now.Add(1*time.Second))  // expires soonest
	ttl.OnInsert("mid",   now.Add(1*time.Minute))

	key, ok := ttl.Evict()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if key != "short" {
		t.Errorf("expected TTL-only to evict 'short', got '%s'", key)
	}
}

func TestTTLOnly_OnAccess_IsNoOp(t *testing.T) {
	ttl := NewTTLOnly()
	now := time.Now()

	ttl.OnInsert("A", now.Add(10*time.Minute))
	ttl.OnInsert("B", now.Add(1*time.Second)) // B expires soonest

	// Accessing A many times should NOT change eviction order
	ttl.OnAccess("A")
	ttl.OnAccess("A")
	ttl.OnAccess("A")

	key, _ := ttl.Evict()
	if key != "B" {
		t.Errorf("OnAccess should be no-op for TTL-only; expected 'B', got '%s'", key)
	}
}

func TestTTLOnly_Name(t *testing.T) {
	if NewTTLOnly().Name() != "ttl" {
		t.Error("expected Name()='ttl'")
	}
}

// --- Random tests ---

func TestRandom_Evict_ReturnsExistingKey(t *testing.T) {
	r := NewRandom()
	keys := []string{"A", "B", "C", "D", "E"}
	keySet := make(map[string]bool)
	for _, k := range keys {
		r.OnInsert(k, time.Now().Add(time.Minute))
		keySet[k] = true
	}

	// Run many evictions to verify randomness doesn't return invalid keys
	for i := 0; i < 100; i++ {
		// Re-insert all keys (simulating a full cache that needs eviction)
		r2 := NewRandom()
		for _, k := range keys {
			r2.OnInsert(k, time.Now().Add(time.Minute))
		}
		key, ok := r2.Evict()
		if !ok {
			t.Fatal("expected ok=true")
		}
		if !keySet[key] {
			t.Errorf("random eviction returned key '%s' which was never inserted", key)
		}
	}
}

func TestRandom_OnEvict_RemovesKey(t *testing.T) {
	r := NewRandom()
	r.OnInsert("only", time.Now().Add(time.Minute))
	r.OnEvict("only")

	_, ok := r.Evict()
	if ok {
		t.Error("expected ok=false after evicting only entry")
	}
}

func TestRandom_Name(t *testing.T) {
	if NewRandom().Name() != "random" {
		t.Error("expected Name()='random'")
	}
}

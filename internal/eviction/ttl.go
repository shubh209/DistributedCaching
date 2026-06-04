package eviction

import (
	"container/heap"
	"time"
)

// ttlEntry holds the data tracked per key for TTL-only eviction decisions.
type ttlEntry struct {
	key       string
	expiresAt time.Time // absolute expiry time — the only thing TTL-only cares about
	index     int       // position in the heap array
}

// ttlHeap is a min-heap of ttlEntry pointers ordered by ExpiresAt ASC.
// The entry at index 0 is always the one expiring soonest → evict this first.
type ttlHeap []*ttlEntry

func (h ttlHeap) Len() int           { return len(h) }
func (h ttlHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h ttlHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *ttlHeap) Push(x any) {
	entry := x.(*ttlEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}
func (h *ttlHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	entry.index = -1
	return entry
}

// TTLOnly implements the EvictionPolicy interface using a min-heap ordered by
// expiry time. The entry expiring soonest is always evicted first.
//
// This policy makes no assumptions about access patterns — it only cares about
// data freshness. An entry accessed 10,000 times will still be evicted if its
// TTL is the shortest in the cache.
type TTLOnly struct {
	h       ttlHeap              // min-heap ordered by ExpiresAt
	entries map[string]*ttlEntry // key → heap entry for O(1) lookup
}

// NewTTLOnly creates a new TTL-only eviction policy.
func NewTTLOnly() *TTLOnly {
	h := make(ttlHeap, 0)
	heap.Init(&h)
	return &TTLOnly{
		h:       h,
		entries: make(map[string]*ttlEntry),
	}
}

// OnAccess is a no-op for TTL-only — recency of access is irrelevant.
// This policy only cares about when entries expire, not when they were used.
func (t *TTLOnly) OnAccess(_ string) {}

// OnInsert adds the new entry to the heap keyed by its expiry time.
func (t *TTLOnly) OnInsert(key string, expiresAt time.Time) {
	entry := &ttlEntry{key: key, expiresAt: expiresAt}
	t.entries[key] = entry
	heap.Push(&t.h, entry)
}

// OnEvict removes the entry from the heap and lookup map.
func (t *TTLOnly) OnEvict(key string) {
	entry, ok := t.entries[key]
	if !ok {
		return
	}
	heap.Remove(&t.h, entry.index)
	delete(t.entries, key)
}

// Evict returns the key expiring soonest — top of the min-heap.
// Returns ok=false only if the heap is empty.
func (t *TTLOnly) Evict() (string, bool) {
	if t.h.Len() == 0 {
		return "", false
	}
	return t.h[0].key, true
}

// Name returns the policy identifier used for Prometheus metric labels.
func (t *TTLOnly) Name() string {
	return "ttl"
}

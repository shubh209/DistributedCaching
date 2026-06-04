package eviction

import (
	"container/heap"
	"time"
)

// lfuEntry holds the data tracked per key for LFU decisions.
type lfuEntry struct {
	key        string
	freq       int64     // total number of Get or Set operations on this key
	accessedAt time.Time // last access time — used to break frequency ties
	index      int       // position in the heap array (maintained by heap.Interface)
}

// lfuHeap is a min-heap of lfuEntry pointers ordered by (freq ASC, accessedAt ASC).
// The entry at index 0 is always the one to evict:
//   - lowest frequency first
//   - if tied, oldest last-access time first (behaves like LRU among equals)
type lfuHeap []*lfuEntry

// --- heap.Interface implementation ---
// Go's container/heap requires these 5 methods to manage the heap automatically.

func (h lfuHeap) Len() int { return len(h) }

func (h lfuHeap) Less(i, j int) bool {
	// Primary sort: lower frequency = higher eviction priority
	if h[i].freq != h[j].freq {
		return h[i].freq < h[j].freq
	}
	// Tie-break: older last-access time = higher eviction priority
	// (among equally frequent entries, evict the one unused for longer)
	return h[i].accessedAt.Before(h[j].accessedAt)
}

func (h lfuHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	// Keep index fields in sync so we can locate entries in O(1) for updates
	h[i].index = i
	h[j].index = j
}

func (h *lfuHeap) Push(x any) {
	entry := x.(*lfuEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *lfuHeap) Pop() any {
	old := *h
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[:n-1]
	entry.index = -1 // mark as removed
	return entry
}

// LFU implements the EvictionPolicy interface using a min-heap ordered by
// access frequency. The least frequently accessed entry is always at the top.
//
// Heap operations are O(log n). This is simpler than the optimal O(1) LFU
// implementation and sufficient for a learning sandbox.
type LFU struct {
	h       lfuHeap              // the min-heap
	entries map[string]*lfuEntry // key → heap entry (O(1) lookup for updates)
}

// NewLFU creates a new LFU eviction policy.
func NewLFU() *LFU {
	h := make(lfuHeap, 0)
	heap.Init(&h)
	return &LFU{
		h:       h,
		entries: make(map[string]*lfuEntry),
	}
}

// OnAccess is called on every Get or Set for an existing key.
// We increment the frequency counter, update the last-access time,
// and fix the heap so ordering stays correct.
func (l *LFU) OnAccess(key string) {
	entry, ok := l.entries[key]
	if !ok {
		return
	}
	entry.freq++
	entry.accessedAt = time.Now()
	// heap.Fix re-sorts the single modified entry — O(log n)
	heap.Fix(&l.h, entry.index)
}

// OnInsert is called when a brand new key enters the cache.
// New entries start at frequency 1 — this is the "new entry problem":
// they're vulnerable to immediate eviction in a heavily loaded cache.
func (l *LFU) OnInsert(key string, _ time.Time) {
	entry := &lfuEntry{
		key:        key,
		freq:       1,
		accessedAt: time.Now(),
	}
	l.entries[key] = entry
	heap.Push(&l.h, entry)
}

// OnEvict is called when a key is removed from the cache for any reason.
// We remove it from the heap and the lookup map.
func (l *LFU) OnEvict(key string) {
	entry, ok := l.entries[key]
	if !ok {
		return
	}
	// heap.Remove removes any element by index in O(log n)
	heap.Remove(&l.h, entry.index)
	delete(l.entries, key)
}

// Evict returns the key at the top of the min-heap — the least frequently used.
// Returns ok=false only if the heap is empty.
func (l *LFU) Evict() (string, bool) {
	if l.h.Len() == 0 {
		return "", false
	}
	// Peek at the top without removing — the cache will call OnEvict after deleting
	return l.h[0].key, true
}

// Name returns the policy identifier used for Prometheus metric labels.
func (l *LFU) Name() string {
	return "lfu"
}

package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// PrefixCacheEntry holds the simulated KV state for a cached token prefix.
// We don't store actual tensors — just the metadata needed to compute savings.
type PrefixCacheEntry struct {
	PrefixHash    string       // SHA-256 of the token sequence
	Model         string       // model name e.g. "Llama-3-70B"
	TokenCount    int          // number of tokens in this prefix
	TFLOPsCost    float64      // attention TFLOPs to compute this prefix from scratch
	CostUSD       float64      // dollar cost of recomputing on H100
	HitCount      int64        // number of times this entry was served from cache
	CachedAt      time.Time
	LastAccessedAt time.Time
}

// SavedCompute returns how much compute has been saved by caching this entry.
// Each cache hit avoids one full recomputation of the prefix attention.
func (e *PrefixCacheEntry) SavedTFLOPs() float64 {
	return e.TFLOPsCost * float64(e.HitCount)
}

func (e *PrefixCacheEntry) SavedUSD() float64 {
	return e.CostUSD * float64(e.HitCount)
}

// PrefixCache is a thread-safe in-memory cache for LLM prefix KV state.
// It wraps the same cache mechanics as the product catalog cache, but the
// domain is token sequences instead of products.
type PrefixCache struct {
	mu      sync.RWMutex
	entries map[string]*PrefixCacheEntry // prefixHash → entry
	maxSize int

	// Aggregate stats
	totalRequests int64
	cacheHits     int64
	totalSavedTFLOPs float64
	totalSavedUSD    float64
}

// NewPrefixCache creates a prefix cache with the given capacity.
func NewPrefixCache(maxSize int) *PrefixCache {
	return &PrefixCache{
		entries: make(map[string]*PrefixCacheEntry, maxSize),
		maxSize: maxSize,
	}
}

// HashPrefix computes the SHA-256 hash of a token sequence.
// In a real system, tokens are integer IDs — we hash the JSON representation
// for simplicity. The hash uniquely identifies the exact prefix.
func HashPrefix(tokens []int) string {
	data, _ := json.Marshal(tokens)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16]) // 16 bytes = 32 hex chars, sufficient for uniqueness
}

// Lookup checks if a prefix is cached.
// Returns (entry, true) on hit, (nil, false) on miss.
// On hit, increments the hit counter and updates aggregate savings stats.
func (pc *PrefixCache) Lookup(_ context.Context, prefixHash string) (*PrefixCacheEntry, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.totalRequests++

	entry, ok := pc.entries[prefixHash]
	if !ok {
		return nil, false
	}

	// Cache HIT — update stats
	entry.HitCount++
	entry.LastAccessedAt = time.Now()
	pc.cacheHits++
	pc.totalSavedTFLOPs += entry.TFLOPsCost
	pc.totalSavedUSD += entry.CostUSD

	return entry, true
}

// Store caches a prefix entry. Evicts LRU entry if at capacity.
func (pc *PrefixCache) Store(_ context.Context, entry *PrefixCacheEntry) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Simple LRU eviction: remove oldest entry if at capacity
	if len(pc.entries) >= pc.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range pc.entries {
			if oldestKey == "" || e.LastAccessedAt.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.LastAccessedAt
			}
		}
		delete(pc.entries, oldestKey)
	}

	pc.entries[entry.PrefixHash] = entry
}

// Stats returns aggregate cache statistics.
func (pc *PrefixCache) Stats() CacheStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	hitRate := 0.0
	if pc.totalRequests > 0 {
		hitRate = float64(pc.cacheHits) / float64(pc.totalRequests) * 100
	}

	return CacheStats{
		TotalRequests:    pc.totalRequests,
		CacheHits:        pc.cacheHits,
		CacheMisses:      pc.totalRequests - pc.cacheHits,
		HitRate:          hitRate,
		EntriesStored:    int64(len(pc.entries)),
		TotalSavedTFLOPs: pc.totalSavedTFLOPs,
		TotalSavedUSD:    pc.totalSavedUSD,
	}
}

// CacheStats holds aggregate metrics for the prefix cache.
type CacheStats struct {
	TotalRequests    int64
	CacheHits        int64
	CacheMisses      int64
	HitRate          float64 // percentage
	EntriesStored    int64
	TotalSavedTFLOPs float64
	TotalSavedUSD    float64
}

// NewEntryForPrefix creates a PrefixCacheEntry for the given token prefix on the given model.
func NewEntryForPrefix(tokens []int, model ModelConfig) *PrefixCacheEntry {
	hash := HashPrefix(tokens)
	tflops := model.AttentionTFLOPs(len(tokens))
	costUSD := model.CostUSD(len(tokens))

	return &PrefixCacheEntry{
		PrefixHash:     hash,
		Model:          model.Name,
		TokenCount:     len(tokens),
		TFLOPsCost:     tflops,
		CostUSD:        costUSD,
		HitCount:       0,
		CachedAt:       time.Now(),
		LastAccessedAt: time.Now(),
	}
}

// FormatTFLOPs formats a TFLOP count for human-readable display.
func FormatTFLOPs(tflops float64) string {
	if tflops >= 1000 {
		return fmt.Sprintf("%.1f PFLOPs", tflops/1000)
	}
	if tflops >= 1 {
		return fmt.Sprintf("%.2f TFLOPs", tflops)
	}
	return fmt.Sprintf("%.4f TFLOPs", tflops)
}

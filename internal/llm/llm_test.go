package llm

import (
	"context"
	"math"
	"testing"
)

// ── Model FLOP math tests ─────────────────────────────────────────────────

func TestAttentionFLOPs_ScalesQuadratically(t *testing.T) {
	// Doubling sequence length should roughly 4x the FLOPs (O(n²) scaling)
	flops128 := Llama3_70B.AttentionFLOPs(128)
	flops256 := Llama3_70B.AttentionFLOPs(256)

	ratio := flops256 / flops128
	if math.Abs(ratio-4.0) > 0.01 {
		t.Errorf("expected quadratic scaling (4x), got %.2fx when doubling seq len", ratio)
	}
}

func TestAttentionFLOPs_Llama3_70B_512Tokens(t *testing.T) {
	// Manual check: 4 × 64 heads × 128 head_dim × 512² × 80 layers
	// = 4 × 64 × 128 × 262144 × 80 = 687,194,767,360 ≈ 0.687 TFLOPs
	expected := 4.0 * 64 * 128 * 512 * 512 * 80
	got := Llama3_70B.AttentionFLOPs(512)
	if math.Abs(got-expected)/expected > 0.001 {
		t.Errorf("Llama3_70B 512-token FLOPs: expected %.0f got %.0f", expected, got)
	}
}

func TestAttentionFLOPs_NeverNegative(t *testing.T) {
	for _, model := range AllModels {
		for _, seqLen := range []int{1, 16, 128, 512, 1024, 2048} {
			flops := model.AttentionFLOPs(seqLen)
			if flops <= 0 {
				t.Errorf("%s seq=%d: FLOPs must be positive, got %f", model.Name, seqLen, flops)
			}
		}
	}
}

func TestCostUSD_AlwaysPositive(t *testing.T) {
	for _, model := range AllModels {
		cost := model.CostUSD(1024)
		if cost <= 0 {
			t.Errorf("%s: cost must be positive, got %f", model.Name, cost)
		}
	}
}

func TestDeepSeekR1_HigherCostThanLlama3_8B(t *testing.T) {
	// DeepSeek-R1 has far more layers and heads — must cost more per token
	r1Cost := DeepSeekR1.CostUSD(1024)
	llamaCost := Llama3_8B.CostUSD(1024)
	if r1Cost <= llamaCost {
		t.Errorf("DeepSeek-R1 should cost more than Llama-3-8B: R1=$%f Llama=$%f", r1Cost, llamaCost)
	}
}

func TestKVCacheSizePerToken_Positive(t *testing.T) {
	for _, model := range AllModels {
		size := model.KVCacheSizePerToken()
		if size <= 0 {
			t.Errorf("%s: KV cache size must be positive, got %d", model.Name, size)
		}
	}
}

// ── Prefix cache correctness tests ────────────────────────────────────────

func TestHashPrefix_Deterministic(t *testing.T) {
	tokens := []int{1, 2, 3, 4, 5}
	h1 := HashPrefix(tokens)
	h2 := HashPrefix(tokens)
	if h1 != h2 {
		t.Error("HashPrefix must be deterministic for the same input")
	}
}

func TestHashPrefix_DifferentInputs_DifferentHashes(t *testing.T) {
	h1 := HashPrefix([]int{1, 2, 3})
	h2 := HashPrefix([]int{1, 2, 4}) // differs only in last token
	if h1 == h2 {
		t.Error("different token sequences must produce different hashes")
	}
}

func TestPrefixCache_HitAfterStore(t *testing.T) {
	cache := NewPrefixCache(100)
	ctx := context.Background()
	tokens := []int{100, 200, 300, 400}

	entry := NewEntryForPrefix(tokens, Llama3_70B)
	cache.Store(ctx, entry)

	got, hit := cache.Lookup(ctx, entry.PrefixHash)
	if !hit {
		t.Fatal("expected cache hit after storing entry")
	}
	if got.TokenCount != len(tokens) {
		t.Errorf("expected %d tokens, got %d", len(tokens), got.TokenCount)
	}
}

func TestPrefixCache_MissForUnknownKey(t *testing.T) {
	cache := NewPrefixCache(100)
	ctx := context.Background()

	_, hit := cache.Lookup(ctx, "nonexistent-hash")
	if hit {
		t.Error("expected cache miss for unknown key")
	}
}

func TestPrefixCache_HitIncrementsCounter(t *testing.T) {
	cache := NewPrefixCache(100)
	ctx := context.Background()
	tokens := []int{1, 2, 3}

	entry := NewEntryForPrefix(tokens, Llama3_8B)
	cache.Store(ctx, entry)

	cache.Lookup(ctx, entry.PrefixHash)
	cache.Lookup(ctx, entry.PrefixHash)
	cache.Lookup(ctx, entry.PrefixHash)

	got, _ := cache.Lookup(ctx, entry.PrefixHash)
	if got.HitCount != 4 {
		t.Errorf("expected HitCount=4, got %d", got.HitCount)
	}
}

func TestPrefixCache_StatsAccurate(t *testing.T) {
	cache := NewPrefixCache(100)
	ctx := context.Background()

	tokens := []int{10, 20, 30}
	entry := NewEntryForPrefix(tokens, Llama3_70B)
	cache.Store(ctx, entry)

	// 3 hits, 2 misses
	cache.Lookup(ctx, entry.PrefixHash)
	cache.Lookup(ctx, entry.PrefixHash)
	cache.Lookup(ctx, entry.PrefixHash)
	cache.Lookup(ctx, "miss-1")
	cache.Lookup(ctx, "miss-2")

	stats := cache.Stats()
	if stats.TotalRequests != 5 {
		t.Errorf("expected 5 total requests, got %d", stats.TotalRequests)
	}
	if stats.CacheHits != 3 {
		t.Errorf("expected 3 hits, got %d", stats.CacheHits)
	}
	if stats.CacheMisses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.CacheMisses)
	}
	if math.Abs(stats.HitRate-60.0) > 0.01 {
		t.Errorf("expected 60%% hit rate, got %.2f%%", stats.HitRate)
	}
}

func TestPrefixCache_SavedTFLOPsAccumulates(t *testing.T) {
	cache := NewPrefixCache(100)
	ctx := context.Background()

	tokens := make([]int, 1024) // 1024-token prefix
	for i := range tokens { tokens[i] = i }
	entry := NewEntryForPrefix(tokens, Llama3_70B)
	expectedTFLOPsPerHit := Llama3_70B.AttentionTFLOPs(1024)

	cache.Store(ctx, entry)
	cache.Lookup(ctx, entry.PrefixHash) // hit 1
	cache.Lookup(ctx, entry.PrefixHash) // hit 2
	cache.Lookup(ctx, entry.PrefixHash) // hit 3

	stats := cache.Stats()
	expectedTotal := expectedTFLOPsPerHit * 3
	if math.Abs(stats.TotalSavedTFLOPs-expectedTotal)/expectedTotal > 0.001 {
		t.Errorf("expected %.4f saved TFLOPs, got %.4f", expectedTotal, stats.TotalSavedTFLOPs)
	}
}

func TestPrefixCache_LRUEviction(t *testing.T) {
	cache := NewPrefixCache(2) // tiny capacity
	ctx := context.Background()

	e1 := NewEntryForPrefix([]int{1, 2, 3}, Llama3_8B)
	e2 := NewEntryForPrefix([]int{4, 5, 6}, Llama3_8B)
	e3 := NewEntryForPrefix([]int{7, 8, 9}, Llama3_8B)

	cache.Store(ctx, e1)
	cache.Store(ctx, e2)
	cache.Lookup(ctx, e1.PrefixHash) // access e1 to make it more recent than e2

	cache.Store(ctx, e3) // should evict e2 (least recently used)

	_, hit2 := cache.Lookup(ctx, e2.PrefixHash)
	_, hit3 := cache.Lookup(ctx, e3.PrefixHash)

	if hit2 {
		t.Error("e2 should have been evicted (LRU)")
	}
	if !hit3 {
		t.Error("e3 should still be in cache after insertion")
	}
}

// ── Scenario simulation tests ──────────────────────────────────────────────

func TestRunScenario_HitRateWithinExpectedRange(t *testing.T) {
	ctx := context.Background()
	s := DefaultScenarios()[0] // Customer Service Bot — 100% reuse rate

	result := RunScenario(ctx, s)

	// With 100% reuse rate and pre-warmed cache, all requests after first should hit
	if result.Stats.HitRate < 95.0 {
		t.Errorf("expected >95%% hit rate for 100%% reuse scenario, got %.1f%%", result.Stats.HitRate)
	}
}

func TestRunScenario_SavingsPositive(t *testing.T) {
	ctx := context.Background()
	for _, s := range DefaultScenarios()[:4] {
		result := RunScenario(ctx, s)
		if result.SavingsUSD < 0 {
			t.Errorf("scenario %q: savings must be non-negative, got $%f", s.Name, result.SavingsUSD)
		}
		if result.WithCacheCost > result.WithoutCacheCost {
			t.Errorf("scenario %q: cost with cache must be <= cost without cache", s.Name)
		}
	}
}

func TestAllModelsComparison_RunsWithoutPanic(t *testing.T) {
	ctx := context.Background()
	results := RunAllModelsComparison(ctx, 512, 100, 0.80)
	if len(results) != len(AllModels) {
		t.Errorf("expected %d results, got %d", len(AllModels), len(results))
	}
}

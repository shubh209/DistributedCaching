package benchmark

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/shared"
)

// EvictionResult holds the benchmark results for one eviction policy.
type EvictionResult struct {
	PolicyName   string
	HitCount     int64
	MissCount    int64
	EvictionCount int64
	MeanLatencyMs float64
	P99LatencyMs  float64
	Error        string // non-empty if the policy failed
}

// HitRate returns the cache hit rate as a percentage.
func (r *EvictionResult) HitRate() float64 {
	total := r.HitCount + r.MissCount
	if total == 0 {
		return 0
	}
	return float64(r.HitCount) / float64(total) * 100
}

// EvictionBenchmarkConfig controls the workload parameters.
type EvictionBenchmarkConfig struct {
	CacheCapacity  int     // maximum entries in cache
	TotalOps       int     // total Get+Set operations per policy
	ReadRatio      float64 // fraction of ops that are Gets (0.8 = 80% reads)
	KeySpaceSize   int     // number of unique keys (larger = more misses)
	ZipfExponent   float64 // skew of access pattern (1.0 = realistic, 0 = uniform)
}

// DefaultEvictionConfig returns a realistic benchmark configuration.
func DefaultEvictionConfig() EvictionBenchmarkConfig {
	return EvictionBenchmarkConfig{
		CacheCapacity: 100,
		TotalOps:      10_000,
		ReadRatio:     0.8,
		KeySpaceSize:  500, // 5x cache size — forces evictions
		ZipfExponent:  1.0, // Zipf distribution: hot keys get hit much more
	}
}

// RunEvictionBenchmark runs the same workload against all 4 eviction policies
// and returns one EvictionResult per policy.
func RunEvictionBenchmark(cfg EvictionBenchmarkConfig) []EvictionResult {
	policies := []shared.EvictionPolicy{
		newLRU(), newLFU(), newTTLOnly(), newRandom(),
	}

	results := make([]EvictionResult, 0, len(policies))
	// Pre-generate the same workload for all policies (fair comparison)
	ops := generateWorkload(cfg)

	for _, policy := range policies {
		result := runSingleEvictionBenchmark(policy, cfg, ops)
		results = append(results, result)
		fmt.Printf("  [%s] hit=%.1f%% miss=%.1f%% evictions=%d mean=%.2fms p99=%.2fms\n",
			result.PolicyName, result.HitRate(), 100-result.HitRate(),
			result.EvictionCount, result.MeanLatencyMs, result.P99LatencyMs)
	}
	return results
}

// runSingleEvictionBenchmark runs the workload against one policy and records metrics.
func runSingleEvictionBenchmark(policy shared.EvictionPolicy, cfg EvictionBenchmarkConfig, ops []benchOp) EvictionResult {
	c := cache.New(cfg.CacheCapacity, policy)
	ctx := context.Background()

	var hits, misses, evictions int64
	latencies := make([]float64, 0, len(ops))

	for _, op := range ops {
		start := time.Now()

		if op.isRead {
			_, found, _ := c.Get(ctx, op.key)
			if found {
				hits++
			} else {
				misses++
				// On miss: populate the cache (simulates cache-aside)
				c.Set(ctx, op.key, []byte("value"), 60*time.Second)
			}
		} else {
			before := c.Len()
			c.Set(ctx, op.key, []byte("value"), 60*time.Second)
			after := c.Len()
			// If Len didn't increase, an eviction must have occurred
			if after <= before && before >= cfg.CacheCapacity {
				evictions++
			}
		}

		latencies = append(latencies, float64(time.Since(start).Microseconds())/1000.0)
	}

	return EvictionResult{
		PolicyName:    policy.Name(),
		HitCount:      hits,
		MissCount:     misses,
		EvictionCount: evictions,
		MeanLatencyMs: mean(latencies),
		P99LatencyMs:  percentile(latencies, 99),
	}
}

// benchOp represents a single cache operation in the workload.
type benchOp struct {
	key    string
	isRead bool
}

// generateWorkload creates a sequence of Get/Set operations following a
// Zipf distribution — a small number of "hot" keys are accessed frequently,
// while many "cold" keys are accessed rarely. This mirrors real traffic.
func generateWorkload(cfg EvictionBenchmarkConfig) []benchOp {
	rng := rand.New(rand.NewSource(42)) // fixed seed for reproducibility
	zipf := rand.NewZipf(rng, cfg.ZipfExponent+1, 1.0, uint64(cfg.KeySpaceSize-1))

	ops := make([]benchOp, cfg.TotalOps)
	for i := range ops {
		keyIdx := zipf.Uint64()
		ops[i] = benchOp{
			key:    fmt.Sprintf("key:%d", keyIdx),
			isRead: rng.Float64() < cfg.ReadRatio,
		}
	}
	return ops
}

// mean calculates the arithmetic mean of a float64 slice.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// percentile calculates the Nth percentile of a float64 slice.
func percentile(values []float64, n float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	// Simple insertion sort (sufficient for benchmark sizes)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	idx := int(math.Ceil(n/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

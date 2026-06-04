package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/eviction"
	"github.com/user/distributed-caching-go/internal/invalidation"
	"github.com/user/distributed-caching-go/internal/shared"
)

// InvalidationResult holds the benchmark results for one invalidation strategy.
type InvalidationResult struct {
	StrategyName    string
	WriteLatencyMs  float64 // mean write latency
	ReadLatencyMs   float64 // mean read latency
	HitRate         float64 // cache hit rate after writes
	Error           string
}

// InvalidationBenchmarkConfig controls the workload for invalidation testing.
type InvalidationBenchmarkConfig struct {
	CacheCapacity int
	TotalOps      int     // total read+write operations per strategy
	WriteRatio    float64 // fraction that are writes (0.2 = 20% writes)
	KeySpaceSize  int
}

// DefaultInvalidationConfig returns a realistic invalidation benchmark config.
func DefaultInvalidationConfig() InvalidationBenchmarkConfig {
	return InvalidationBenchmarkConfig{
		CacheCapacity: 100,
		TotalOps:      5_000,
		WriteRatio:    0.2, // 80% reads, 20% writes — typical read-heavy API
		KeySpaceSize:  200,
	}
}

// RunInvalidationBenchmark runs the same workload against all 3 strategies.
func RunInvalidationBenchmark(cfg InvalidationBenchmarkConfig) []InvalidationResult {
	strategies := []shared.InvalidationStrategy{
		invalidation.NewTTL(),
		invalidation.NewWriteThrough(),
		invalidation.NewWriteBehind(),
	}

	results := make([]InvalidationResult, 0, len(strategies))
	ops := generateWorkload(EvictionBenchmarkConfig{
		TotalOps:      cfg.TotalOps,
		ReadRatio:     1 - cfg.WriteRatio,
		KeySpaceSize:  cfg.KeySpaceSize,
		ZipfExponent:  1.0,
	})

	for _, strategy := range strategies {
		result := runSingleInvalidationBenchmark(strategy, cfg, ops)
		results = append(results, result)
		fmt.Printf("  [%s] write=%.2fms read=%.2fms hit=%.1f%%\n",
			result.StrategyName, result.WriteLatencyMs, result.ReadLatencyMs, result.HitRate)
	}
	return results
}

// runSingleInvalidationBenchmark runs the workload against one strategy.
func runSingleInvalidationBenchmark(strategy shared.InvalidationStrategy, cfg InvalidationBenchmarkConfig, ops []benchOp) InvalidationResult {
	c := cache.New(cfg.CacheCapacity, eviction.NewLRU())
	ctx := context.Background()

	var readLatencies, writeLatencies []float64
	var hits, total int64

	for _, op := range ops {
		start := time.Now()

		if !op.isRead {
			// Simulate a write: update cache via strategy
			newVal := []byte(`{"updated":true}`)
			strategy.OnWrite(ctx, c, op.key, newVal)
			writeLatencies = append(writeLatencies, float64(time.Since(start).Microseconds())/1000.0)
		} else {
			// Simulate a read
			_, found, _ := c.Get(ctx, op.key)
			if found {
				hits++
			} else {
				// Miss: populate cache
				c.Set(ctx, op.key, []byte("value"), 60*time.Second)
			}
			total++
			readLatencies = append(readLatencies, float64(time.Since(start).Microseconds())/1000.0)
		}
	}

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	return InvalidationResult{
		StrategyName:   strategy.Name(),
		WriteLatencyMs: mean(writeLatencies),
		ReadLatencyMs:  mean(readLatencies),
		HitRate:        hitRate,
	}
}

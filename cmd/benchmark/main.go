package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/user/distributed-caching-go/internal/benchmark"
)

func main() {
	mode := flag.String("mode", "both", "benchmark mode: eviction | invalidation | both")
	flag.Parse()

	switch *mode {
	case "eviction":
		runEviction()
	case "invalidation":
		runInvalidation()
	case "both":
		runEviction()
		runInvalidation()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q — use eviction, invalidation, or both\n", *mode)
		os.Exit(1)
	}
}

func runEviction() {
	cfg := benchmark.DefaultEvictionConfig()
	fmt.Printf("\nEviction Benchmark: capacity=%d ops=%d keyspace=%d read_ratio=%.0f%%\n",
		cfg.CacheCapacity, cfg.TotalOps, cfg.KeySpaceSize, cfg.ReadRatio*100)

	results := benchmark.RunEvictionBenchmark(cfg)
	benchmark.PrintEvictionReport(results)
}

func runInvalidation() {
	cfg := benchmark.DefaultInvalidationConfig()
	fmt.Printf("\nInvalidation Benchmark: capacity=%d ops=%d keyspace=%d write_ratio=%.0f%%\n",
		cfg.CacheCapacity, cfg.TotalOps, cfg.KeySpaceSize, cfg.WriteRatio*100)

	results := benchmark.RunInvalidationBenchmark(cfg)
	benchmark.PrintInvalidationReport(results)
}

package benchmark

import "fmt"

// PrintEvictionReport prints a formatted comparison table for eviction policies.
func PrintEvictionReport(results []EvictionResult) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║              EVICTION POLICY BENCHMARK RESULTS                      ║")
	fmt.Println("╠════════════╦══════════╦══════════╦═══════════╦══════════╦══════════╣")
	fmt.Println("║ Policy     ║ Hit Rate ║Miss Rate ║ Evictions ║ Mean(ms) ║  P99(ms) ║")
	fmt.Println("╠════════════╬══════════╬══════════╬═══════════╬══════════╬══════════╣")
	for _, r := range results {
		if r.Error != "" {
			fmt.Printf("║ %-10s ║ ERROR: %-55s ║\n", r.PolicyName, r.Error)
			continue
		}
		fmt.Printf("║ %-10s ║  %5.1f%%  ║  %5.1f%%  ║ %9d ║ %8.3f ║ %8.3f ║\n",
			r.PolicyName,
			r.HitRate(),
			100-r.HitRate(),
			r.EvictionCount,
			r.MeanLatencyMs,
			r.P99LatencyMs,
		)
	}
	fmt.Println("╚════════════╩══════════╩══════════╩═══════════╩══════════╩══════════╝")
}

// PrintInvalidationReport prints a formatted comparison table for invalidation strategies.
func PrintInvalidationReport(results []InvalidationResult) {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           INVALIDATION STRATEGY BENCHMARK RESULTS           ║")
	fmt.Println("╠════════════════╦══════════════╦═════════════╦═══════════════╣")
	fmt.Println("║ Strategy       ║ Write(ms)    ║ Read(ms)    ║ Hit Rate      ║")
	fmt.Println("╠════════════════╬══════════════╬═════════════╬═══════════════╣")
	for _, r := range results {
		if r.Error != "" {
			fmt.Printf("║ %-14s ║ ERROR: %-43s ║\n", r.StrategyName, r.Error)
			continue
		}
		fmt.Printf("║ %-14s ║ %12.3f ║ %11.3f ║ %12.1f%% ║\n",
			r.StrategyName,
			r.WriteLatencyMs,
			r.ReadLatencyMs,
			r.HitRate,
		)
	}
	fmt.Println("╚════════════════╩══════════════╩═════════════╩═══════════════╝")
}

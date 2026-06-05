package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/user/distributed-caching-go/internal/llm"
)

func main() {
	mode := flag.String("mode", "all", "simulation mode: all | scenarios | models | cost")
	flag.Parse()

	ctx := context.Background()

	fmt.Println("╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          LLM Prefix Cache Simulation — Compute Cost Savings         ║")
	fmt.Println("║     Open-source models: Llama-3, Mistral, Mixtral, Qwen, DeepSeek   ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════╝")

	switch *mode {
	case "scenarios":
		runScenarios(ctx)
	case "models":
		runModelComparison(ctx)
	case "cost":
		runCostAnalysis()
	default:
		runScenarios(ctx)
		fmt.Println()
		runModelComparison(ctx)
		fmt.Println()
		runCostAnalysis()
	}

	fmt.Println("\n╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║  ✅ Simulation complete — metrics ready for resume & portfolio       ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════╝")
}

func runScenarios(ctx context.Context) {
	fmt.Println("\n▶ REAL-WORLD SCENARIO SIMULATIONS")
	fmt.Println("  Each scenario simulates a different business use case for prefix caching.")

	scenarios := llm.DefaultScenarios()[:4] // skip the multi-model one for main scenarios
	for _, s := range scenarios {
		result := llm.RunScenario(ctx, s)
		llm.PrintResults(result)
	}
}

func runModelComparison(ctx context.Context) {
	fmt.Println("\n▶ OPEN-SOURCE MODEL COMPARISON (1024-token prefix, 1000 requests, 90% reuse)")
	fmt.Println("  Same workload across all 6 models — shows cost difference at different scales.")

	results := llm.RunAllModelsComparison(ctx, 1024, 1000, 0.90)
	llm.PrintModelComparison(results)
}

func runCostAnalysis() {
	fmt.Println("\n▶ COST ANALYSIS — Attention FLOPs vs Prefix Length")
	fmt.Println("  Demonstrates the quadratic scaling of attention with sequence length.")
	fmt.Printf("\n  %-20s  %10s  %12s  %12s  %14s\n",
		"Model", "128 tokens", "512 tokens", "1024 tokens", "2048 tokens")
	fmt.Println("  " + strings.Repeat("─", 72))

	for _, model := range llm.AllModels {
		fmt.Printf("  %-20s  %10s  %12s  %12s  %14s\n",
			model.Name,
			llm.FormatTFLOPs(model.AttentionTFLOPs(128)),
			llm.FormatTFLOPs(model.AttentionTFLOPs(512)),
			llm.FormatTFLOPs(model.AttentionTFLOPs(1024)),
			llm.FormatTFLOPs(model.AttentionTFLOPs(2048)),
		)
	}

	fmt.Println("\n  H100 pricing: ~$2.50/hr, ~312 TFLOPS")
	fmt.Println("\n  Dollar cost per prefix computation (Llama-3-70B):")
	model := llm.Llama3_70B
	for _, seqLen := range []int{128, 512, 1024, 2048, 4096} {
		cost := model.CostUSD(seqLen)
		savings1000 := cost * 900 // 90% hit rate on 1000 requests
		fmt.Printf("    %5d tokens → $%.6f/req  →  $%.4f saved per 1000 reqs (90%% hit rate)\n",
			seqLen, cost, savings1000)
	}
}

package llm

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Scenario describes a real-world use case for LLM prefix caching.
type Scenario struct {
	Name            string
	Description     string
	Company         string       // type of company this applies to
	SystemPromptLen int          // tokens in the shared system prompt / RAG context
	UserMessageLen  int          // tokens in the varying user message
	NumRequests     int          // total requests to simulate
	PrefixReuseRate float64      // fraction of requests that share the same prefix (0–1)
	Model           ModelConfig
}

// ScenarioResult holds the simulation output for one scenario.
type ScenarioResult struct {
	Scenario        Scenario
	Stats           CacheStats
	WithoutCacheCost  float64 // total USD if every request recomputed the prefix
	WithCacheCost     float64 // total USD with prefix caching
	SavingsUSD        float64 // dollar savings
	SavingsPct        float64 // percentage savings
	Duration          time.Duration
}

// DefaultScenarios returns the pre-configured simulation scenarios.
func DefaultScenarios() []Scenario {
	return []Scenario{
		{
			Name:            "Customer Service Bot",
			Description:     "500 users share the same 512-token system prompt. User messages vary.",
			Company:         "SaaS companies (Salesforce, HubSpot, Zendesk)",
			SystemPromptLen: 512,
			UserMessageLen:  64,
			NumRequests:     500,
			PrefixReuseRate: 1.0, // all requests share the same system prompt
			Model:           Llama3_70B,
		},
		{
			Name:            "Code Assistant (RAG)",
			Description:     "200 developers, same 1024-token codebase context prepended to each query.",
			Company:         "AI coding tools (Cursor, GitHub Copilot, Sourcegraph)",
			SystemPromptLen: 1024,
			UserMessageLen:  128,
			NumRequests:     1000,
			PrefixReuseRate: 0.85, // 85% of requests hit the same file context
			Model:           Llama3_70B,
		},
		{
			Name:            "RAG Pipeline (Document QA)",
			Description:     "1000 queries over the same 2048-token retrieved document. Maximum prefix reuse.",
			Company:         "Enterprise AI (Glean, Notion AI, Perplexity Enterprise)",
			SystemPromptLen: 2048,
			UserMessageLen:  96,
			NumRequests:     1000,
			PrefixReuseRate: 0.92, // 92% share the same retrieved document
			Model:           Llama3_70B,
		},
		{
			Name:            "Legal Document Analysis",
			Description:     "500 queries over the same 3000-token legal context. Fixed domain knowledge prefix.",
			Company:         "Vertical AI (Harvey, Lexion, ContractPodAi)",
			SystemPromptLen: 3000,
			UserMessageLen:  128,
			NumRequests:     500,
			PrefixReuseRate: 0.99, // almost all requests use the same legal guidelines
			Model:           Qwen2_5_72B,
		},
		{
			Name:            "Open Source Model Comparison",
			Description:     "1000 requests across all 6 open-source models with 1024-token shared prefix.",
			Company:         "LLM API providers (Groq, Together.ai, Fireworks.ai)",
			SystemPromptLen: 1024,
			UserMessageLen:  64,
			NumRequests:     1000,
			PrefixReuseRate: 0.90,
			Model:           Llama3_8B, // shown per-model in results
		},
	}
}

// RunScenario simulates a scenario and returns the results.
func RunScenario(ctx context.Context, s Scenario) ScenarioResult {
	cache := NewPrefixCache(10000)
	rng := rand.New(rand.NewSource(42))

	// Build the shared system prompt tokens (simulated as sequential int IDs)
	sharedPrefix := make([]int, s.SystemPromptLen)
	for i := range sharedPrefix {
		sharedPrefix[i] = i + 1000 // offset to distinguish from user tokens
	}

	// Pre-populate cache with the shared prefix (simulates warm-up)
	entry := NewEntryForPrefix(sharedPrefix, s.Model)
	cache.Store(ctx, entry)

	start := time.Now()

	// Simulate requests
	for i := 0; i < s.NumRequests; i++ {
		// Decide whether this request shares the common prefix
		usesSharedPrefix := rng.Float64() < s.PrefixReuseRate

		var prefixTokens []int
		if usesSharedPrefix {
			prefixTokens = sharedPrefix
		} else {
			// Unique prefix — different context
			uniquePrefix := make([]int, s.SystemPromptLen)
			for j := range uniquePrefix {
				uniquePrefix[j] = rng.Intn(100000) + 200000
			}
			prefixTokens = uniquePrefix
		}

		hash := HashPrefix(prefixTokens)
		_, hit := cache.Lookup(ctx, hash)
		if !hit {
			// Cache miss — store this prefix for future requests
			newEntry := NewEntryForPrefix(prefixTokens, s.Model)
			cache.Store(ctx, newEntry)
		}
	}

	duration := time.Since(start)
	stats := cache.Stats()

	// Cost without caching: every request pays full prefill cost
	prefixCostPerRequest := s.Model.CostUSD(s.SystemPromptLen)
	withoutCacheCost := prefixCostPerRequest * float64(s.NumRequests)

	// Cost with caching: only misses pay the prefill cost
	withCacheCost := prefixCostPerRequest * float64(stats.CacheMisses)
	savingsUSD := withoutCacheCost - withCacheCost
	savingsPct := 0.0
	if withoutCacheCost > 0 {
		savingsPct = savingsUSD / withoutCacheCost * 100
	}

	return ScenarioResult{
		Scenario:        s,
		Stats:           stats,
		WithoutCacheCost:  withoutCacheCost,
		WithCacheCost:     withCacheCost,
		SavingsUSD:        savingsUSD,
		SavingsPct:        savingsPct,
		Duration:          duration,
	}
}

// RunAllModelsComparison runs the same scenario across all 6 open-source models.
func RunAllModelsComparison(ctx context.Context, prefixLen, numRequests int, reuseRate float64) []ScenarioResult {
	results := make([]ScenarioResult, 0, len(AllModels))
	for _, model := range AllModels {
		s := Scenario{
			Name:            model.Name,
			SystemPromptLen: prefixLen,
			NumRequests:     numRequests,
			PrefixReuseRate: reuseRate,
			Model:           model,
		}
		results = append(results, RunScenario(ctx, s))
	}
	return results
}

// PrintResults prints a formatted results table.
func PrintResults(result ScenarioResult) {
	sep := strings.Repeat("─", 70)
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("  Scenario:   %s\n", result.Scenario.Name)
	fmt.Printf("  Company:    %s\n", result.Scenario.Company)
	fmt.Printf("  Model:      %s (%.0fB params)\n", result.Scenario.Model.Name, result.Scenario.Model.ParamsBillion)
	fmt.Printf("  Prefix:     %d tokens | Requests: %d | Reuse rate: %.0f%%\n",
		result.Scenario.SystemPromptLen, result.Scenario.NumRequests,
		result.Scenario.PrefixReuseRate*100)
	fmt.Printf("%s\n", sep)
	fmt.Printf("  Cache hit rate:      %.1f%% (%d/%d requests)\n",
		result.Stats.HitRate, result.Stats.CacheHits, result.Stats.TotalRequests)
	fmt.Printf("  Prefix cost/req:     $%.6f (%.4f TFLOPs)\n",
		result.Scenario.Model.CostUSD(result.Scenario.SystemPromptLen),
		result.Scenario.Model.AttentionTFLOPs(result.Scenario.SystemPromptLen))
	fmt.Printf("  Cost WITHOUT cache:  $%.4f\n", result.WithoutCacheCost)
	fmt.Printf("  Cost WITH cache:     $%.4f\n", result.WithCacheCost)
	fmt.Printf("  ✦ Compute saved:     %s\n", FormatTFLOPs(result.Stats.TotalSavedTFLOPs))
	fmt.Printf("  ✦ Cost saved:        $%.4f (%.1f%% reduction)\n", result.SavingsUSD, result.SavingsPct)
	fmt.Printf("  Simulation time:     %v\n", result.Duration)
}

// PrintModelComparison prints a comparison table across all models.
func PrintModelComparison(results []ScenarioResult) {
	fmt.Printf("\n%-20s %8s %12s %12s %10s\n",
		"Model", "Hit Rate", "Cost/Req", "Saved Total", "Reduction")
	fmt.Println(strings.Repeat("─", 70))
	for _, r := range results {
		fmt.Printf("%-20s %7.1f%% $%10.6f %11s %9.1f%%\n",
			r.Scenario.Model.Name,
			r.Stats.HitRate,
			r.Scenario.Model.CostUSD(r.Scenario.SystemPromptLen),
			FormatTFLOPs(r.Stats.TotalSavedTFLOPs),
			r.SavingsPct,
		)
	}
}

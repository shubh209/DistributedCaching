package llm

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// Document represents a single retrievable document in the knowledge base.
// In a real RAG system this would hold text + an embedding vector. Here we
// only need the token count to compute the attention cost of prepending it.
type Document struct {
	ID         string // e.g. "doc-001"
	TokenCount int    // size of the document in tokens
}

// DocumentStore is the simulated knowledge base a RAG pipeline retrieves from.
// Retrieval is ID-based (not semantic) — see ADR note in the resume doc.
type DocumentStore struct {
	docs []Document
}

// NewDocumentStore creates a store of numDocs documents, each of tokensPerDoc tokens.
func NewDocumentStore(numDocs, tokensPerDoc int) *DocumentStore {
	docs := make([]Document, numDocs)
	for i := range docs {
		docs[i] = Document{
			ID:         fmt.Sprintf("doc-%03d", i+1),
			TokenCount: tokensPerDoc,
		}
	}
	return &DocumentStore{docs: docs}
}

// Retrieve returns the top-K documents for a query, selected by index.
// This is deterministic ID-based retrieval: the same query indices always
// return the same document set, which is what makes prefix caching effective.
func (s *DocumentStore) Retrieve(docIndices []int) []Document {
	retrieved := make([]Document, 0, len(docIndices))
	for _, idx := range docIndices {
		if idx >= 0 && idx < len(s.docs) {
			retrieved = append(retrieved, s.docs[idx])
		}
	}
	return retrieved
}

// RAGConfig controls the RAG simulation workload.
type RAGConfig struct {
	Model          ModelConfig
	NumDocuments   int     // size of the knowledge base
	TokensPerDoc   int     // tokens per document
	TopK           int     // documents retrieved per query
	QueryTokens    int     // tokens in the user's question (appended after docs)
	NumRequests    int     // total queries to simulate
	ZipfExponent   float64 // skew of document popularity (higher = more skew)
	RequestsPerDay int     // for monthly cost projection
}

// DefaultRAGConfig returns a realistic RAG workload:
// an enterprise document-QA assistant over a 200-document knowledge base.
func DefaultRAGConfig() RAGConfig {
	return RAGConfig{
		Model:          Llama3_70B,
		NumDocuments:   200,
		TokensPerDoc:   512,
		TopK:           3,
		QueryTokens:    64,
		NumRequests:    5000,
		ZipfExponent:   1.2, // a few documents dominate retrieval (FAQ, top manuals)
		RequestsPerDay: 1_000_000,
	}
}

// RAGResult holds the outcome of a RAG simulation.
type RAGResult struct {
	Config           RAGConfig
	Stats            CacheStats
	PrefixTokens     int     // tokens in a retrieved-context prefix (TopK docs)
	CostPerMissUSD   float64 // cost to compute one uncached retrieved prefix
	WithoutCacheUSD  float64 // total cost if every query recomputes its prefix
	WithCacheUSD     float64 // total cost with prefix caching
	SavingsUSD       float64
	SavingsPct       float64
	MonthlyWithout   float64 // projected monthly cost without caching
	MonthlyWith      float64 // projected monthly cost with caching
	MonthlySavings   float64
	Duration         time.Duration
}

// RunRAGSimulation simulates a RAG pipeline where each query retrieves TopK
// documents, prepends them as a prefix, and checks the prefix cache.
//
// Pipeline per query (LangChain style retrieve-then-read):
//  1. Pick TopK documents using a Zipf distribution (hot docs retrieved often)
//  2. Build the prefix = concatenated retrieved documents
//  3. Hash the prefix and look it up in the cache
//  4. On miss, the prefix attention must be computed (and cached) — this is the cost
//  5. On hit, the cost is avoided
func RunRAGSimulation(ctx context.Context, cfg RAGConfig) RAGResult {
	store := NewDocumentStore(cfg.NumDocuments, cfg.TokensPerDoc)
	cache := NewPrefixCache(100000)
	rng := rand.New(rand.NewSource(42))
	zipf := rand.NewZipf(rng, cfg.ZipfExponent+1, 1.0, uint64(cfg.NumDocuments-1))

	// The prefix is TopK documents plus the user query tokens.
	prefixTokens := cfg.TopK*cfg.TokensPerDoc + cfg.QueryTokens

	start := time.Now()

	for i := 0; i < cfg.NumRequests; i++ {
		// Step 1: retrieve TopK document indices via Zipf (hot docs recur)
		docIndices := make([]int, cfg.TopK)
		for k := range docIndices {
			docIndices[k] = int(zipf.Uint64())
		}
		// Sort so {3,1,2} and {1,2,3} are treated as the same retrieved set —
		// retrieval order does not change which documents are in context.
		sort.Ints(docIndices)

		// Step 2: build the prefix token sequence from the retrieved doc IDs.
		// We encode the doc set into a token sequence so identical retrievals
		// produce an identical hash (a cache hit).
		retrieved := store.Retrieve(docIndices)
		prefix := buildPrefixTokens(retrieved, cfg.QueryTokens, docIndices)

		// Step 3+4: look up the prefix; on miss, compute and cache it.
		hash := HashPrefix(prefix)
		if _, hit := cache.Lookup(ctx, hash); !hit {
			entry := newEntryForTokenCount(hash, prefixTokens, cfg.Model)
			cache.Store(ctx, entry)
		}
	}

	duration := time.Since(start)
	stats := cache.Stats()

	// Cost math: every query without caching pays the full prefix prefill cost.
	costPerMiss := cfg.Model.CostUSD(prefixTokens)
	withoutCache := costPerMiss * float64(cfg.NumRequests)
	withCache := costPerMiss * float64(stats.CacheMisses)
	savings := withoutCache - withCache
	savingsPct := 0.0
	if withoutCache > 0 {
		savingsPct = savings / withoutCache * 100
	}

	// Monthly projection: scale the per-request average to RequestsPerDay × 30.
	perReqWithout := withoutCache / float64(cfg.NumRequests)
	perReqWith := withCache / float64(cfg.NumRequests)
	monthlyRequests := float64(cfg.RequestsPerDay) * 30
	monthlyWithout := perReqWithout * monthlyRequests
	monthlyWith := perReqWith * monthlyRequests

	return RAGResult{
		Config:          cfg,
		Stats:           stats,
		PrefixTokens:    prefixTokens,
		CostPerMissUSD:  costPerMiss,
		WithoutCacheUSD: withoutCache,
		WithCacheUSD:    withCache,
		SavingsUSD:      savings,
		SavingsPct:      savingsPct,
		MonthlyWithout:  monthlyWithout,
		MonthlyWith:     monthlyWith,
		MonthlySavings:  monthlyWithout - monthlyWith,
		Duration:        duration,
	}
}

// buildPrefixTokens encodes a retrieved document set into a token-ID sequence.
// The same set of document indices always produces the same sequence, so
// identical retrievals hash to the same cache key (exact-set matching).
//
// The query tokens are appended as a fixed block — in this simulation the
// user question is treated as part of the cacheable prefix because the cost
// story is about the retrieved-context prefill, which dominates.
func buildPrefixTokens(docs []Document, queryTokens int, docIndices []int) []int {
	tokens := make([]int, 0, len(docs)*8+queryTokens)
	// Encode each document as a repeated marker derived from its index so the
	// byte content is deterministic per document set.
	for _, idx := range docIndices {
		// 8 marker tokens per doc is enough to make the hash unique per set
		for j := 0; j < 8; j++ {
			tokens = append(tokens, 500000+idx)
		}
	}
	// Append a fixed query block (same for all — the variable part is the docs)
	for j := 0; j < queryTokens; j++ {
		tokens = append(tokens, 900000+j)
	}
	return tokens
}

// newEntryForTokenCount builds a cache entry where the cost reflects the full
// prefix token count, but the hash is the pre-computed retrieved-set hash.
func newEntryForTokenCount(hash string, tokenCount int, model ModelConfig) *PrefixCacheEntry {
	return &PrefixCacheEntry{
		PrefixHash:     hash,
		Model:          model.Name,
		TokenCount:     tokenCount,
		TFLOPsCost:     model.AttentionTFLOPs(tokenCount),
		CostUSD:        model.CostUSD(tokenCount),
		HitCount:       0,
		CachedAt:       time.Now(),
		LastAccessedAt: time.Now(),
	}
}

// PrintRAGResult prints a stakeholder-facing report: per-run totals plus a
// monthly cost projection — the number a budget owner actually acts on.
func PrintRAGResult(r RAGResult) {
	sep := strings.Repeat("─", 70)
	fmt.Printf("\n%s\n", sep)
	fmt.Println("  RAG PIPELINE COST SIMULATION")
	fmt.Printf("%s\n", sep)
	fmt.Printf("  Knowledge base:   %d documents × %d tokens\n", r.Config.NumDocuments, r.Config.TokensPerDoc)
	fmt.Printf("  Retrieval:        top-%d documents per query (Zipf popularity)\n", r.Config.TopK)
	fmt.Printf("  Prefix size:      %d tokens (%d docs + %d query)\n",
		r.PrefixTokens, r.Config.TopK, r.Config.QueryTokens)
	fmt.Printf("  Model:            %s (%.0fB params)\n", r.Config.Model.Name, r.Config.Model.ParamsBillion)
	fmt.Printf("  Requests:         %d simulated\n", r.Config.NumRequests)
	fmt.Printf("%s\n", sep)
	fmt.Printf("  Cache hit rate:   %.1f%% (%d hits / %d requests)\n",
		r.Stats.HitRate, r.Stats.CacheHits, r.Stats.TotalRequests)
	fmt.Printf("  Compute saved:    %s\n", FormatTFLOPs(r.Stats.TotalSavedTFLOPs))
	fmt.Printf("  Cost per uncached prefix: $%.6f\n", r.CostPerMissUSD)
	fmt.Printf("%s\n", sep)
	fmt.Println("  PER-RUN TOTALS")
	fmt.Printf("    Without caching:  $%.4f\n", r.WithoutCacheUSD)
	fmt.Printf("    With caching:     $%.4f\n", r.WithCacheUSD)
	fmt.Printf("    ✦ Saved:          $%.4f (%.1f%% reduction)\n", r.SavingsUSD, r.SavingsPct)
	fmt.Printf("%s\n", sep)
	fmt.Printf("  MONTHLY PROJECTION (at %s requests/day)\n", formatInt(r.Config.RequestsPerDay))
	fmt.Printf("    Without caching:  $%s / month\n", formatMoney(r.MonthlyWithout))
	fmt.Printf("    With caching:     $%s / month\n", formatMoney(r.MonthlyWith))
	fmt.Printf("    ✦ Saved:          $%s / month\n", formatMoney(r.MonthlySavings))
	fmt.Printf("%s\n", sep)
	fmt.Printf("  Simulation time:  %v\n", r.Duration.Round(time.Millisecond))
}

// formatInt adds thousands separators to an integer (e.g. 1000000 → 1,000,000).
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	return out
}

// formatMoney formats a dollar amount with thousands separators and 2 decimals.
func formatMoney(v float64) string {
	whole := int64(v)
	frac := int64((v - float64(whole)) * 100)
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%s.%02d", formatInt(int(whole)), frac)
}

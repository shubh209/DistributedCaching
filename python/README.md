# llmsim — LLM Prefix-KV-Cache & RAG Pipeline Cost Simulator

A Python CLI that analytically computes transformer attention FLOP costs and simulates an in-memory LRU prefix cache to quantify the compute and dollar savings of prefix caching for LLM inference and RAG pipelines.

No real models are run. Every number comes from closed-form FLOP arithmetic and a simulated cache.

## Installation

```bash
pip install -e ".[dev]"
```

## Usage

```bash
# Default: five pre-built business scenarios
python -m llmsim

# Compare cost across all six supported models
python -m llmsim --mode=all-models

# RAG pipeline simulation with Zipf document popularity and monthly cost projection
python -m llmsim --mode=rag
```

## Sample output: RAG mode

```
──────────────────────────────────────────────────────────────────────
  RAG PIPELINE COST SIMULATION
──────────────────────────────────────────────────────────────────────
  Knowledge base:   200 documents × 512 tokens
  Retrieval:        top-3 documents per query (Zipf popularity)
  Prefix size:      1600 tokens (3 docs + 64 query)
  Model:            Llama-3-70B (70B params)
  Requests:         5000 simulated
──────────────────────────────────────────────────────────────────────
  Cache hit rate:   93.9% (4697 hits / 5000 requests)
  Compute saved:    31.5 PFLOPs
  Cost per uncached prefix: $0.000015
──────────────────────────────────────────────────────────────────────
  PER-RUN TOTALS
    Without caching:  $0.0747
    With caching:     $0.0045
    ✦ Saved:          $0.0702 (93.9% reduction)
──────────────────────────────────────────────────────────────────────
  MONTHLY PROJECTION (at 1,000,000 requests/day)
    Without caching:  $448.11 / month
    With caching:     $27.16 / month
    ✦ Saved:          $420.95 / month
──────────────────────────────────────────────────────────────────────
```

## Supported models

| Model | Layers | Q heads | KV heads | Params |
|---|---|---|---|---|
| Llama-3-8B | 32 | 32 | 8 | 8B |
| Llama-3-70B | 80 | 64 | 8 | 70B |
| Mistral-7B | 32 | 32 | 8 | 7B |
| Mixtral-8x7B | 32 | 32 | 8 | 46.7B |
| Qwen2.5-72B | 80 | 64 | 8 | 72B |
| DeepSeek-R1-671B | 61 | 128 | 128 | 671B |

Architecture parameters sourced from Hugging Face config.json files.

## FLOP math

Attention prefill cost: `num_layers × (4 × num_q_heads × head_dim × seq_len²)`

Cost basis: H100 SXM5 at 312 TFLOPS fp16, $2.50/hr.

Doubling prompt length quadruples prefill cost — which is why prefix caching ROI grows with model size and prompt length.

## Tests

```bash
pytest tests/ -v
```

206 tests, all passing. Covers FLOP math correctness, LRU eviction, exact-set prefix matching, savings arithmetic, monthly projection, and CLI dispatch. Property-based tests use Hypothesis.

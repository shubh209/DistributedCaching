# Implementation Plan: python-llm-rag-simulation

## Overview

Implement a Python CLI module (`python/src/llmsim/`) that ports the Go LLM prefix-KV-cache and
RAG-pipeline cost simulation from `internal/llm/`. The module uses analytic FLOP math, an
in-memory LRU prefix cache, and NumPy for vectorized and Zipf computations. It exposes three
simulation modes via `python -m llmsim --mode=...`. Implementation follows the layer order:
project scaffold → models → prefix_cache → scenarios → rag → report → cli → tests.

Language: **Python 3.11+** (as specified throughout the design document).

---

## Tasks

- [x] 1. Scaffold the Python package structure and project configuration
  - Create `python/pyproject.toml` declaring package metadata, entry point
    `llmsim = "llmsim.__main__:main"`, and dependencies pinned to exact
    versions: `numpy==2.2.6`, `pytest==8.3.5`, `hypothesis==6.131.15`
  - Create `python/README.md` with a brief description and usage instructions
    (`python -m llmsim --mode=...`)
  - Create the directory tree `python/src/llmsim/` with empty stub files:
    `__init__.py`, `__main__.py`, `models.py`, `prefix_cache.py`,
    `scenarios.py`, `rag.py`, `report.py`, `cli.py`
  - Create `python/tests/` with empty stub test files: `test_models.py`,
    `test_prefix_cache.py`, `test_scenarios.py`, `test_rag.py`, `test_cli.py`
  - `__init__.py` shall re-export the public API surface:
    `ModelConfig`, `ALL_MODELS`, `get_model`, `PrefixCache`, `CacheEntry`,
    `CacheStats`, `hash_prefix`, `run_scenario`, `run_all_models_comparison`,
    `run_rag_simulation`, `default_rag_config`, `RAGConfig`
  - `__main__.py` shall contain only `from llmsim.cli import main; import sys; sys.exit(main())`
  - _Requirements: 12.1, 12.6_

- [x] 2. Implement the Model Catalog (`models.py`)
  - [x] 2.1 Define `ModelConfig` dataclass, module-level model constants, `ALL_MODELS` tuple, and error types
    - Implement `@dataclass(frozen=True) class ModelConfig` with fields:
      `name: str`, `num_layers: int`, `num_kv_heads: int`, `num_q_heads: int`,
      `head_dim: int`, `max_context_len: int`, `params_billion: float`
    - Define module constants `MAX_SEQ_LEN = 1_048_576`,
      `H100_TFLOPS_PER_SEC = 312.0`, `H100_HOURLY_COST_USD = 2.50`
    - Define `class UnknownModelError(ValueError)` and `class SeqLenRangeError(ValueError)`
    - Define all six frozen `ModelConfig` constants with values exactly matching
      Requirements 1.2–1.7 and the Go reference (`models.go`):
      `LLAMA3_8B`, `LLAMA3_70B`, `MISTRAL_7B`, `MIXTRAL_8X7B`, `QWEN2_5_72B`,
      `DEEPSEEK_R1`
    - Define `ALL_MODELS: tuple[ModelConfig, ...]` in the order specified in
      Requirement 1.8 (8B → 70B → Mistral → Mixtral → Qwen → DeepSeek)
    - _Requirements: 1.1–1.8_

  - [x] 2.2 Implement `validate_seq_len`, FLOP/cost methods, and KV-cache-size method on `ModelConfig`
    - Implement `validate_seq_len(seq_len: int) -> None` — rejects booleans
      (`isinstance(seq_len, bool)`), non-integers, negatives, and values
      `> MAX_SEQ_LEN`; raises `SeqLenRangeError` (Req 2.7); `0` is valid
    - Implement `ModelConfig.attention_flops(seq_len)`:
      `num_layers * (4 * num_q_heads * head_dim * seq_len**2)` — calls
      `validate_seq_len` first; returns `0.0` when `seq_len == 0` (Req 2.6)
    - Implement `ModelConfig.attention_tflops(seq_len)`:
      `attention_flops(seq_len) / 1e12`
    - Implement `ModelConfig.cost_usd(seq_len)`:
      `attention_tflops(seq_len) / 312.0 / 3600.0 * 2.50`
    - Implement `ModelConfig.kv_cache_size_per_token()`:
      `2 * num_kv_heads * head_dim * 2 * num_layers`
    - _Requirements: 2.1–2.4, 2.6, 2.7_

  - [x] 2.3 Implement `get_model` lookup and NumPy vectorized helpers
    - Implement `get_model(name: str) -> ModelConfig` — searches `ALL_MODELS`
      by name, raises `UnknownModelError` listing valid names on a miss
    - Implement `attention_tflops_vec(models, seq_len) -> np.ndarray`:
      builds NumPy arrays from model fields and computes TFLOPs in one
      vectorized expression; calls `validate_seq_len` first
    - Implement `cost_usd_vec(models, seq_len) -> np.ndarray` using the same
      vectorized chain: `flops = layers * (4.0 * q_heads * head_dim * seq_len**2)`,
      `tflops = flops / 1e12`, `usd = tflops / 312.0 / 3600.0 * 2.50`
    - _Requirements: 1.9, 1.10, 2.5, 7.3_

  - [x]* 2.4 Write property tests for `ModelConfig` math and validation (Properties 1, 2, 3, 4)
    - **Property 1: Analytic attention and cost math match the reference formulas**
    - Use `@given(st.sampled_from(ALL_MODELS), st.integers(0, 1_048_576))`
    - Assert `attention_flops == num_layers * (4 * num_q_heads * head_dim * seq_len**2)`,
      `attention_tflops == flops / 1e12`, `cost_usd == tflops / 312 / 3600 * 2.50`,
      `kv_cache_size_per_token == 2 * num_kv_heads * head_dim * 2 * num_layers`
    - At `seq_len=0` assert FLOPs, TFLOPs, USD are all `0.0`
    - **Property 2: NumPy vectorized cost equals the scalar cost within tolerance**
    - Assert `np.allclose(cost_usd_vec(ALL_MODELS, seq_len), [m.cost_usd(seq_len) for m in ALL_MODELS], rtol=1e-9)`
    - **Property 3: Sequence-length validation rejects out-of-range and non-integer inputs**
    - Assert `SeqLenRangeError` raised for negative, `> 1_048_576`, float, bool
    - **Property 4: Unknown model and scenario lookups raise errors**
    - Assert `UnknownModelError` raised for any string not in `ALL_MODELS`
    - **Validates: Requirements 1.10, 2.1–2.7, 14.1**
    - _Tag: `Feature: python-llm-rag-simulation, Property 1: Analytic attention and cost math match the reference formulas`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 2: NumPy vectorized cost equals the scalar cost within tolerance`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 3: Sequence-length validation rejects out-of-range and non-integer inputs`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 4: Unknown model and scenario lookups raise errors`_

  - [x]* 2.5 Write unit/example tests for `models.py` (fixed catalog values, boundary cases)
    - Assert each of the six `ModelConfig` constants has the exact field values
      from Requirements 1.2–1.7 (names, layers, kv_heads, q_heads, head_dim,
      params_billion)
    - Assert `ALL_MODELS` has length 6 and its order matches Requirement 1.8
    - Assert `attention_flops(Llama3_8B, 1024)` equals the Go reference value
      within `rtol=1e-9`
    - Assert `kv_cache_size_per_token` for each model against expected bytes
    - Assert `attention_flops(any_model, 0) == 0.0`,
      `attention_tflops(any_model, 0) == 0.0`,
      `cost_usd(any_model, 0) == 0.0`
    - _Requirements: 14.1_

- [x] 3. Implement the Prefix Cache (`prefix_cache.py`)
  - [x] 3.1 Implement `hash_prefix` and `CacheEntry` dataclass
    - Implement `hash_prefix(tokens: Sequence[int]) -> str`:
      serialize to compact JSON (`json.dumps(tokens, separators=(",", ":"))`,
      matching Go's `json.Marshal` layout), take `sha256(...).digest()`,
      hex-encode the first 16 bytes → exactly 32 lowercase hex characters
    - Handle empty list without error (hashes `[]`, Req 3.4)
    - Implement `@dataclass class CacheEntry` with fields:
      `prefix_hash: str`, `model: str`, `token_count: int`,
      `tflops_cost: float`, `cost_usd: float`, `hit_count: int = 0`
    - Implement `CacheEntry.saved_tflops() -> float` and `CacheEntry.saved_usd() -> float`:
      `tflops_cost * hit_count` and `cost_usd * hit_count` respectively; both `0.0`
      when `hit_count == 0`
    - _Requirements: 3.1–3.4, 5.5_

  - [x] 3.2 Implement `CacheStats`, `PrefixCache.__init__`, `lookup`, and `store`
    - Implement `@dataclass(frozen=True) class CacheStats` with fields:
      `total_requests: int`, `cache_hits: int`, `cache_misses: int`,
      `hit_rate: float`, `entries_stored: int`, `total_saved_tflops: float`,
      `total_saved_usd: float`
    - Implement `PrefixCache.__init__(capacity: int)`: uses
      `collections.OrderedDict[str, CacheEntry]`; raises `ValueError`
      when `capacity < 1`
    - Implement `PrefixCache.lookup(prefix_hash: str) -> CacheEntry | None`:
      increments `total_requests` always; on hit: `move_to_end`, increment
      `hit_count`, `cache_hits`, accumulate saved totals, return entry; on
      miss: return `None` (Req 4.1–4.4)
    - Implement `PrefixCache.store(entry: CacheEntry) -> None`: on existing
      hash: replace + `move_to_end`, leave entries count unchanged (Req 4.9);
      on new hash at capacity: `popitem(last=False)` to evict LRU (Req 4.6);
      insert new entry at end as MRU (Req 4.5, 4.7)
    - _Requirements: 4.1–4.9, 5.1–5.4_

  - [x] 3.3 Implement `PrefixCache.stats` and helper `new_entry_for_prefix`
    - Implement `PrefixCache.stats() -> CacheStats`:
      `hit_rate = cache_hits / total_requests * 100` when `total_requests > 0`
      else `0.0`; `cache_misses = total_requests - cache_hits`;
      `entries_stored = len(self._entries)` (Req 5.1–5.4)
    - Implement `new_entry_for_prefix(tokens: Sequence[int], model: ModelConfig) -> CacheEntry`:
      compute hash, tflops, cost_usd from `model.attention_tflops(len(tokens))`
      and `model.cost_usd(len(tokens))`
    - _Requirements: 5.1–5.5_

  - [x]* 3.4 Write property tests for hashing (Properties 5, 6)
    - **Property 5: Prefix hash is a stable 32-character lowercase hex string**
    - Use `@given(st.lists(st.integers()))` — includes empty list
    - Assert result matches `re.fullmatch(r'[0-9a-f]{32}', h)`
    - Assert identical token list called twice produces the same hash
    - **Property 6: Prefix hashing distinguishes different sequences**
    - Use `@given` with two distinct token lists that differ in length, value,
      or order
    - Assert hashes differ
    - **Validates: Requirements 3.1–3.4**
    - _Tag: `Feature: python-llm-rag-simulation, Property 5: Prefix hash is a stable 32-character lowercase hex string`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 6: Prefix hashing distinguishes different sequences`_

  - [x]* 3.5 Write property tests for cache hit/miss/store/eviction/accounting (Properties 7–12)
    - **Property 7: Cache hit returns the entry, marks it MRU, and accumulates savings**
    - **Property 8: Cache miss returns nothing and leaves the cache unchanged**
    - **Property 9: Store inserts or replaces, keeping at most one entry per hash**
    - **Property 10: Storing at full capacity evicts exactly the LRU entry**
    - **Property 11: Cache accounting is consistent**
    - **Property 12: Entry saved totals scale with hit count**
    - Use composite Hypothesis strategies for access patterns and cache capacity
    - Assert `cache_hits + cache_misses == total_requests` for all sequences
    - Assert `hit_rate == cache_hits / total_requests * 100` when `total_requests > 0`
    - Assert at most one entry per distinct hash at all times
    - **Validates: Requirements 4.1–4.9, 5.1–5.5**
    - _Tag: `Feature: python-llm-rag-simulation, Property 7: Cache hit returns the entry, marks it most-recently-used, and accumulates savings`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 8: Cache miss returns nothing and leaves the cache unchanged`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 9: Store inserts or replaces, keeping at most one entry per hash`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 10: Storing at full capacity evicts exactly the least-recently-used entry`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 11: Cache accounting is consistent`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 12: Entry saved totals scale with hit count`_

  - [x]* 3.6 Write unit/example tests for `prefix_cache.py` (fixed behaviors, boundary cases)
    - Assert cache hit returns the matching `CacheEntry` and records a hit
    - Assert cache miss returns `None` and records a miss
    - Assert LRU eviction removes the least-recently-used entry when store at capacity
    - Assert `PrefixCache(0)` raises `ValueError`
    - Assert `hash_prefix([])` returns a 32-char lowercase hex string (no error)
    - Assert `hash_prefix([1, 2, 3]) == hash_prefix([1, 2, 3])` across calls
    - Assert `hash_prefix([1, 2]) != hash_prefix([2, 1])`
    - _Requirements: 14.2_

- [x] 4. Checkpoint — Ensure all tests pass through `prefix_cache.py`
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Implement the Scenario Simulator (`scenarios.py`)
  - [x] 5.1 Define `Scenario`, `ScenarioResult` dataclasses and `default_scenarios`
    - Implement `@dataclass(frozen=True) class Scenario` with fields:
      `name`, `description`, `company`, `system_prompt_len`, `user_message_len`,
      `num_requests`, `prefix_reuse_rate`, `model`
    - Implement `@dataclass(frozen=True) class ScenarioResult` with fields:
      `scenario`, `stats`, `without_cache_cost`, `with_cache_cost`,
      `savings_usd`, `savings_pct`
    - Implement `class UnknownScenarioError(ValueError)` and
      `class ComparisonParamError(ValueError)`
    - Implement `default_scenarios() -> list[Scenario]` returning exactly the
      five scenarios with names, models, and parameter values matching the Go
      reference (`simulator.go`): Customer Service Bot, Code Assistant (RAG),
      RAG Pipeline (Document QA), Legal Document Analysis,
      Open Source Model Comparison
    - Implement `get_scenario(name: str) -> Scenario` — raises
      `UnknownScenarioError` for unknown names; runs no simulation
    - _Requirements: 6.1, 6.2, 6.12_

  - [x] 5.2 Implement `run_scenario`
    - Build shared prefix tokens: `[1000 + i for i in range(system_prompt_len)]`
    - Pre-populate the cache via `cache.store(new_entry_for_prefix(shared_prefix, model))` (Req 6.3)
    - Use `numpy.random.default_rng(seed)` for the per-request Bernoulli draw
    - Per request: `rng.random() < prefix_reuse_rate` → shared prefix; else
      draw a unique prefix from `rng.integers(200000, 300000, size=system_prompt_len)`
      (mirrors Go `rng.Intn(100000) + 200000`)
    - On miss: `cache.store(new_entry_for_prefix(...))` (Req 6.5)
    - After all requests: compute cost math from `stats.cache_misses`:
      `prefix_cost = model.cost_usd(system_prompt_len)`,
      `without_cache = prefix_cost * num_requests`,
      `with_cache = prefix_cost * stats.cache_misses`,
      `savings_usd = without_cache - with_cache`,
      `savings_pct = savings_usd / without_cache * 100` when `without_cache > 0` else `0.0`
    - Return `ScenarioResult`
    - _Requirements: 6.3–6.11_

  - [x] 5.3 Implement `run_all_models_comparison`
    - Validate inputs first: `prefix_len >= 1`, `num_requests >= 1`,
      `0.0 <= reuse_rate <= 1.0`; raise `ComparisonParamError` naming the
      offending parameter and run nothing on violation (Req 7.4)
    - Compute `cost_usd_vec(ALL_MODELS, prefix_len)` with NumPy and assert
      element-wise equality to per-model `cost_usd(prefix_len)` within
      `np.allclose(..., rtol=1e-9)` (Req 7.3)
    - Run `run_scenario` for each model in `ALL_MODELS` order, building a
      one-per-model `Scenario` with the shared `prefix_len`, `num_requests`,
      `reuse_rate` (Req 7.1)
    - Return the results list in `ALL_MODELS` catalog order (Req 7.2)
    - _Requirements: 7.1–7.5_

  - [x]* 5.4 Write property tests for scenarios (Properties 4, 13, 14, 15, 16)
    - **Property 13: Savings arithmetic relationships hold for both simulators**
    - Use `@given` with `st.floats(min_value=0.0)` for cost, `st.integers(min_value=0)` for counts
    - Assert `without == cost * requests`, `with == cost * misses`,
      `savings == without - with`, `pct == savings / without * 100` when `without > 0`
    - Assert `savings_pct == 0.0` when `without_cache == 0` (no division)
    - **Property 14: Seeded simulations are deterministic**
    - Run each of the 5 scenarios twice with `seed=42`, assert results are identical
    - **Property 15: All-models comparison produces one ordered, well-formed result per model**
    - Assert length == 6, ordered as `ALL_MODELS`, all `hit_rate in [0, 100]`,
      `savings_pct in [0, 100]`
    - **Property 16: Comparison parameter validation rejects invalid inputs**
    - Use `@given` with out-of-range `prefix_len`, `num_requests`, `reuse_rate`
    - Assert `ComparisonParamError` raised naming the bad field; no results produced
    - **Validates: Requirements 6.6–6.11, 7.1–7.5**
    - _Tag: `Feature: python-llm-rag-simulation, Property 13: Savings arithmetic relationships hold for both simulators`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 14: Seeded simulations are deterministic`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 15: All-models comparison produces one ordered, well-formed result per model`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 16: Comparison parameter validation rejects invalid inputs`_

  - [x]* 5.5 Write unit/example tests for `scenarios.py`
    - Assert `default_scenarios()` returns exactly 5 `Scenario` objects
    - Assert each scenario has the name, model, and numeric parameters from
      the Go reference (e.g. Customer Service Bot: 512 tokens, 500 requests,
      1.0 reuse rate, Llama-3-70B)
    - Assert `get_scenario("Customer Service Bot")` returns the expected `Scenario`
    - Assert `get_scenario("Unknown")` raises `UnknownScenarioError`
    - Run one scenario with `seed=42` and assert `savings_pct > 0.0`
    - Assert `run_all_models_comparison(prefix_len=0, ...)` raises `ComparisonParamError`
    - _Requirements: 6.1, 6.2, 6.9, 6.10, 6.12_

- [x] 6. Implement the RAG Simulator and Document Store (`rag.py`)
  - [x] 6.1 Implement `Document`, `DocumentStore` and `RAGConfig`/`RAGConfigError`/`RAGResult`
    - Implement `@dataclass(frozen=True) class Document` with fields `id: str`, `token_count: int`
    - Implement `class DocumentStoreParamError(ValueError)`
    - Implement `class DocumentStore`:
      - `__init__(num_docs, tokens_per_doc)`: validates both `>= 1`, raises
        `DocumentStoreParamError` naming the bad field; creates `num_docs`
        `Document` objects with `id = f"doc-{i+1:03d}"` and given token count
      - `num_docs` property returning the document count
      - `retrieve(doc_indices) -> list[Document]`: silently skips out-of-range
        indices; returns empty list if no valid indices
    - Implement `@dataclass(frozen=True) class RAGConfig` with all fields from
      the design and a `validate()` method raising `RAGConfigError` naming the
      first invalid field (including `top_k > num_documents`)
    - Implement `class RAGConfigError(ValueError)`
    - Implement `@dataclass(frozen=True) class RAGResult` with all fields from
      the design (`stats`, `prefix_tokens`, `cost_per_miss_usd`,
      `without_cache_usd`, `with_cache_usd`, `savings_usd`, `savings_pct`,
      `per_req_without_usd`, `per_req_with_usd`, `monthly_requests`,
      `monthly_without`, `monthly_with`, `monthly_savings`)
    - _Requirements: 8.1–8.6, 10.1, 10.2_

  - [x] 6.2 Implement `build_prefix_tokens` and exact-set matching
    - Implement `build_prefix_tokens(doc_indices: Sequence[int], query_tokens: int) -> list[int]`:
      - De-duplicate and sort ascending first (`sorted(set(doc_indices))`) (Req 9.1)
      - For each index in sorted order, append 8 marker tokens: `[500000 + idx] * 8`
      - Append query block: `[900000 + j for j in range(query_tokens)]`
      - With no indices, return only the query block (Req 9.5)
    - Ensure two calls with the same unique index set in any order produce
      identical token lists and therefore identical hashes (Req 9.2)
    - Ensure two calls with different unique index sets produce different token lists (Req 9.3)
    - Implement `new_entry_for_token_count(prefix_hash, token_count, model) -> CacheEntry`:
      constructs a `CacheEntry` using `model.attention_tflops(token_count)` and
      `model.cost_usd(token_count)` as cost fields
    - _Requirements: 9.1–9.5_

  - [x] 6.3 Implement `run_rag_simulation` with Zipf sampling and rejection
    - Call `cfg.validate()` first; raise `RAGConfigError` on failure (Req 10.2)
    - Build `DocumentStore(cfg.num_documents, cfg.tokens_per_doc)`
    - Build `PrefixCache(100_000)` (Req 10.5)
    - Compute `prefix_tokens = cfg.top_k * cfg.tokens_per_doc + cfg.query_tokens` (Req 10.4)
    - Use `numpy.random.default_rng(seed)` for all random draws
    - Per request: sample `top_k` indices using rejection sampling:
      draw batches via `rng.zipf(cfg.zipf_exponent + 1, size=batch)`, convert to
      0-based (`value - 1`), discard samples `>= cfg.num_documents`, repeat until
      `top_k` valid samples collected (Req 10.11)
    - Call `build_prefix_tokens(sorted(set(indices)), cfg.query_tokens)`,
      hash, lookup; on miss: store `new_entry_for_token_count(...)` (Req 10.5, 10.6)
    - After simulation: compute cost and projection math:
      - `cost_per_miss = model.cost_usd(prefix_tokens)`
      - `without_cache = cost_per_miss * num_requests` (Req 10.7)
      - `with_cache = cost_per_miss * stats.cache_misses`
      - `savings_usd = without_cache - with_cache`
      - `savings_pct = savings_usd / without_cache * 100` when `without_cache > 0` else `0.0` (Req 10.8, 10.9)
      - `per_req_without = without_cache / num_requests` (Req 11.1)
      - `per_req_with = with_cache / num_requests`
      - `monthly_requests = cfg.requests_per_day * 30` (Req 11.3)
      - `monthly_without = per_req_without * monthly_requests` (Req 11.4)
      - `monthly_with = per_req_with * monthly_requests` (Req 11.5)
      - `monthly_savings = monthly_without - monthly_with` (Req 11.6)
    - _Requirements: 10.3–10.11, 11.1–11.6_

  - [x] 6.4 Implement `default_rag_config`
    - Return `RAGConfig` matching Go reference (`rag.go`): `Llama3_70B`, 200 docs,
      512 tokens/doc, top-k=3, 64 query tokens, 5000 requests,
      zipf_exponent=1.2, 1_000_000 requests_per_day
    - _Requirements: 10.1_

  - [x]* 6.5 Write property tests for document store and exact-set matching (Properties 17, 18, 19, 20, 21, 22, 23)
    - **Property 17: Document store construction and retrieval are well-behaved**
    - Use `@given(st.integers(1, 1000), st.integers(1, 1000))` for construction;
      `st.lists(st.integers())` for retrieval indices
    - Assert store holds exactly `num_docs` documents, each with correct `token_count`
    - Assert valid indices return documents, out-of-range indices are silently excluded,
      empty-range returns empty list
    - **Property 18: Document store rejects invalid construction parameters**
    - Assert `DocumentStoreParamError` raised for `num_docs < 1` or `tokens_per_doc < 1`
    - **Property 19: Exact-set prefix matching is order- and duplicate-independent**
    - Use `@given(st.lists(st.integers(0, 50), min_size=1), st.integers(0, 100))`
    - Shuffle and add duplicates; assert `build_prefix_tokens` produces same result
    - **Property 20: Different unique index sets produce different prefix hashes**
    - Assert two different unique index sets produce different `build_prefix_tokens` output
    - **Property 21: RAG configuration validation rejects invalid fields**
    - Parametrize over each field out-of-range; assert `RAGConfigError` naming field
    - **Property 22: RAG prefix size and cache accounting are consistent**
    - Assert `prefix_tokens == top_k * tokens_per_doc + query_tokens`
    - Assert `cache_hits + cache_misses == num_requests` after simulation
    - **Property 23: Zipf-selected document indices stay within range**
    - Run `default_rag_config()` simulation; assert all retrieved doc indices in
      `[0, num_documents - 1]` (instrument via a patched `DocumentStore.retrieve`)
    - **Validates: Requirements 8.1–8.6, 9.1–9.5, 10.2, 10.4–10.6, 10.11**
    - _Tag: `Feature: python-llm-rag-simulation, Property 17: Document store construction and retrieval are well-behaved`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 18: Document store rejects invalid construction parameters`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 19: Exact-set prefix matching is order- and duplicate-independent`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 20: Different unique index sets produce different prefix hashes`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 21: RAG configuration validation rejects invalid fields`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 22: RAG prefix size and cache accounting are consistent`_
    - _Tag: `Feature: python-llm-rag-simulation, Property 23: Zipf-selected document indices stay within range`_

  - [x]* 6.6 Write property tests for savings arithmetic and monthly projection (Properties 13, 24)
    - **Property 24: Monthly projection arithmetic holds**
    - Use `@given` composite strategy for valid `RAGConfig`
    - Assert `monthly_requests == requests_per_day * 30`
    - Assert `monthly_without == per_req_without * monthly_requests`
    - Assert `monthly_with == per_req_with * monthly_requests`
    - Assert `monthly_savings == monthly_without - monthly_with`
    - Assert `per_req_without == without_cache / num_requests` when `num_requests > 0`
    - **Validates: Requirements 11.1–11.6**
    - _Tag: `Feature: python-llm-rag-simulation, Property 24: Monthly projection arithmetic holds`_

  - [x]* 6.7 Write unit/example tests for `rag.py`
    - Assert `DocumentStore(200, 512)` holds exactly 200 documents each with `token_count=512`
    - Assert `DocumentStore(0, 512)` raises `DocumentStoreParamError`
    - Assert retrieve with `[]` returns `[]`
    - Assert retrieve with all-out-of-range indices returns `[]`
    - Assert `build_prefix_tokens([], 0)` returns `[]`
    - Assert `build_prefix_tokens([3, 1, 2], 64) == build_prefix_tokens([1, 2, 3], 64)`
      (order-independent)
    - Assert `build_prefix_tokens([1, 1, 2], 64) == build_prefix_tokens([1, 2], 64)`
      (duplicate-independent)
    - Run `default_rag_config()` simulation and assert `savings_pct > 90.0`
      (≈92.3% headline result)
    - Assert savings math with zero cost: `savings_pct == 0.0`
    - Assert monthly projection: `monthly_requests == requests_per_day * 30`
    - _Requirements: 8.1–8.6, 9.1–9.5, 10.7–11.6, 14.3, 14.4_

- [x] 7. Checkpoint — Ensure all tests pass through `rag.py`
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Implement the Report Formatter (`report.py`)
  - [x] 8.1 Implement all scalar formatting functions
    - Implement `format_money(value: float) -> str`:
      `f"{value:,.2f}"` — comma thousands separator, 2 decimal places
    - Implement `format_int(value: int) -> str`:
      `f"{value:,d}"` — comma thousands separator, no decimals
    - Implement `format_percent(value: float) -> str`:
      `f"{value:.1f}"` — one decimal place
    - Implement `format_tflops(value: float) -> str` mirroring Go tiers:
      `>= 1000` → `f"{value/1000:.1f} PFLOPs"`;
      `>= 1` → `f"{value:.2f} TFLOPs"`;
      `< 1` → `f"{value:.4f} TFLOPs"`
    - _Requirements: 13.1–13.6_

  - [x] 8.2 Implement report renderers for each simulation mode
    - Implement `render_scenario_result(result: ScenarioResult) -> str`:
      include scenario name, company, model, prefix tokens, request count,
      reuse rate, cache hit rate, compute saved, cost per prefix,
      without-cache cost, with-cache cost, savings USD, savings percentage
    - Implement `render_model_comparison(results: list[ScenarioResult]) -> str`:
      one row per model with hit rate, per-prefix cost, total saved, and
      reduction percentage (Req 13.8)
    - Implement `render_rag_result(result: RAGResult) -> str`:
      include cache hit rate, compute saved, cost per uncached prefix,
      per-run totals (without cache, with cache, saved USD, saved percentage),
      and monthly projection (without cache, with cache, saved per month)
      (Req 13.7)
    - All renderers return strings (not print directly) so they are testable
    - _Requirements: 13.7, 13.8_

  - [x] 8.3 Write unit/example tests for `report.py`
    - Assert `format_money(1234567.89) == "1,234,567.89"`
    - Assert `format_money(0.01) == "0.01"`
    - Assert `format_int(1000000) == "1,000,000"`
    - Assert `format_percent(92.3) == "92.3"`
    - Assert `format_tflops(0.5)` ends with "TFLOPs" and uses 4 decimals
    - Assert `format_tflops(1.0)` ends with "TFLOPs" and uses 2 decimals
    - Assert `format_tflops(999.99)` ends with "TFLOPs" and uses 2 decimals
    - Assert `format_tflops(1000.0)` ends with "PFLOPs" and uses 1 decimal
    - Assert `render_rag_result(...)` string contains required fields:
      cache hit rate, compute saved, cost per uncached prefix, "Without",
      "With", monthly projection (Req 13.7)
    - Assert `render_model_comparison(...)` has exactly 6 model rows, each
      with hit rate, cost, saved, reduction percentage (Req 13.8)
    - _Requirements: 13.1–13.8_

- [x] 9. Implement the CLI (`cli.py`)
  - [x] 9.1 Implement argument parsing and mode dispatch in `cli.py`
    - Define `VALID_MODES = {"prefix-cache", "all-models", "rag"}` (Req 12.2)
    - Define `DEFAULT_MODE = "prefix-cache"` (Req 12.3)
    - Implement `build_parser() -> argparse.ArgumentParser`:
      adds `--mode` argument with default `"prefix-cache"`
    - Implement `main(argv: list[str] | None = None) -> int`:
      - Parse `--mode` from `argv`
      - If mode not in `VALID_MODES`: print error listing all three valid modes,
        return non-zero exit code (Req 12.4)
      - `"prefix-cache"`: run all five `default_scenarios()` via `run_scenario`,
        collect `ScenarioResult` objects, render each with `render_scenario_result`,
        print to stdout, return `0`
      - `"all-models"`: run `run_all_models_comparison` with default parameters
        matching Go reference (prefix_len=1024, num_requests=1000, reuse_rate=0.90),
        render with `render_model_comparison`, print, return `0`
      - `"rag"`: run `run_rag_simulation(default_rag_config())`, render with
        `render_rag_result`, print, return `0`
      - No sockets or network listeners opened (Req 12.6)
    - _Requirements: 12.1–12.6_

  - [x] 9.2 Write unit/example tests for `cli.py`
    - Assert `main([])` returns `0` and prints output containing scenario names
      (default mode is `prefix-cache`, Req 12.3)
    - Assert `main(["--mode=prefix-cache"])` returns `0`
    - Assert `main(["--mode=all-models"])` returns `0` and output contains 6 model rows
    - Assert `main(["--mode=rag"])` returns `0` and output contains monthly projection
    - Assert `main(["--mode=invalid"])` returns non-zero exit code and stderr/stdout
      contains a message listing the three valid modes (Req 12.4)
    - Use `capsys` or `capfd` pytest fixture to capture stdout
    - _Requirements: 12.2–12.5_

- [x] 10. Final Checkpoint — Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.
  - Run `python -m pytest python/tests/ -v` from the repository root (from the `python/`
    subdirectory with the package installed in editable mode: `pip install -e .`)
  - Verify the headline result: `python -m llmsim --mode=rag` produces a savings
    percentage in the range 85%–99% (≈92.3% for the default config)
  - Verify `python -m llmsim` (default mode) completes with exit code 0
  - Verify `python -m llmsim --mode=all-models` completes with exit code 0

---

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP.
- Property tests use Hypothesis with `max_examples=100` minimum.
- Each property test carries a tag comment:
  `Feature: python-llm-rag-simulation, Property {N}: {property_text}`.
- Seeded RNG (seed `42`) ensures determinism; all simulator entry points accept
  a `seed` keyword argument defaulting to `42`.
- The `src/` layout requires `pip install -e python/` (editable install) before
  running `python -m llmsim` or `pytest`.
- Go's `math/rand.NewZipf` and NumPy's `Generator.zipf` are not byte-identical
  even under the same seed; the ≈92.3% RAG result is reproduced approximately.
- All monetary values use Python f-string format specs (`f"{v:,.2f}"`) — no
  third-party formatting libraries.
- Each task is independently executable: model constants can be tested before the
  cache is written; the cache can be tested before the simulators are written.

---

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["2.2"] },
    { "id": 3, "tasks": ["2.3"] },
    { "id": 4, "tasks": ["2.4", "2.5", "3.1"] },
    { "id": 5, "tasks": ["3.2"] },
    { "id": 6, "tasks": ["3.3"] },
    { "id": 7, "tasks": ["3.4", "3.5", "3.6"] },
    { "id": 8, "tasks": ["5.1"] },
    { "id": 9, "tasks": ["5.2"] },
    { "id": 10, "tasks": ["5.3"] },
    { "id": 11, "tasks": ["5.4", "5.5", "6.1"] },
    { "id": 12, "tasks": ["6.2"] },
    { "id": 13, "tasks": ["6.3"] },
    { "id": 14, "tasks": ["6.4"] },
    { "id": 15, "tasks": ["6.5", "6.6", "6.7", "8.1"] },
    { "id": 16, "tasks": ["8.2"] },
    { "id": 17, "tasks": ["8.3", "9.1"] },
    { "id": 18, "tasks": ["9.2"] }
  ]
}
```

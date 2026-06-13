# Requirements Document

## Introduction

This feature is a Python port of an existing Go LLM prefix-KV-cache and RAG-pipeline
cost simulation. It is a CLI-only learning/resume project that lives in a `python/`
subdirectory of the existing distributed-caching repository. The module is a
**simulation**: it does not run real models or perform real inference. Instead it
computes transformer attention FLOP costs analytically and simulates an in-memory
prefix cache to quantify the compute and dollar savings that prefix/KV caching
produces for LLM inference and RAG pipelines.

The Python module ports and matches the numerical behavior of the existing Go module
in `internal/llm/` (`models.go`, `prefix_cache.go`, `simulator.go`, `rag.go`) while
using NumPy where natural (FLOP arrays, vectorized cost computation across models,
Zipf sampling) to legitimately demonstrate Python and NumPy proficiency.

The deliverable produces stakeholder-facing cost numbers — per-run totals and a
monthly projection — that help non-technical decision makers reason about the budget
impact of AI features. The headline reference result is approximately a 92.3% compute
reduction on a 1,000-request RAG pipeline.

## Glossary

- **Simulation_System**: The complete Python CLI application that runs cost simulations.
- **Model_Catalog**: The component holding the six supported model configurations and their FLOP/cost math.
- **Model_Config**: A single transformer architecture specification (layers, KV heads, Q heads, head dim, parameter count).
- **Prefix_Cache**: The in-memory cache that stores simulated prefix KV state keyed by a hash of the token sequence, with LRU eviction.
- **Cache_Entry**: A single cached prefix record holding its hash, model name, token count, TFLOPs cost, USD cost, and hit count.
- **Cache_Stats**: Aggregate metrics (total requests, hits, misses, hit rate, entries stored, total TFLOPs saved, total USD saved).
- **Scenario_Simulator**: The component that runs pre-built prefix-cache business scenarios and the all-models comparison.
- **RAG_Simulator**: The component that runs the retrieve-then-read RAG pipeline cost simulation.
- **Document_Store**: The simulated knowledge base of documents that the RAG_Simulator retrieves from by ID.
- **CLI**: The command-line entry point that selects and runs simulation modes and prints reports.
- **Report_Formatter**: The component that renders human-readable reports (tables, thousands separators, TFLOPs/PFLOPs formatting).
- **Prefix**: A token sequence that precedes the variable portion of an LLM request; the cacheable unit.
- **Prefix_Reuse_Rate**: The fraction of requests in a scenario that share the same prefix (0.0–1.0).
- **Hit_Rate**: The percentage of total requests served from the Prefix_Cache.
- **TFLOPs**: Tera floating-point operations (1e12 FLOPs).
- **PFLOPs**: Peta floating-point operations (1e15 FLOPs), equal to 1000 TFLOPs.
- **H100**: NVIDIA H100 GPU, used as the cost basis at 312 TFLOPS fp16 and $2.50/hour.
- **Zipf_Distribution**: A skewed popularity distribution used to model hot documents being retrieved more often.

## Requirements

### Requirement 1: Model Catalog

**User Story:** As an AI engineer, I want a catalog of real open-source model architectures, so that the simulation reflects production transformer configurations.

#### Acceptance Criteria

1. THE Model_Catalog SHALL provide exactly six Model_Config entries, each identified by a unique name: Llama-3-8B, Llama-3-70B, Mistral-7B, Mixtral-8x7B, Qwen2.5-72B, and DeepSeek-R1-671B.
2. THE Model_Catalog SHALL define Llama-3-8B with 32 layers, 8 KV heads, 32 Q heads, head dimension 128, and 8.0 billion parameters.
3. THE Model_Catalog SHALL define Llama-3-70B with 80 layers, 8 KV heads, 64 Q heads, head dimension 128, and 70.0 billion parameters.
4. THE Model_Catalog SHALL define Mistral-7B with 32 layers, 8 KV heads, 32 Q heads, head dimension 128, and 7.0 billion parameters.
5. THE Model_Catalog SHALL define Mixtral-8x7B with 32 layers, 8 KV heads, 32 Q heads, head dimension 128, and 46.7 billion parameters.
6. THE Model_Catalog SHALL define Qwen2.5-72B with 80 layers, 8 KV heads, 64 Q heads, head dimension 128, and 72.0 billion parameters.
7. THE Model_Catalog SHALL define DeepSeek-R1-671B with 61 layers, 128 KV heads, 128 Q heads, head dimension 128, and 671.0 billion parameters.
8. THE Model_Catalog SHALL expose an ordered collection containing all six Model_Config entries in the order listed in acceptance criterion 1.
9. WHEN a Model_Config is requested by a name matching one of the six entries, THE Model_Catalog SHALL return the matching Model_Config.
10. IF a Model_Config is requested by a name that does not match any of the six entries, THEN THE Model_Catalog SHALL return no Model_Config and report an error indicating that the requested name is unknown.

### Requirement 2: Attention FLOP and Cost Math

**User Story:** As an AI engineer, I want analytic attention FLOP and dollar cost calculations, so that I can quantify inference compute economics without running real models.

#### Acceptance Criteria

1. WHEN given a sequence length that is an integer in the range 0 to 1,048,576 inclusive, THE Model_Catalog SHALL compute prefill attention FLOPs as num_layers × (4 × num_q_heads × head_dim × seq_len²).
2. WHEN given a sequence length in the range 0 to 1,048,576 inclusive, THE Model_Catalog SHALL compute attention TFLOPs as the attention FLOPs divided by 1e12.
3. WHEN given a sequence length in the range 0 to 1,048,576 inclusive, THE Model_Catalog SHALL compute H100 dollar cost as attention_TFLOPs ÷ 312 ÷ 3600 × 2.50.
4. THE Model_Catalog SHALL compute KV cache size per token in bytes as 2 × num_kv_heads × head_dim × 2 × num_layers.
5. WHERE NumPy is used for cost computation across multiple models, THE Model_Catalog SHALL produce values numerically equal to the scalar formulas in acceptance criteria 1 through 4 within a relative tolerance of 1e-9.
6. WHEN a sequence length of zero is provided, THE Model_Catalog SHALL return zero attention FLOPs, zero attention TFLOPs, and zero H100 dollar cost.
7. IF a sequence length that is negative, non-integer, or greater than 1,048,576 is provided, THEN THE Model_Catalog SHALL reject the input, return an error indicating the sequence length is out of the accepted range, and compute no FLOP, TFLOP, or dollar cost values.

### Requirement 3: Prefix Hashing

**User Story:** As an AI engineer, I want a deterministic hash of a token sequence, so that identical prefixes map to the same cache key.

#### Acceptance Criteria

1. WHEN given a token sequence of zero or more integer token identifiers, THE Prefix_Cache SHALL compute a SHA-256 based hash represented as a string of exactly 32 lowercase hexadecimal characters (drawn from 0-9 and a-f).
2. WHEN two token sequences contain identical token values in identical order, THE Prefix_Cache SHALL produce identical hash strings, and THE Prefix_Cache SHALL produce that same hash string for the identical token sequence across separate invocations and separate process runs.
3. IF two token sequences differ in length, in any token value, or in token ordering, THEN THE Prefix_Cache SHALL produce different hash strings.
4. WHEN given an empty token sequence, THE Prefix_Cache SHALL return a hash string of exactly 32 lowercase hexadecimal characters without raising an error.

### Requirement 4: Prefix Cache Lookup, Store, and Eviction

**User Story:** As an AI engineer, I want an in-memory prefix cache with LRU eviction, so that the simulation models which prefix recomputations are avoided.

#### Acceptance Criteria

1. WHEN a lookup is performed for a prefix hash present in the Prefix_Cache, THE Prefix_Cache SHALL return the matching Cache_Entry, record a cache hit, and mark that Cache_Entry as the most recently accessed entry.
2. WHEN a lookup is performed for a prefix hash absent from the Prefix_Cache, THE Prefix_Cache SHALL report a cache miss and return no entry, leaving the set of stored Cache_Entry records and their recency ordering unchanged.
3. WHEN a lookup results in a cache hit, THE Prefix_Cache SHALL increment the Cache_Entry hit count by one, add the entry TFLOPs cost to total TFLOPs saved, and add the entry USD cost to total USD saved.
4. WHEN any lookup is performed, THE Prefix_Cache SHALL increment the total request count by one.
5. WHEN a Cache_Entry is stored for a prefix hash not already present and the Prefix_Cache contains fewer entries than its capacity, THE Prefix_Cache SHALL add the Cache_Entry, mark it as the most recently accessed entry, and evict no existing Cache_Entry.
6. WHEN a Cache_Entry is stored for a prefix hash not already present and the Prefix_Cache holds a number of entries equal to its capacity, THE Prefix_Cache SHALL evict exactly one Cache_Entry — the least recently accessed entry, where recency is determined by the most recent lookup or store of each entry — before adding the new Cache_Entry and marking it as the most recently accessed entry.
7. WHILE the Prefix_Cache holds entries, THE Prefix_Cache SHALL key each Cache_Entry by its prefix hash, storing at most one Cache_Entry per distinct prefix hash.
8. THE Prefix_Cache SHALL be configured with a capacity expressed as an integer greater than or equal to 1.
9. WHEN a Cache_Entry is stored for a prefix hash already present in the Prefix_Cache, THE Prefix_Cache SHALL replace the existing Cache_Entry, mark it as the most recently accessed entry, and leave the entries-stored count unchanged.

### Requirement 5: Cache Statistics

**User Story:** As a stakeholder, I want aggregate cache statistics, so that I can see how effective prefix caching is.

#### Acceptance Criteria

1. WHEN Cache_Stats is requested, THE Prefix_Cache SHALL report Cache_Stats containing total requests, cache hits, cache misses, hit rate, entries stored, total TFLOPs saved, and total USD saved, where entries stored equals the current count of Cache_Entry records held in the Prefix_Cache after any eviction.
2. WHEN total requests is greater than zero, THE Prefix_Cache SHALL compute hit rate as (cache_hits ÷ total_requests) × 100, expressed as a percentage value between 0 and 100 inclusive.
3. IF total requests is zero, THEN THE Prefix_Cache SHALL report a hit rate of zero, a total TFLOPs saved of zero, and a total USD saved of zero.
4. THE Prefix_Cache SHALL report cache misses as total requests minus cache hits, such that cache hits plus cache misses equals total requests.
5. THE Cache_Entry SHALL report saved TFLOPs as its TFLOPs cost multiplied by its hit count and saved USD as its USD cost multiplied by its hit count, reporting zero for both saved TFLOPs and saved USD when its hit count is zero.

### Requirement 6: Prefix-Cache Business Scenarios

**User Story:** As a stakeholder, I want pre-built business scenarios, so that I can see prefix-cache savings for recognizable real-world use cases.

#### Acceptance Criteria

1. THE Scenario_Simulator SHALL provide exactly five pre-built scenarios identified as Customer Service Bot, Code Assistant RAG, RAG Pipeline Document QA, Legal Document Analysis, and Open-Source Model Comparison.
2. THE Scenario_Simulator SHALL define each scenario with a shared system-prompt or context length that is an integer from 1 to 1,000,000 tokens, a user message length that is an integer from 1 to 1,000,000 tokens, a request count that is an integer from 1 to 10,000,000, a Prefix_Reuse_Rate from 0.0 to 1.0 inclusive, and exactly one assigned Model_Config.
3. WHEN a scenario is run, THE Scenario_Simulator SHALL pre-populate the Prefix_Cache with the shared prefix before simulating requests.
4. WHEN simulating each request, THE Scenario_Simulator SHALL classify the request as reusing the shared prefix with probability equal to the Prefix_Reuse_Rate, and otherwise assign a unique prefix.
5. WHEN a simulated request produces a cache miss, THE Scenario_Simulator SHALL store a Cache_Entry for that prefix.
6. WHEN a scenario completes, THE Scenario_Simulator SHALL compute cost without cache as the per-prefix cost multiplied by the request count.
7. WHEN a scenario completes, THE Scenario_Simulator SHALL compute cost with cache as the per-prefix cost multiplied by the cache miss count.
8. WHEN a scenario completes, THE Scenario_Simulator SHALL compute savings USD as cost without cache minus cost with cache.
9. IF cost without cache is greater than zero, THEN THE Scenario_Simulator SHALL compute savings percentage as (savings USD ÷ cost without cache) × 100.
10. IF cost without cache is zero, THEN THE Scenario_Simulator SHALL report a savings percentage of 0.0.
11. WHERE a fixed random seed is configured, THE Scenario_Simulator SHALL produce identical cache miss counts, cost values, and savings values across repeated runs of the same scenario.
12. IF a scenario is requested by an identifier that does not match one of the five scenarios, THEN THE Scenario_Simulator SHALL report an error indicating the scenario identifier is unknown and SHALL run no simulation.

### Requirement 7: All-Models Comparison

**User Story:** As an AI engineer, I want one workload run across all six models, so that I can compare cost and savings at different model scales.

#### Acceptance Criteria

1. WHEN an all-models comparison is requested with a prefix length that is an integer from 1 to 1,000,000 tokens, a request count that is an integer from 1 to 1,000,000, and a prefix reuse rate from 0.0 to 1.0 inclusive, THE Scenario_Simulator SHALL run a workload using the identical prefix length, request count, and reuse rate against each of the six Model_Config entries in the Model_Catalog.
2. WHEN the all-models comparison completes, THE Scenario_Simulator SHALL produce exactly one result per model, ordered to match the Model_Catalog ordered collection, with each result containing the hit rate as a percentage from 0 to 100, the per-prefix cost in USD, the total TFLOPs saved, and the savings percentage from 0 to 100.
3. WHERE NumPy is used to compute per-model costs, THE Scenario_Simulator SHALL produce values numerically equal to the per-model scalar computation within a relative tolerance of 1e-9.
4. IF the prefix length is less than 1, OR the request count is less than 1, OR the prefix reuse rate is outside the range 0.0 to 1.0 inclusive, THEN THE Scenario_Simulator SHALL reject the comparison with an error indicating which parameter is invalid and SHALL run no model workload and produce no results.
5. WHERE a fixed random seed is configured, THE Scenario_Simulator SHALL produce identical all-models comparison results across repeated runs that use the same prefix length, request count, and reuse rate.

### Requirement 8: Document Store and ID-Based Retrieval

**User Story:** As an AI engineer, I want a simulated document knowledge base with ID-based retrieval, so that the RAG simulation models retrieve-then-read without semantic embeddings.

#### Acceptance Criteria

1. WHEN a Document_Store is created with a document count between 1 and 1,000,000 inclusive and a tokens-per-document value between 1 and 1,000,000 inclusive, THE Document_Store SHALL contain exactly that many documents, each having exactly the given token count, and SHALL define the valid index range as the integers 0 through document count minus 1 inclusive.
2. WHEN retrieval is requested with a set of document indices, THE Document_Store SHALL return exactly one document for each requested index that falls within the valid index range.
3. IF a requested document index is less than 0 or greater than document count minus 1, THEN THE Document_Store SHALL exclude that index from the retrieved set and SHALL return the remaining in-range documents without raising an error.
4. THE Document_Store SHALL perform retrieval by document index only, without semantic embedding computation or similarity scoring.
5. IF a Document_Store is created with a document count less than 1 or a tokens-per-document value less than 1, THEN THE Document_Store SHALL reject the creation, SHALL not create a store, and SHALL return an error indicating which parameter was invalid.
6. IF a retrieval request contains no indices within the valid index range, THEN THE Document_Store SHALL return an empty retrieved set without raising an error.

### Requirement 9: Exact-Set Prefix Matching

**User Story:** As an AI engineer, I want retrieved document sets matched by content regardless of order, so that the same documents produce the same cache key.

#### Acceptance Criteria

1. WHEN a set of retrieved document indices is selected, THE RAG_Simulator SHALL remove duplicate indices and sort the remaining indices into ascending order before building the prefix.
2. WHEN two retrievals contain the same set of unique document indices in different orders, THE RAG_Simulator SHALL produce identical prefix hashes.
3. IF two retrievals contain different sets of unique document indices, THEN THE RAG_Simulator SHALL produce prefixes with differing content and therefore different prefix hashes.
4. WHEN building a prefix, THE RAG_Simulator SHALL form the prefix from the retrieved documents concatenated in ascending index order, followed by a single fixed query block appended after the final document.
5. WHEN no document indices are selected, THE RAG_Simulator SHALL form the prefix from the fixed query block only.

### Requirement 10: RAG Pipeline Simulation

**User Story:** As an AI engineer, I want a RAG pipeline cost simulation, so that I can quantify prefix-cache savings for a retrieve-then-read workload with realistic document popularity.

#### Acceptance Criteria

1. THE RAG_Simulator SHALL accept a configuration containing a Model_Config, a number of documents that is an integer from 1 to 1,000,000, a tokens-per-document value that is an integer from 1 to 1,000,000, a top-K value that is an integer from 1 to the number of documents, a query token count that is an integer from 0 to 1,000,000, a request count that is an integer from 1 to 10,000,000, a Zipf exponent greater than 0.0, and a requests-per-day value that is an integer from 1 to 1,000,000,000.
2. IF any configuration field is outside its accepted range, OR the top-K value exceeds the number of documents, THEN THE RAG_Simulator SHALL reject the configuration with an error indicating which field is invalid and SHALL run no simulation.
3. WHEN the RAG_Simulator runs a request, THE RAG_Simulator SHALL select top-K document indices using a Zipf_Distribution parameterized by the configured Zipf exponent.
4. THE RAG_Simulator SHALL compute the prefix token count as top_k × tokens_per_doc + query_tokens.
5. WHEN a request produces a prefix-cache miss, THE RAG_Simulator SHALL store a Cache_Entry whose cost reflects the full prefix token count and SHALL increment the cache miss count by one.
6. WHEN a request produces a prefix-cache hit, THE RAG_Simulator SHALL increment the cache hit count by one and SHALL store no new Cache_Entry.
7. WHEN the simulation completes, THE RAG_Simulator SHALL compute cost without cache as cost-per-uncached-prefix multiplied by the request count, and cost with cache as cost-per-uncached-prefix multiplied by the cache miss count.
8. IF cost without cache is greater than zero, THEN THE RAG_Simulator SHALL compute savings percentage as (savings USD ÷ cost without cache) × 100.
9. IF cost without cache is zero, THEN THE RAG_Simulator SHALL report a savings percentage of zero without performing division.
10. WHERE a fixed random seed is configured, THE RAG_Simulator SHALL produce identical hit rate and savings results across repeated runs of the same configuration.
11. WHERE NumPy random sampling is used for Zipf selection, THE RAG_Simulator SHALL constrain selected document indices to the valid range of zero through number-of-documents minus one.

### Requirement 11: Monthly Cost Projection

**User Story:** As a non-technical decision maker, I want a monthly cost projection, so that I can make budget decisions about AI features.

#### Acceptance Criteria

1. WHEN a RAG simulation completes with a request count greater than zero, THE RAG_Simulator SHALL compute the per-request average cost without cache (in USD) as the total cost without cache divided by the request count, and the per-request average cost with cache (in USD) as the total cost with cache divided by the request count.
2. IF a RAG simulation completes with a request count of zero, THEN THE RAG_Simulator SHALL report a per-request average cost without cache of zero and a per-request average cost with cache of zero.
3. WHEN a RAG simulation completes, THE RAG_Simulator SHALL compute monthly request volume as the configured requests per day multiplied by 30.
4. WHEN a RAG simulation completes, THE RAG_Simulator SHALL compute projected monthly cost without cache (in USD) as the per-request average cost without cache multiplied by monthly request volume.
5. WHEN a RAG simulation completes, THE RAG_Simulator SHALL compute projected monthly cost with cache (in USD) as the per-request average cost with cache multiplied by monthly request volume.
6. WHEN a RAG simulation completes, THE RAG_Simulator SHALL compute projected monthly savings (in USD) as projected monthly cost without cache minus projected monthly cost with cache.

### Requirement 12: Command-Line Interface

**User Story:** As a user, I want a single command-line entry point with a mode flag, so that I can run any simulation from the terminal.

#### Acceptance Criteria

1. THE CLI SHALL provide a single command-line entry point invokable as a Python module from the `python/` subdirectory.
2. THE CLI SHALL accept a mode flag whose set of valid values is exactly three: prefix-cache scenarios, all-models comparison, and RAG simulation, matched case-sensitively.
3. WHEN no mode flag is provided, THE CLI SHALL run the prefix-cache scenarios mode as the default mode and terminate with a success exit code of 0.
4. IF a mode value other than the three valid values is provided, THEN THE CLI SHALL terminate without running any simulation, print an error message indicating that the provided value is unrecognized and listing the three valid mode values, and terminate with a non-zero exit code.
5. WHEN a simulation mode completes successfully, THE CLI SHALL print a human-readable report for that mode to standard output and terminate with a success exit code of 0.
6. THE CLI SHALL operate without starting a web server or network listener.

### Requirement 13: Report Formatting

**User Story:** As a stakeholder, I want clearly formatted reports, so that I can read cost numbers and savings at a glance.

#### Acceptance Criteria

1. WHEN formatting a monetary value, THE Report_Formatter SHALL display it with a comma as the thousands separator and exactly two decimal places.
2. WHEN formatting an integer count, THE Report_Formatter SHALL display it with a comma as the thousands separator and no decimal places.
3. WHEN formatting a percentage value, THE Report_Formatter SHALL display it with exactly one decimal place.
4. WHEN a TFLOPs value is at least 1000, THE Report_Formatter SHALL display it in PFLOPs, computed as the TFLOPs value divided by 1000, with exactly one decimal place.
5. WHEN a TFLOPs value is at least 1 and less than 1000, THE Report_Formatter SHALL display it in TFLOPs with exactly two decimal places.
6. IF a TFLOPs value is less than 1, THEN THE Report_Formatter SHALL display it in TFLOPs with exactly four decimal places.
7. WHEN rendering a RAG report, THE Report_Formatter SHALL include cache hit rate, compute saved, cost per uncached prefix, per-run totals (without cache, with cache, saved USD, saved percentage), and the monthly projection (without cache, with cache, saved per month).
8. WHEN rendering an all-models comparison, THE Report_Formatter SHALL present one row per model with hit rate, per-prefix cost, total saved, and reduction percentage.

### Requirement 14: Test Coverage

**User Story:** As a developer, I want unit tests with pytest, so that the simulation's correctness is verifiable.

#### Acceptance Criteria

1. THE Simulation_System SHALL include pytest unit tests that assert computed attention FLOP, TFLOPs, KV-cache-size, and H100 dollar cost values equal their expected reference values within a relative tolerance of 1e-9, including the zero-sequence-length boundary case.
2. THE Simulation_System SHALL include pytest unit tests that assert prefix-cache hit returns the matching Cache_Entry and records a hit, prefix-cache miss returns no entry and records a miss, and LRU eviction removes the least-recently-used entry when a store occurs at full capacity.
3. THE Simulation_System SHALL include pytest unit tests that assert the retrieved document set equals the expected document set by order-independent set equality, including the empty-set and full-overlap boundary cases.
4. THE Simulation_System SHALL include pytest unit tests that assert savings math and monthly projection math equal their expected reference values within a relative tolerance of 1e-9, including the zero-cost-without-cache boundary case where savings percentage is reported as zero.
5. WHEN the test suite is run, THE Simulation_System SHALL execute all included tests and report each as passing or failing.
6. IF one or more tests fail when the test suite is run, THEN THE Simulation_System SHALL report the count and identity of the failing tests and terminate with a non-zero exit status.

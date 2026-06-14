# Distributed Caching System with LLM Inference Optimization

## Tagline
End-to-end distributed key-value cache built from scratch in Go, with a Python simulation layer that measures LLM prefix caching savings across 6 open-source models using real transformer math.

---

## Tech Stack

| Category | Technologies |
|---|---|
| **Language** | Go 1.22, Python 3.11 |
| **APIs** | REST API (HTTP/JSON), gRPC, Protocol Buffers |
| **Database** | PostgreSQL 16 (pgx/v5, connection pooling) |
| **Infrastructure** | Docker, Docker Compose, multi-stage Dockerfile |
| **Observability** | Prometheus, Grafana (provisioned dashboards), `/metrics` endpoints |
| **AI / ML** | NumPy, LLM prefix caching, RAG pipeline simulation |
| **Tools** | Git, Make, protoc, race detector (`go test -race`), pytest, Hypothesis |

---

## Problem

AI companies serving millions of users (Groq, Together.ai, Cursor, Perplexity) pay to recompute the same GPU work millions of times per day.

Every LLM request that includes a shared system prompt recomputes the attention matrices for that prefix from scratch. On Llama-3-70B at H100 pricing, a 2,048-token shared prefix costs ~$0.000024 per request in GPU time. At one million daily requests with 90% prefix reuse, that is ~$21.60 wasted per day, ~$8,000 per year, on a single prompt template.

The same problem exists in traditional backend systems. Without a caching strategy, every read request triggers a full database roundtrip regardless of whether the same data was fetched milliseconds ago. Engineers have no empirical basis to choose between LRU, LFU, write-through, or write-behind without live data to compare them.

---

## Solution

Built a full-stack distributed caching sandbox in Go from scratch (no Redis wrapper, no managed cache) plus a Python simulation module that measures LLM prefix caching savings using real transformer math.

**What was built (5 running services + 2 simulation modules):**

**In-memory cache** — hash map with per-entry TTL, lazy expiry (same approach as Redis), and 4 swappable eviction policies (LRU, LFU, TTL-only, Random) behind a common Go interface, benchmarkable under identical Zipf-distributed workloads.

**3-node cache cluster** — consistent hashing with 150 virtual nodes per physical node (FNV-1a, O(log n) binary search) and Raft consensus for leader election. Node failure routes cache misses to PostgreSQL with zero errors surfaced to callers.

**REST API layer** — product catalog with cache-aside pattern, 3 swappable invalidation strategies (TTL, write-through, write-behind), `X-Cache: HIT/MISS` response headers, and a `/cache/debug/{key}` endpoint showing which cluster node owns a given key in real time.

**Reverse proxy** — caches full HTTP responses (status + headers + body) keyed by URL, respects `Cache-Control` semantics, restricted to 2xx responses only. (Discovered and fixed a bug during live testing where 404s were being cached.)

**Observability stack** — Prometheus with policy/strategy labels and Grafana dashboards provisioned at startup. Real-time dark-theme SPA dashboard served by the proxy with 2-second cluster status polling.

**Python LLM simulation** — two CLI modes. The prefix cache module simulates KV state caching across 6 open-source models (Llama-3-8B/70B, Mistral-7B, Mixtral-8x7B, Qwen2.5-72B, DeepSeek-R1-671B) using real transformer architecture specs from Hugging Face. The RAG pipeline module simulates a retrieve-then-read workload with Zipf-distributed document popularity and outputs a monthly cost projection. 206 tests, all passing.

---

## My Role

Sole architect and engineer. All design and implementation decisions were mine.

**Interface design** — defined `EvictionPolicy` and `InvalidationStrategy` as Go interfaces so all 4 eviction policies and 3 invalidation strategies plug in at construction time without touching handler code. This is what makes direct benchmark comparison under identical workloads possible.

**Distributed systems design** — chose Raft for leader election only, not write replication. Documented the tradeoff in ADR 0001. Chose mixed protocols (gRPC internally, HTTP/REST externally) to mirror real production boundaries.

**LLM simulation** — sourced real transformer architecture configs from Hugging Face `config.json` files, derived FLOP savings math from attention formulas, and validated with unit tests including manual verification of Llama-3-70B at 512 tokens.

**RAG pipeline** — built a Python module with Zipf-distributed document retrieval, exact-set prefix matching, and a monthly cost projection. Headline result: 93.9% compute reduction and $420/month saved at 1M daily requests on Llama-3-70B.

**Bug discovery and fix** — identified during live testing that the proxy was caching 404 responses. Fixed by restricting caching to `statusCode >= 200 && statusCode < 300`.

**Test coverage** — 17 Go unit tests for the LLM Go module (FLOP math, cache hit/miss, LRU eviction, stats), all passing with `go test -race`. 206 Python tests combining pytest unit tests and Hypothesis property-based tests covering 24 correctness properties.

---

## Impact

**Go backend throughput** — processed ~10,600 read requests/sec and ~6,000 writes/sec at 100% success rate under concurrent load. This measures the throughput capacity a backend needs when the cost of degradation is measured in lost requests per second.

**Cluster fault tolerance** — leader election in ~163ms on startup and ~259ms average re-election after node failure across 5 trials. The system self-heals in under 300ms with zero requests dropped.

**Eviction policy benchmarks** — measured LRU/LFU at 98.7% cache hit rate vs TTL-only at 98.4% across 10,000 operations on identical Zipf workloads. Gives engineers concrete data to justify policy selection instead of defaulting to LRU without evidence.

**LLM prefix caching (Go module)** — 92.3% compute reduction in a 1,000-request RAG pipeline scenario (2,048-token shared prefix on Llama-3-70B), saving 10.1 PFLOPs and ~$0.023 per 1,000 requests (~$23,000 per billion requests).

**Model scale comparison** — DeepSeek-R1-671B costs 4.19 TFLOPs per 1,024-token prefix vs 0.55 TFLOPs for Llama-3-8B. Doubling prompt length quadruples prefill cost, which is the main reason prefix caching ROI grows with model size.

**RAG pipeline simulation (Python module)** — 93.9% compute reduction and $420/month saved at 1M daily requests on the default config. Per-run totals plus monthly projections give budget owners a number they can act on.

**Cache failure behavior** — 100% request success rate during single-node failure by routing cache misses to PostgreSQL without surfacing errors to callers. End users see slightly higher latency but no failures. [ESTIMATE: p99 latency under 1ms on local Docker network; remote DB would show a larger gap on the cache-hit path.]

---

## How It Works

### System architecture

```
Client (browser / curl)
    ↓  HTTP/REST
Reverse Proxy :8080       ← full HTTP response cache, Cache-Control semantics
    ↓  gRPC
API Layer :8081           ← product catalog, cache-aside, invalidation strategies
    ↓  gRPC                           ↓  SQL
Cache Cluster :9001-9003        PostgreSQL :5432
(Raft + consistent hashing)

Prometheus :9090  ← scrapes /metrics from all 5 services (5s interval)
Grafana :3000     ← 3 pre-provisioned dashboards (cluster, eviction, invalidation)
Dashboard SPA     ← served at GET / by the proxy, polls /cluster/status every 2s

Python CLI        ← python -m llmsim --mode=prefix-cache|all-models|rag
```

### Key design decisions and tradeoffs

**1. Interface-driven eviction and invalidation**
Both `EvictionPolicy` and `InvalidationStrategy` are Go interfaces. The benchmark runner instantiates all variants and exercises them under the same workload. This is only possible because the interface boundary is enforced at compile time. The tradeoff is slight indirection overhead against the ability to swap strategies at startup via a single environment variable.

**2. Consistent hashing with 150 virtual nodes per physical node**
Each physical node gets 150 positions on a uint32 circular ring (FNV-1a hashed). Binary search gives O(log n) key lookup. When a node is removed, only keys in the affected arc remap. Verified by unit test across 100 keys on a 3-node ring.

**3. Raft for election only, no data replication (ADR 0001)**
Cache data lives on exactly one node per consistent hashing. If that node dies, its keys become cache misses that fall through to PostgreSQL. The tradeoff is simpler implementation (no log replication, no commit index) at the cost of cache availability during node failure. Result: 100% request success during failure, with the DB absorbing the load.

**4. Lazy TTL expiry**
No background goroutine scans for expired entries. Expiry is checked on `Get`, the same approach Redis uses. Expired entries occupy memory until accessed, but this avoids lock contention from a background sweeper competing with read/write goroutines under high concurrency.

**5. LLM FLOP math from real architecture configs**
Attention cost formula: `4 x num_layers x num_heads x head_dim x seq_len^2`. Doubling a prompt from 1,024 to 2,048 tokens quadruples prefill cost, verified by unit test. Model configs sourced from Hugging Face. Cost converted using H100 SXM5: 312 TFLOPS at $2.50/hr. The simulation stores only hash + metadata, no real tensors needed.

**6. Python Zipf sampling with rejection (not modulo)**
The RAG simulator samples document indices using NumPy's Zipf generator, which returns values >= 1 and is unbounded above. Mapping into range with modulo would fold the long tail back onto the head and distort document popularity. Instead, out-of-range samples are discarded and redrawn. This preserves the hot-document skew that makes prefix caching effective in the first place.

**7. Proxy caches 2xx responses only**
The initial implementation cached any non-5xx response. During testing, a 404 returned before products were seeded was cached and served for subsequent valid requests. Fixed by restricting caching to `statusCode >= 200 && statusCode < 300`. Error responses are transient by nature and should never be cached.

---

## Keywords

**Backend / API:** Go, REST API, gRPC, Protobuf, HTTP, JSON, backend development, microservices, concurrent programming, goroutines, high-throughput systems

**Database / SQL:** PostgreSQL, SQL, connection pooling, cache-aside pattern, write-through, write-behind, database optimization

**Infrastructure / DevOps:** Docker, Docker Compose, containerization, multi-stage build, Git, Makefile, CI/CD-ready, large-scale systems

**Distributed Systems:** Raft consensus, leader election, consistent hashing, distributed cache, fault tolerance, graceful degradation, cluster coordination

**Observability:** Prometheus, Grafana, metrics instrumentation, p99 latency, throughput, hit rate, dashboards

**AI / ML Systems:** Python, NumPy, LLM inference optimization, KV cache, prefix caching, RAG pipeline, LangChain style retrieval, transformer architecture, FLOP analysis, open-source models (Llama, Mistral, Mixtral, Qwen, DeepSeek), Hugging Face, compute cost reduction, token compute, Hypothesis property-based testing

**Recruiter-targeted:** Large-scale backend, high-throughput systems, fault-tolerant architecture, empirical benchmarking, system design, cross-functional impact (engineering + product + finance)

---

## Locked Resume Bullets

### Backend SWE

**B1 — LOCKED**
Built a 3-node cache cluster in Go with gRPC, PostgreSQL, and Docker achieving 10,600 reads per second to understand how engineering teams keep their product fast and available during the busiest hours of the day so companies stop losing revenue when traffic peaks.
Keywords: [Go, gRPC, PostgreSQL, Docker]
Metric type: MEASURED

**B2 — LOCKED**
Built a testing system using REST API, SQL, Go, and CI/CD to handle thousands of simultaneous user requests on the product to ensure that companies never serve corrupted product data to customers during peak traffic.
Keywords: [REST API, SQL, Go, CI/CD]
Metric type: MEASURED

**B3 — LOCKED**
Configured Prometheus and Grafana dashboards with Git across all servers to give operations teams a live view of system performance so they can present real data to stakeholders and influence infrastructure investment decisions.
Keywords: [Prometheus, Grafana, Git]
Metric type: ESTIMATE

### AI Engineer

**AI-1 — LOCKED**
Built a prefix caching simulation in Python using NumPy and Git comparing compute cost across open source models including Llama, Mistral, Qwen, and DeepSeek to help AI teams understand how model size affects infrastructure spend before choosing a model for production.
Keywords: [Python, NumPy, Git, LLMs]
Metric type: MEASURED

**AI-2 — LOCKED**
Built a RAG pipeline simulation in Python with LangChain style document retrieval and NumPy showing 93.9% compute reduction and $420 saved per month at 1M daily requests to help AI teams influence stakeholders to make smarter spending decisions on AI features before committing budget.
Keywords: [Python, RAG, LangChain, NumPy]
Metric type: MEASURED

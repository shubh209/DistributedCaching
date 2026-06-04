# CONTEXT.md

## Glossary

### Project
A **learning sandbox** — a locally runnable system that demonstrates caching behavior at each layer of a distributed architecture. Not intended for production deployment.

### Implemented Layers
The layers that are built as real running Go services: in-memory cache, API layer (with caching middleware), reverse proxy (with cache), and database layer (with read-through cache).

### Conceptual Layers
Layers covered through documentation and diagrams only, not implemented as running services: DNS-level caching, load balancer caching.

### In-Memory Cache
A cache built from scratch in Go using a hash map with TTL expiry. Not a wrapper around Redis or Memcached.

### Eviction Policy
A swappable strategy that determines which cache entry is removed when the cache is full. All policies are implemented behind a common interface so they can be benchmarked and compared. Policies: LRU (Least Recently Used), LFU (Least Frequently Used), TTL-only, and Random.

### Eviction Benchmark
A comparative measurement of eviction policies under the same workload — used to surface trade-offs (hit rate, memory pressure, latency, fairness) between policies.

### Cache Cluster
A set of cache nodes that collectively own the full keyspace via consistent hashing. A key is always routed to a specific node — it is not replicated across all nodes by default.

### Consistent Hashing
The key distribution algorithm used to assign keys to cache nodes. Ensures minimal key remapping when nodes join or leave the cluster.

### Raft Consensus
Used exclusively for leader election among cache nodes. The elected leader acts as the cluster coordinator for routing decisions. Cache data is NOT replicated via Raft — keys live on specific nodes per consistent hashing.

### Coordinator
The elected leader node in the cache cluster, chosen via Raft. Responsible for routing incoming requests to the correct cache node.

### API Layer
A domain-specific HTTP API that simulates a real application. Caching is applied at the handler level, making cache key design, invalidation, and staleness concrete and observable.

### Domain
The simulated application domain is a **product catalog** — a read-heavy system where products are fetched frequently and updated occasionally. Endpoints include product lookup, listing, and updates.

### Cache Invalidation Strategy
A swappable policy that determines how the cache is kept consistent with the source of truth after a write. Three strategies are implemented: TTL-based expiry (stale data served until expiry), write-through (cache updated synchronously on every write), and write-behind (cache invalidated asynchronously after the write). All three are benchmarkable against each other.

### Reverse Proxy
A Go HTTP proxy that sits in front of the API layer and caches full HTTP responses (status code, headers, body) keyed by URL. The proxy has no domain knowledge — it caches responses regardless of content. Implements Cache-Control header semantics.

### Source of Truth
PostgreSQL — the persistent database that holds the authoritative product catalog data. Run locally via Docker.

### Cache-Aside
The caching pattern used at the database layer. The application checks the cache first; on a miss, it fetches from PostgreSQL and populates the cache. The application is responsible for cache population and invalidation — the cache does not interact with the DB directly.

### Observability Stack
Prometheus + Grafana, run as Docker services alongside PostgreSQL. Every component (cache nodes, API layer, reverse proxy) exposes a `/metrics` endpoint. Metrics include cache hit rate, miss rate, eviction counts, and request latency — used to compare eviction policies and invalidation strategies visually.

### Communication Protocol
Mixed: HTTP/REST for external-facing communication (client → reverse proxy → API layer), gRPC for internal communication (cache node ↔ coordinator, cache node ↔ cache node). Protobuf is used for gRPC message definitions.

### Project Structure
A single Go module (one `go.mod` at the root) with packages per service under `internal/`. Shared types (interfaces, metrics, config) live in shared packages. All services are orchestrated via Docker Compose.

### Build Order
The intended implementation sequence:
1. In-memory cache core (eviction policies: LRU, LFU, TTL, Random + consistent hashing)
2. Cache cluster + Raft leader election (3-node coordinator)
3. API layer (product catalog HTTP API + cache-aside against PostgreSQL)
4. Reverse proxy (full HTTP response caching)
5. Observability (Prometheus + Grafana instrumentation across all layers)


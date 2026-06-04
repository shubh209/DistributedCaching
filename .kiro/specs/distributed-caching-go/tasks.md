# Implementation Plan: distributed-caching-go

## Overview

Build a locally runnable Go learning sandbox demonstrating caching at every layer of a distributed system. The implementation follows the build order: project scaffolding → cache core → eviction policies → consistent hashing + cluster → Raft leader election → full cluster wiring → database layer → API layer → invalidation strategies → reverse proxy → observability → benchmarks → documentation → integration tests.

All services are Go packages under `internal/`, compiled to binaries under `cmd/`, and orchestrated via Docker Compose. gRPC is used for all internal communication; HTTP/REST for external.

## Tasks

- [ ] 1. Project scaffolding and shared interfaces
  - [x] 1.1 Initialise Go module and directory structure
    - Create `go.mod` at repository root with module path `github.com/user/distributed-caching-go`
    - Create all directories: `cmd/cachenode`, `cmd/api`, `cmd/proxy`, `cmd/benchmark`, `internal/shared`, `internal/cache`, `internal/eviction`, `internal/cluster`, `internal/raft`, `internal/api`, `internal/invalidation`, `internal/proxy`, `internal/metrics`, `internal/benchmark`, `internal/db/migrations`, `proto/cache/v1`, `proto/raft/v1`, `proto/api/v1`, `observability/prometheus`, `observability/grafana/datasources`, `observability/grafana/dashboards`, `docs/adr`
    - Create a root `Makefile` with targets: `proto-gen`, `build`, `test`, `bench`, `up`, `down`
    - _Requirements: 11.1, 11.2_
  - [ ] 1.2 Define shared interfaces and config loader
    - Write `internal/shared/eviction.go` — `EvictionPolicy` interface (`OnAccess`, `OnInsert`, `OnEvict`, `Evict`, `Name`)
    - Write `internal/shared/invalidation.go` — `InvalidationStrategy` interface (`OnWrite`, `Name`)
    - Write `internal/shared/cache.go` — `Cache` interface (`Get`, `Set`, `Delete`, `Len`)
    - Write `internal/shared/config.go` — config struct and loader (env vars + YAML) for `EVICTION_POLICY`, `CACHE_CAPACITY`, `INVALIDATION_STRATEGY`, `CACHE_CLUSTER_ADDR`, `DB_DSN`
    - _Requirements: 1.4, 5.5, 11.5_

- [ ] 2. In-memory cache core
  - [ ] 2.1 Implement `CacheEntry` and core cache hash map with TTL
    - Write `internal/cache/entry.go` — `CacheEntry` struct with `Key`, `Value`, `ExpiresAt`, `CreatedAt`, `AccessedAt`, `AccessCount`, and `IsExpired(now time.Time) bool`
    - Write `internal/cache/cache.go` — `Cache` struct backed by `map[string]*CacheEntry`, `sync.RWMutex`, configurable capacity, and eviction policy dispatch; implement `Get`, `Set`, `Delete`, `Len`
    - `Set` must upsert (replace value + reset TTL for existing keys) and call `EvictionPolicy.OnInsert` / `OnAccess` appropriately
    - `Get` must call `EvictionPolicy.OnAccess` on hit and return `(nil, false, nil)` on miss or expired entry
    - Reject `Set` with negative TTL; treat TTL=0 as immediately expired
    - _Requirements: 1.1, 1.2, 1.3, 1.5, 1.6, 1.7_
  - [ ]* 2.2 Write property test — Property 1: TTL expiry makes entries absent
    - **Property 1: TTL expiry makes entries absent**
    - Use `rapid.StringN` for keys, `rapid.IntRange(1, 86400)` for TTL; advance a mock clock past TTL; assert `Get` returns `(nil, false)`
    - **Validates: Requirements 1.1, 1.2, 1.5**
  - [ ]* 2.3 Write property test — Property 2: Cache capacity is never exceeded
    - **Property 2: Cache capacity is never exceeded**
    - Use `rapid.IntRange(1, 1000)` for capacity, `rapid.SliceOf` for key-value pairs; after any sequence of `Set` calls assert `cache.Len() <= capacity`
    - **Validates: Requirements 1.3**
  - [ ]* 2.4 Write property test — Property 3: Upsert replaces value and resets TTL
    - **Property 3: Upsert replaces value and resets TTL**
    - Insert a key, then `Set` again with a different value and TTL; assert `Get` returns the new value and the new expiry is in effect
    - **Validates: Requirements 1.7**
  - [ ]* 2.5 Write property test — Property 4: Get on absent key always returns miss
    - **Property 4: Get on absent key always returns miss**
    - Use `rapid.String` for keys never inserted (or whose TTL has elapsed); assert `Get` returns `(nil, false)` and never `true`
    - **Validates: Requirements 1.5**

- [ ] 3. Eviction policy implementations
  - [ ] 3.1 Implement LRU eviction policy
    - Write `internal/eviction/lru.go` — doubly-linked list + map; `OnAccess` moves key to front; `Evict` removes tail
    - Implement all five `EvictionPolicy` interface methods; `Name()` returns `"lru"`
    - _Requirements: 2.1_
  - [ ]* 3.2 Write property test — Property 5: LRU evicts the least recently accessed entry
    - **Property 5: LRU evicts the least recently accessed entry**
    - Generate a full cache and a random access sequence; insert one new entry; assert the evicted key has the oldest last-access timestamp
    - **Validates: Requirements 2.1**
  - [ ] 3.3 Implement LFU eviction policy
    - Write `internal/eviction/lfu.go` — min-heap keyed by `(accessCount, lastAccessedAt)`; `OnAccess` increments frequency; `Evict` pops minimum
    - Tie-break by earliest last-access timestamp
    - _Requirements: 2.2_
  - [ ]* 3.4 Write property test — Property 6: LFU evicts the lowest-frequency entry (tie-broken by timestamp)
    - **Property 6: LFU evicts the lowest-frequency entry (tie-broken by timestamp)**
    - Generate a frequency map; insert one new entry; assert evicted key has minimum access count; on tie assert earliest last-access timestamp is evicted
    - **Validates: Requirements 2.2**
  - [ ] 3.5 Implement TTL-only eviction policy
    - Write `internal/eviction/ttl.go` — min-heap ordered by `ExpiresAt`; `OnInsert` pushes entry; `Evict` pops earliest expiry
    - _Requirements: 2.3_
  - [ ]* 3.6 Write property test — Property 7: TTL-only evicts the entry with the earliest expiry
    - **Property 7: TTL-only evicts the entry with the earliest expiry**
    - Generate a full cache with varied TTLs; insert one new entry; assert evicted key had the earliest `ExpiresAt`
    - **Validates: Requirements 2.3**
  - [ ] 3.7 Implement Random eviction policy
    - Write `internal/eviction/random.go` — reservoir sampling over current key set; `Evict` returns a uniformly random key
    - _Requirements: 2.4_
  - [ ]* 3.8 Write property test — Property 8: Random eviction always removes an existing entry
    - **Property 8: Random eviction always removes an existing entry**
    - Generate a full cache of size N; insert one new entry; assert cache contains exactly N entries and the evicted key was one of the pre-insertion entries
    - **Validates: Requirements 2.4**
  - [ ] 3.9 Verify all four policies satisfy `EvictionPolicy` interface (compile-time check)
    - Add a `var _ shared.EvictionPolicy = (*LRU)(nil)` style assertion for each policy in `internal/eviction/eviction_test.go`
    - _Requirements: 1.4_

- [ ] 4. Checkpoint — cache core and eviction policies
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 5. Consistent hashing ring and coordinator
  - [ ] 5.1 Implement consistent hashing ring
    - Write `internal/cluster/ring.go` — `Ring` struct with `sync.RWMutex`, `nodes map[uint32]string`, `sortedTokens []uint32`, `nodeAddrs map[string]string`
    - Implement `Add(nodeID, addr string)` — generates 150 virtual tokens via FNV-1a hash of `"nodeID-N"` strings
    - Implement `Remove(nodeID string)` — removes all virtual tokens for the node
    - Implement `Get(key string) (nodeID string, ok bool)` — clockwise lookup using `sort.Search`
    - _Requirements: 3.2, 3.3_
  - [ ]* 5.2 Write property test — Property 9: Consistent hashing is deterministic
    - **Property 9: Consistent hashing is deterministic**
    - Use `rapid.String` for keys; call `ring.Get(key)` multiple times on the same ring; assert all calls return the same node ID
    - **Validates: Requirements 3.2**
  - [ ]* 5.3 Write property test — Property 10: Consistent hashing minimises remapping on node removal
    - **Property 10: Consistent hashing minimises remapping on node removal**
    - Distribute a set of keys across a 3-node ring; remove one node; assert only keys previously assigned to that node change assignment
    - **Validates: Requirements 3.3**
  - [ ] 5.4 Implement coordinator routing logic and node health tracking
    - Write `internal/cluster/node.go` — `NodeHealth` struct tracking last-probe time and availability; health probe timeout = 500ms
    - Write `internal/cluster/coordinator.go` — `Coordinator` struct that holds the ring and node health map; implements `Route(key string) (nodeAddr string, err error)` which checks health before routing; returns `ErrNoCoordinator` when no leader elected, `ErrRoutingMismatch` on ownership check failure, and cache-miss signal when target node is unavailable
    - _Requirements: 3.4, 3.5, 3.7, 4.6_

- [ ] 6. Raft leader election state machine
  - [ ] 6.1 Define Raft state machine types and persistent state
    - Write `internal/raft/state.go` — `RaftNode` struct with `id`, `state NodeState` (Follower/Candidate/Leader), `currentTerm int64`, `votedFor string`, `leaderID string`, `peers []PeerConfig`, `electionTimer *time.Timer`, `stopCh chan struct{}`
    - Implement persistent state load/save to a per-node JSON file (`currentTerm`, `votedFor`) so a restarted node does not vote twice in the same term
    - _Requirements: 4.1, 4.3, 4.5_
  - [ ] 6.2 Implement Raft election timeout and state transitions
    - Write `internal/raft/timer.go` — randomised election timeout in [150ms, 300ms]; heartbeat interval = 50ms
    - Implement state transitions: Follower → Candidate on timeout; Candidate → Leader on majority votes; Candidate → Follower on higher term or valid `AppendEntries`; Leader → Follower on higher term in any RPC response
    - Implement split-vote handling: restart election with new randomised timeout
    - _Requirements: 4.1, 4.2, 4.5_
  - [ ] 6.3 Implement Raft RPC handlers (RequestVote and AppendEntries)
    - Write `internal/raft/rpc.go` — `RequestVote` handler (grant vote if term ≥ currentTerm and not yet voted); `AppendEntries` handler (heartbeat only — no log entries; reset election timer; update leaderID)
    - When a node transitions to Leader, broadcast identity via `AppendEntries` heartbeats so the coordinator can register the leader's gRPC address
    - _Requirements: 4.1, 4.4, 4.6_

- [ ] 7. Protobuf definitions and gRPC code generation
  - [ ] 7.1 Write `.proto` files for all three gRPC services
    - Write `proto/cache/v1/cache.proto` — `CacheService` with `Get`, `Set`, `Delete`, `Health`, `Stats` RPCs and all message types as specified in the design
    - Write `proto/raft/v1/raft.proto` — `RaftService` with `RequestVote` and `AppendEntries` RPCs (heartbeat only)
    - Write `proto/api/v1/api.proto` — `APIService` with `GetProduct`, `ListProducts`, `UpdateProduct` RPCs
    - _Requirements: 10.3, 10.4_
  - [ ] 7.2 Add proto generation to Makefile and generate Go bindings
    - Add `proto-gen` Makefile target that runs `protoc` with `protoc-gen-go` and `protoc-gen-go-grpc` for all three `.proto` files; output to `internal/proto/`
    - Run `make proto-gen` and verify generated files compile without errors
    - _Requirements: 10.5, 10.6_

- [ ] 8. Cache cluster wiring — gRPC server, health checks, and cmd binary
  - [ ] 8.1 Implement gRPC `CacheService` server on each cache node
    - Write the gRPC server in `internal/cluster/` that implements the generated `CacheService` interface; delegate `Get`/`Set`/`Delete` to the in-memory cache; delegate `Health` to node health tracking; delegate `Stats` to cache metrics
    - Wire the Raft node into the same process so each cache node participates in leader election
    - _Requirements: 3.1, 3.6, 4.1_
  - [ ] 8.2 Implement gRPC `RaftService` server on each cache node
    - Expose `RaftService` on the same gRPC server; route `RequestVote` and `AppendEntries` to the `RaftNode` handlers from task 6.3
    - _Requirements: 4.1, 10.3_
  - [ ] 8.3 Write `cmd/cachenode/main.go` binary
    - Parse flags: `--id`, `--port`, `--peers`; load config from env (`EVICTION_POLICY`, `CACHE_CAPACITY`); start gRPC server serving both `CacheService` and `RaftService`; start HTTP server on metrics port for `/metrics` and `/health`
    - _Requirements: 3.1, 9.2, 11.3_

- [ ] 9. Checkpoint — cluster and Raft
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 10. PostgreSQL schema and database package
  - [ ] 10.1 Write SQL migration and `db` package
    - Write `internal/db/migrations/001_products.sql` — `products` table with `id TEXT PRIMARY KEY`, `name`, `description`, `price_usd NUMERIC(10,2)`, `category`, `created_at`, `updated_at`; add `idx_products_category` index
    - Write `internal/db/postgres.go` — `DB` struct wrapping a `pgx` connection pool; implement `GetProduct(ctx, id) (*Product, error)`, `ListProducts(ctx, page, pageSize int) ([]Product, int, error)`, `UpdateProduct(ctx, id string, req ProductUpdateRequest) (*Product, error)`
    - _Requirements: 8.1, 8.4_

- [ ] 11. API layer — product catalog handlers and cache-aside
  - [ ] 11.1 Define `Product` and related data models
    - Write `internal/api/model.go` — `Product`, `ProductUpdateRequest`, `ProductListResponse` structs with JSON and db tags as specified in the design
    - _Requirements: 5.1, 5.2, 5.3_
  - [ ] 11.2 Implement cache-aside middleware and HTTP handlers
    - Write `internal/api/middleware.go` — cache-aside middleware: check cache first; on miss fetch from DB, populate cache with TTL ≥ 1s, set `X-Cache: MISS`; on hit set `X-Cache: HIT`; fail request if header cannot be added
    - Write `internal/api/handler.go` — `GET /products/{id}` (404 on missing, 503 on DB unavailable), `GET /products` (pagination with `page`/`pageSize` defaults), `PUT /products/{id}` (400 on invalid body, 404 on missing product, dispatch to `InvalidationStrategy.OnWrite` after DB write)
    - Write `internal/api/server.go` — HTTP server setup; register routes; expose `/health` and `/metrics`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.6, 5.7, 5.8, 5.9, 5.10, 8.2, 8.3, 8.6, 8.7_
  - [ ]* 11.3 Write property test — Property 11: Cache-aside always checks cache before DB
    - **Property 11: Cache-aside always checks cache before DB**
    - Use a mock cache and mock DB; for any `GET /products/{id}` assert cache is queried before DB; assert DB is only called on cache miss
    - **Validates: Requirements 8.2**
  - [ ]* 11.4 Write property test — Property 12: Cache population on miss
    - **Property 12: Cache population on miss**
    - Generate product IDs that exist in mock DB but not in cache; after `GET /products/{id}` assert cache contains the key with TTL ≥ 1s
    - **Validates: Requirements 5.4, 8.3, 8.6**
  - [ ]* 11.5 Write property test — Property 13: X-Cache header reflects actual cache status
    - **Property 13: X-Cache header reflects actual cache status**
    - For any `GET /products/{id}`, assert response contains `X-Cache: HIT` iff served from cache, `X-Cache: MISS` iff served from DB; assert the two cases are mutually exclusive
    - **Validates: Requirements 5.6, 5.7**
  - [ ]* 11.6 Write property test — Property 18: PUT with invalid input returns 400 or 404
    - **Property 18: PUT with invalid input returns 400 or 404**
    - Use an invalid JSON generator for malformed bodies (assert 400); use a non-existent ID generator (assert 404)
    - **Validates: Requirements 5.10**
  - [ ] 11.7 Write `cmd/api/main.go` binary
    - Parse flags: `--port`; load config from env (`INVALIDATION_STRATEGY`, `CACHE_CLUSTER_ADDR`, `DB_DSN`); start HTTP server; expose `/health` and `/metrics`
    - _Requirements: 11.3, 11.4_

- [ ] 12. Cache invalidation strategies
  - [ ] 12.1 Implement TTL-based invalidation strategy
    - Write `internal/invalidation/ttl.go` — `TTLStrategy` whose `OnWrite` is a no-op; `Name()` returns `"ttl"`
    - _Requirements: 6.1_
  - [ ] 12.2 Implement write-through invalidation strategy
    - Write `internal/invalidation/writethrough.go` — `WriteThroughStrategy` whose `OnWrite` synchronously calls `cache.Set` with the new value; if cache update fails, log the error and return nil (DB write already committed; next read is a cache miss)
    - _Requirements: 6.2_
  - [ ]* 12.3 Write property test — Property 14: Write-through preserves DB write on cache failure
    - **Property 14: Write-through preserves DB write on cache failure**
    - Inject a failing mock cache; for any product update assert the DB always contains the updated value regardless of cache update outcome
    - **Validates: Requirements 6.2**
  - [ ] 12.4 Implement write-behind invalidation strategy
    - Write `internal/invalidation/writebehind.go` — `WriteBehindStrategy` whose `OnWrite` schedules async `cache.Delete` in a goroutine; async invalidation must complete within 5 seconds; on failure log error (entry expires via natural TTL)
    - _Requirements: 6.3_
  - [ ] 12.5 Verify all three strategies satisfy `InvalidationStrategy` interface (compile-time check)
    - Add `var _ shared.InvalidationStrategy` assertions for all three strategies in `internal/invalidation/invalidation_test.go`
    - _Requirements: 5.5_

- [ ] 13. Checkpoint — API layer and invalidation
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 14. Reverse proxy with HTTP response caching
  - [ ] 14.1 Implement proxy response store
    - Write `internal/proxy/store.go` — `CachedHTTPResponse` struct (`StatusCode`, `Headers`, `Body`, `CachedAt`, `ExpiresAt`); `ResponseStore` backed by `sync.RWMutex` map keyed by full URL including query string; implement `Get(url string)`, `Set(url string, resp CachedHTTPResponse)`, `Delete(url string)`, `Len() int`
    - _Requirements: 7.1_
  - [ ] 14.2 Implement reverse proxy with Cache-Control semantics
    - Write `internal/proxy/proxy.go` — HTTP handler that: checks `ResponseStore` for cached response (serve on hit without calling upstream); on miss forwards to API layer via gRPC, parses `Cache-Control` response header, stores response if not `no-store`/`no-cache`/`private` and `max-age > 0`; does not store if `max-age=0`; serves stale cached response on upstream timeout (>30s); returns 503 if no cached response available on timeout
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8_
  - [ ]* 14.3 Write property test — Property 15: Proxy caches full response keyed by URL
    - **Property 15: Proxy caches full response keyed by URL**
    - Use a URL generator and response generator; after first request assert second identical request is served from cache without calling upstream; assert body, status code, and headers match
    - **Validates: Requirements 7.1, 7.3**
  - [ ]* 14.4 Write property test — Property 16: Cache-Control no-store/no-cache/private responses are never cached
    - **Property 16: Cache-Control no-store/no-cache/private responses are never cached**
    - Use a `Cache-Control` directive generator; assert proxy never stores responses with `no-store`, `no-cache`, or `private`; assert subsequent requests always forward to upstream
    - **Validates: Requirements 7.6**
  - [ ]* 14.5 Write property test — Property 17: Cache-Control max-age drives proxy TTL
    - **Property 17: Cache-Control max-age drives proxy TTL**
    - Use `rapid.IntRange(1, 86400)` for N and a mock clock; assert cached response is served within N seconds; assert entry is treated as expired after N seconds
    - **Validates: Requirements 7.7**
  - [ ] 14.6 Write `cmd/proxy/main.go` binary
    - Parse flags: `--port`, `--upstream`; start HTTP server; expose `/health` and `/metrics`
    - _Requirements: 11.3, 11.4_

- [ ] 15. Prometheus metrics instrumentation
  - [ ] 15.1 Implement cache node metrics helpers
    - Write `internal/metrics/cache.go` — register `dcg_cache_hits_total`, `dcg_cache_misses_total`, `dcg_cache_evictions_total` (Counters), `dcg_cache_entries` (Gauge), `dcg_cache_routed_requests_total` (Counter), `dcg_cache_operation_duration_seconds` (Histogram); all labelled with `node_id` and `eviction_policy`
    - Wire metrics calls into `internal/cache/cache.go` `Get`/`Set`/`Delete` and into `internal/cluster/coordinator.go` routing
    - _Requirements: 9.1, 9.2_
  - [ ] 15.2 Implement API layer metrics helpers
    - Write `internal/metrics/api.go` — register `dcg_api_requests_total`, `dcg_api_request_duration_seconds`, `dcg_api_cache_hits_total`, `dcg_api_cache_misses_total`, `dcg_api_db_queries_total`, `dcg_api_invalidation_duration_seconds`; wire into `internal/api/middleware.go` and `internal/api/handler.go`
    - _Requirements: 9.3_
  - [ ] 15.3 Implement reverse proxy metrics helpers
    - Write `internal/metrics/proxy.go` — register `dcg_proxy_hits_total`, `dcg_proxy_misses_total`, `dcg_proxy_request_duration_seconds`, `dcg_proxy_cached_entries`, `dcg_proxy_upstream_errors_total`; wire into `internal/proxy/proxy.go`
    - _Requirements: 9.4_

- [ ] 16. Grafana dashboards and Prometheus configuration
  - [ ] 16.1 Write Prometheus scrape configuration
    - Write `observability/prometheus/prometheus.yml` — scrape configs for all services: cache-node-1 (:9101), cache-node-2 (:9102), cache-node-3 (:9103), api (:8181), proxy (:8180); set scrape interval to 5s
    - _Requirements: 9.5_
  - [ ] 16.2 Write Grafana provisioning files and dashboard JSON
    - Write `observability/grafana/datasources/prometheus.yml` — Prometheus datasource pointing to `http://prometheus:9090`
    - Write `observability/grafana/dashboards/cache-cluster.json` — panels for hit rate, miss rate, eviction count, entry count, routed requests per node
    - Write `observability/grafana/dashboards/eviction-benchmark.json` — panels comparing all four eviction policies on hit rate, miss rate, eviction count, mean latency, p99 latency (grouped by `eviction_policy` label)
    - Write `observability/grafana/dashboards/invalidation-benchmark.json` — panels comparing all three invalidation strategies on write latency, read latency, hit rate (grouped by `invalidation_strategy` label)
    - _Requirements: 9.6, 9.7, 9.8_

- [ ] 17. Benchmark runner
  - [ ] 17.1 Implement eviction policy benchmark
    - Write `internal/benchmark/eviction.go` — run the same workload (configurable key space, read/write ratio, total operations) against all four eviction policies sequentially; record hit rate, miss rate, eviction count, mean latency (ms), p99 latency (ms) per policy; handle policy errors by recording failure and continuing
    - _Requirements: 2.5, 2.6, 2.7_
  - [ ] 17.2 Implement invalidation strategy benchmark
    - Write `internal/benchmark/invalidation.go` — run the same workload against all three invalidation strategies; record write latency, read latency, cache hit rate per strategy; handle zero-operation strategies by recording failure and continuing
    - _Requirements: 6.4, 6.5_
  - [ ] 17.3 Implement benchmark report generator and `cmd/benchmark/main.go`
    - Write `internal/benchmark/report.go` — format results as a table with one row per policy/strategy and columns for all recorded metrics
    - Write `cmd/benchmark/main.go` — parse `--mode` flag (`eviction` or `invalidation`); run the appropriate benchmark; print report to stdout
    - _Requirements: 2.7, 6.5_

- [ ] 18. Docker Compose and multi-stage Dockerfile
  - [ ] 18.1 Write multi-stage Dockerfile
    - Write a multi-stage `Dockerfile` at the repository root: builder stage compiles all four binaries (`cachenode`, `api`, `proxy`, `benchmark`) from the single Go module; final stage copies binaries into a minimal image
    - _Requirements: 11.1_
  - [ ] 18.2 Write `docker-compose.yml`
    - Write `docker-compose.yml` defining all services: `cache-node-1` (port 9001/9101), `cache-node-2` (9002/9102), `cache-node-3` (9003/9103), `api` (8081/8181), `proxy` (8080/8180), `postgres` (5432), `prometheus` (9090), `grafana` (3000)
    - Configure `healthcheck` for each service; set `depends_on` chains; mount Grafana provisioning volumes; set `EVICTION_POLICY`, `CACHE_CAPACITY`, `INVALIDATION_STRATEGY`, `CACHE_CLUSTER_ADDR`, `DB_DSN` env vars
    - _Requirements: 11.3, 11.4_

- [ ] 19. Checkpoint — full stack wired
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 20. Conceptual documentation
  - [ ] 20.1 Write DNS caching documentation
    - Write `docs/dns-caching.md` covering: how TTL works in DNS responses, how recursive resolvers cache records, trade-offs of short vs long DNS TTLs, and how DNS caching differs from application-level caching
    - _Requirements: 12.1_
  - [ ] 20.2 Write load balancer caching documentation
    - Write `docs/loadbalancer-caching.md` covering: how layer-7 load balancers can cache responses, the difference between connection-level and response-level caching, and an explicit comparison to the reverse proxy layer in this sandbox
    - _Requirements: 12.2_
  - [ ] 20.3 Add layer status table to README
    - Add a section to `README.md` with a table assigning each layer (DNS, load balancer, reverse proxy, API, cache cluster, in-memory cache, database) an explicit status of "implemented as a running service" or "conceptual documentation only"
    - _Requirements: 12.3_

- [ ] 21. Integration tests and smoke tests
  - [ ] 21.1 Write smoke tests for interface compliance and structural requirements
    - Write compile-time assertions in `internal/eviction/eviction_test.go` and `internal/invalidation/invalidation_test.go` confirming all four eviction policies and all three invalidation strategies satisfy their respective interfaces
    - Write a test that verifies a single `go.mod` exists at the repository root and `docker-compose.yml` defines all required services
    - _Requirements: 1.4, 5.5, 11.1, 11.3_
  - [ ] 21.2 Write integration tests for Raft election and write-behind timing
    - Write `internal/raft/raft_test.go` integration test: start 3 in-process Raft nodes; kill the leader; assert a new leader is elected within 10 seconds
    - Write `internal/invalidation/invalidation_test.go` integration test: trigger write-behind invalidation; assert async cache deletion completes within 5 seconds
    - _Requirements: 4.2, 6.3_
  - [ ]* 21.3 Write integration test for concurrent cache access under race detector
    - Spawn 50 goroutines performing concurrent `Get`/`Set`/`Delete` on the same cache instance; run with `go test -race`; assert no data races are detected
    - _Requirements: 1.6_
  - [ ]* 21.4 Write smoke test for `docker compose up` health checks
    - Write a shell-based or Go test that runs `docker compose up -d`, polls each service's `/health` endpoint until all return HTTP 200 (or timeout after 60s), then runs `docker compose down`
    - _Requirements: 11.4_

- [ ] 22. Final checkpoint — all tests pass
  - Ensure all tests pass (`go test ./...` and `go test -race ./...`), ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for a faster MVP; all 18 correctness properties have corresponding PBT sub-tasks
- Each task references specific requirements for traceability
- Checkpoints at tasks 4, 9, 13, 19, and 22 ensure incremental validation
- Property-based tests use [`pgregory.net/rapid`](https://github.com/flyingmutant/rapid) — add it to `go.mod` before writing PBT tasks
- The `eviction_policy` and `invalidation_strategy` Prometheus labels enable all policies/strategies to be compared on the same Grafana panel simultaneously
- The design uses Go throughout — no pseudocode — so no language selection step is needed
- Build order: shared interfaces → cache core → eviction → cluster/hashing → Raft → proto/gRPC → cluster wiring → DB → API → invalidation → proxy → metrics → observability → benchmarks → docs → integration tests

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["2.1", "7.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "2.4", "2.5", "3.1", "3.3", "3.5", "3.7", "7.2"] },
    { "id": 3, "tasks": ["3.2", "3.4", "3.6", "3.8", "3.9", "5.1", "10.1"] },
    { "id": 4, "tasks": ["5.2", "5.3", "5.4", "6.1"] },
    { "id": 5, "tasks": ["6.2", "8.1"] },
    { "id": 6, "tasks": ["6.3", "8.2", "11.1"] },
    { "id": 7, "tasks": ["8.3", "11.2", "12.1", "12.2", "12.4"] },
    { "id": 8, "tasks": ["11.3", "11.4", "11.5", "11.6", "12.3", "12.5", "14.1"] },
    { "id": 9, "tasks": ["11.7", "14.2"] },
    { "id": 10, "tasks": ["14.3", "14.4", "14.5", "15.1", "15.2", "15.3"] },
    { "id": 11, "tasks": ["14.6", "16.1", "17.1", "17.2"] },
    { "id": 12, "tasks": ["16.2", "17.3", "18.1"] },
    { "id": 13, "tasks": ["18.2", "20.1", "20.2"] },
    { "id": 14, "tasks": ["20.3", "21.1", "21.2"] },
    { "id": 15, "tasks": ["21.3", "21.4"] }
  ]
}
```

# Design Document: distributed-caching-go

## Overview

This document describes the technical design for a locally runnable Go learning sandbox that demonstrates caching behavior at each layer of a distributed system. The sandbox is built as a single Go module with five real running services — in-memory cache, cache cluster (3 nodes), API layer, reverse proxy, and database layer — plus an observability stack (Prometheus + Grafana) and PostgreSQL as the source of truth. All services are orchestrated via Docker Compose.

The primary goal is empirical learning: a developer can observe, benchmark, and compare eviction policies and invalidation strategies by watching live Prometheus/Grafana metrics while the system runs under load.

### Key Design Decisions

- **Single Go module** (`go.mod` at root), packages under `internal/`
- **gRPC + Protobuf** for all internal communication; HTTP/REST for external (client → proxy → API)
- **Consistent hashing** for key distribution across 3 cache nodes
- **Raft** used exclusively for leader election (not data replication) — see ADR 0001
- **4 eviction policies** (LRU, LFU, TTL-only, Random) behind a common `EvictionPolicy` interface
- **3 invalidation strategies** (TTL, write-through, write-behind) behind a common `InvalidationStrategy` interface
- **Cache-aside** pattern at the database layer
- **Docker Compose** orchestrates all services including PostgreSQL, Prometheus, and Grafana


---

## Architecture

### System Architecture Diagram

```mermaid
graph TB
    Client["Client (curl / browser)"]

    subgraph Docker Compose Network
        Proxy["Reverse Proxy\n:8080\nHTTP response cache\n/metrics /health"]

        subgraph API Layer
            API["API Layer\n:8081\nGET/PUT /products\n/metrics /health"]
        end

        subgraph Cache Cluster
            CC["Coordinator\n(Raft-elected leader)"]
            N1["Cache Node 1\n:9001\n/metrics /health"]
            N2["Cache Node 2\n:9002\n/metrics /health"]
            N3["Cache Node 3\n:9003\n/metrics /health"]
            CC -->|gRPC route| N1
            CC -->|gRPC route| N2
            CC -->|gRPC route| N3
            N1 <-->|Raft heartbeat| N2
            N2 <-->|Raft heartbeat| N3
            N1 <-->|Raft heartbeat| N3
        end

        PG["PostgreSQL\n:5432"]

        subgraph Observability
            Prom["Prometheus\n:9090"]
            Grafana["Grafana\n:3000"]
            Prom --> Grafana
        end
    end

    Client -->|HTTP/REST| Proxy
    Proxy -->|gRPC| API
    API -->|gRPC| CC
    API -->|SQL| PG

    Prom -->|scrape /metrics| Proxy
    Prom -->|scrape /metrics| API
    Prom -->|scrape /metrics| N1
    Prom -->|scrape /metrics| N2
    Prom -->|scrape /metrics| N3
```

### Request Flow Summary

| Path | Protocol | Description |
|------|----------|-------------|
| Client → Reverse Proxy | HTTP/REST | External-facing; proxy caches full HTTP responses |
| Reverse Proxy → API Layer | gRPC | Internal; proxy forwards cache misses upstream |
| API Layer → Cache Cluster | gRPC | Cache-aside reads/writes via Coordinator |
| Coordinator → Cache Nodes | gRPC | Key routing per consistent hashing ring |
| Cache Nodes ↔ Cache Nodes | gRPC | Raft heartbeats and vote RPCs |
| API Layer → PostgreSQL | SQL (pgx) | Source-of-truth reads/writes |
| Prometheus → All services | HTTP scrape | `/metrics` endpoint on every service |


---

## Components and Interfaces

### Component Responsibilities

| Component | Package | Responsibility |
|-----------|---------|----------------|
| Cache Core | `internal/cache` | In-memory hash map with TTL, eviction policy dispatch, concurrent access |
| Eviction Policies | `internal/eviction` | LRU, LFU, TTL-only, Random — all implement `EvictionPolicy` |
| Cache Cluster | `internal/cluster` | Consistent hashing ring, node health tracking, request routing |
| Raft | `internal/raft` | Leader election state machine (follower/candidate/leader), vote RPCs |
| API Layer | `internal/api` | HTTP handlers for product catalog, cache-aside logic, invalidation dispatch |
| Invalidation Strategies | `internal/invalidation` | TTL, write-through, write-behind — all implement `InvalidationStrategy` |
| Reverse Proxy | `internal/proxy` | HTTP response caching keyed by URL, Cache-Control header semantics |
| gRPC Services | `internal/proto` | Generated Go bindings from `.proto` files |
| Shared Types | `internal/shared` | `EvictionPolicy` interface, `InvalidationStrategy` interface, config, metrics helpers |
| Benchmark | `internal/benchmark` | Workload runner, metrics collector, report generator |
| Observability | `internal/metrics` | Prometheus counter/histogram registration helpers |

### Key Interfaces

```go
// internal/shared/eviction.go

// EvictionPolicy determines which entry is removed when the cache is full.
// All four policies (LRU, LFU, TTL-only, Random) implement this interface.
type EvictionPolicy interface {
    // OnAccess is called on every Get or Set for an existing key.
    OnAccess(key string)
    // OnInsert is called when a new key is inserted.
    OnInsert(key string, expiresAt time.Time)
    // OnEvict is called when an entry is explicitly removed (e.g. TTL expiry).
    OnEvict(key string)
    // Evict selects and returns the key that should be removed.
    // The cache calls this when it is at capacity before inserting a new entry.
    Evict() (key string, ok bool)
    // Name returns the policy name for metrics labelling.
    Name() string
}
```

```go
// internal/shared/invalidation.go

// InvalidationStrategy determines how the cache is kept consistent after a write.
// All three strategies (TTL, write-through, write-behind) implement this interface.
type InvalidationStrategy interface {
    // OnWrite is called after a successful write to the source of truth.
    // It receives the cache instance, the affected key, and the new value.
    // For TTL: no-op. For write-through: synchronous cache update.
    // For write-behind: schedules async invalidation.
    OnWrite(ctx context.Context, cache Cache, key string, value []byte) error
    // Name returns the strategy name for metrics labelling.
    Name() string
}
```

```go
// internal/shared/cache.go

// Cache is the interface exposed by the in-memory cache to consumers.
// Both the standalone cache and the cluster-backed cache implement this.
type Cache interface {
    Get(ctx context.Context, key string) (value []byte, found bool, err error)
    Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    Len() int
}
```


---

## Package Structure

```
distributed-caching-go/
├── go.mod                          # Single module root
├── go.sum
├── docker-compose.yml              # Orchestrates all services
├── Makefile                        # proto gen, build, test, bench targets
│
├── cmd/
│   ├── cachenode/main.go           # Cache node binary (3 instances via compose)
│   ├── api/main.go                 # API layer binary
│   ├── proxy/main.go               # Reverse proxy binary
│   └── benchmark/main.go          # Benchmark runner binary
│
├── internal/
│   ├── shared/                     # Shared interfaces and types
│   │   ├── eviction.go             # EvictionPolicy interface
│   │   ├── invalidation.go         # InvalidationStrategy interface
│   │   ├── cache.go                # Cache interface
│   │   └── config.go               # Config loading (env vars + YAML)
│   │
│   ├── cache/                      # In-memory cache core
│   │   ├── cache.go                # Core hash map + TTL + eviction dispatch
│   │   └── cache_test.go
│   │
│   ├── eviction/                   # Eviction policy implementations
│   │   ├── lru.go                  # LRU (doubly-linked list + map)
│   │   ├── lfu.go                  # LFU (min-heap + frequency map)
│   │   ├── ttl.go                  # TTL-only (min-heap by expiry time)
│   │   ├── random.go               # Random (reservoir sampling)
│   │   └── eviction_test.go
│   │
│   ├── cluster/                    # Cache cluster and consistent hashing
│   │   ├── ring.go                 # Consistent hashing ring
│   │   ├── coordinator.go          # Coordinator routing logic
│   │   ├── node.go                 # Node health tracking
│   │   └── cluster_test.go
│   │
│   ├── raft/                       # Raft leader election
│   │   ├── state.go                # State machine (follower/candidate/leader)
│   │   ├── rpc.go                  # RequestVote and AppendEntries (heartbeat only)
│   │   ├── timer.go                # Election timeout management
│   │   └── raft_test.go
│   │
│   ├── api/                        # Product catalog API layer
│   │   ├── handler.go              # HTTP handlers (GET/PUT /products)
│   │   ├── middleware.go           # Cache-aside middleware
│   │   ├── server.go               # HTTP server setup
│   │   └── handler_test.go
│   │
│   ├── invalidation/               # Invalidation strategy implementations
│   │   ├── ttl.go                  # TTL-based (no-op on write)
│   │   ├── writethrough.go         # Write-through (sync cache update)
│   │   ├── writebehind.go          # Write-behind (async invalidation)
│   │   └── invalidation_test.go
│   │
│   ├── proxy/                      # Reverse proxy
│   │   ├── proxy.go                # HTTP response caching + Cache-Control
│   │   ├── store.go                # Response store (URL → CachedResponse)
│   │   └── proxy_test.go
│   │
│   ├── metrics/                    # Prometheus metrics helpers
│   │   ├── cache.go                # Cache hit/miss/eviction counters
│   │   ├── api.go                  # API request latency histograms
│   │   └── proxy.go                # Proxy hit/miss/latency metrics
│   │
│   ├── benchmark/                  # Benchmark runner
│   │   ├── eviction.go             # Eviction policy benchmark
│   │   ├── invalidation.go         # Invalidation strategy benchmark
│   │   └── report.go               # Report generation
│   │
│   └── db/                         # Database layer
│       ├── postgres.go             # pgx connection pool + queries
│       └── migrations/             # SQL migration files
│           └── 001_products.sql
│
├── proto/
│   ├── cache/v1/cache.proto        # CacheService (Get, Set, Delete, Health)
│   ├── raft/v1/raft.proto          # RaftService (RequestVote, AppendEntries)
│   └── api/v1/api.proto            # APIService (GetProduct, ListProducts, UpdateProduct)
│
├── observability/
│   ├── prometheus/
│   │   └── prometheus.yml          # Scrape config for all services
│   └── grafana/
│       ├── datasources/
│       │   └── prometheus.yml
│       └── dashboards/
│           ├── cache-cluster.json
│           ├── eviction-benchmark.json
│           └── invalidation-benchmark.json
│
└── docs/
    ├── adr/
    │   └── 0001-raft-for-leader-election-only.md
    ├── dns-caching.md
    └── loadbalancer-caching.md
```


---

## Data Models

### Product (domain model)

```go
// internal/api/model.go

type Product struct {
    ID          string    `json:"id" db:"id"`
    Name        string    `json:"name" db:"name"`
    Description string    `json:"description" db:"description"`
    PriceUSD    float64   `json:"price_usd" db:"price_usd"`
    Category    string    `json:"category" db:"category"`
    CreatedAt   time.Time `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type ProductUpdateRequest struct {
    Name        *string  `json:"name"`
    Description *string  `json:"description"`
    PriceUSD    *float64 `json:"price_usd"`
    Category    *string  `json:"category"`
}

type ProductListResponse struct {
    Products   []Product `json:"products"`
    Page       int       `json:"page"`
    PageSize   int       `json:"page_size"`
    TotalCount int       `json:"total_count"`
}
```

### CacheEntry (internal cache model)

```go
// internal/cache/entry.go

type CacheEntry struct {
    Key       string
    Value     []byte        // JSON-encoded payload
    ExpiresAt time.Time     // absolute expiry; zero means no expiry
    CreatedAt time.Time
    AccessedAt time.Time
    AccessCount int64       // used by LFU
}

// IsExpired returns true if the entry has passed its TTL.
func (e *CacheEntry) IsExpired(now time.Time) bool {
    return !e.ExpiresAt.IsZero() && now.After(e.ExpiresAt)
}
```

### CachedHTTPResponse (reverse proxy model)

```go
// internal/proxy/store.go

type CachedHTTPResponse struct {
    StatusCode int
    Headers    http.Header
    Body       []byte
    CachedAt   time.Time
    ExpiresAt  time.Time   // derived from Cache-Control max-age
}
```

### PostgreSQL Schema

```sql
-- internal/db/migrations/001_products.sql

CREATE TABLE products (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_usd   NUMERIC(10, 2) NOT NULL,
    category    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_products_category ON products(category);
```


---

## gRPC Service Definitions

### proto/cache/v1/cache.proto

```protobuf
syntax = "proto3";
package cache.v1;
option go_package = "github.com/user/distributed-caching-go/internal/proto/cache/v1";

// CacheService is implemented by each cache node.
// The Coordinator routes requests to the correct node via this service.
service CacheService {
    rpc Get(GetRequest) returns (GetResponse);
    rpc Set(SetRequest) returns (SetResponse);
    rpc Delete(DeleteRequest) returns (DeleteResponse);
    rpc Health(HealthRequest) returns (HealthResponse);
    rpc Stats(StatsRequest) returns (StatsResponse);
}

message GetRequest  { string key = 1; }
message GetResponse { bytes value = 1; bool found = 2; }

message SetRequest  { string key = 1; bytes value = 2; int64 ttl_seconds = 3; }
message SetResponse { bool ok = 1; }

message DeleteRequest  { string key = 1; }
message DeleteResponse { bool ok = 1; }

message HealthRequest  {}
message HealthResponse { bool healthy = 1; string node_id = 2; bool is_coordinator = 3; }

message StatsRequest  {}
message StatsResponse {
    int64 hit_count      = 1;
    int64 miss_count     = 2;
    int64 eviction_count = 3;
    int64 entry_count    = 4;
    string eviction_policy = 5;
}
```

### proto/raft/v1/raft.proto

```protobuf
syntax = "proto3";
package raft.v1;
option go_package = "github.com/user/distributed-caching-go/internal/proto/raft/v1";

// RaftService handles leader election RPCs between cache nodes.
// AppendEntries is used only for heartbeats (no log replication).
service RaftService {
    rpc RequestVote(RequestVoteRequest) returns (RequestVoteResponse);
    rpc AppendEntries(AppendEntriesRequest) returns (AppendEntriesResponse);
}

message RequestVoteRequest {
    int64  term           = 1;
    string candidate_id   = 2;
    int64  last_log_index = 3;  // always 0 (no log)
    int64  last_log_term  = 4;  // always 0 (no log)
}
message RequestVoteResponse {
    int64 term         = 1;
    bool  vote_granted = 2;
}

message AppendEntriesRequest {
    int64  term        = 1;
    string leader_id   = 2;
    // No log entries — used purely as heartbeat
}
message AppendEntriesResponse {
    int64 term    = 1;
    bool  success = 2;
}
```

### proto/api/v1/api.proto

```protobuf
syntax = "proto3";
package api.v1;
option go_package = "github.com/user/distributed-caching-go/internal/proto/api/v1";

// APIService is called by the Reverse Proxy to forward requests to the API Layer.
service APIService {
    rpc GetProduct(GetProductRequest) returns (GetProductResponse);
    rpc ListProducts(ListProductsRequest) returns (ListProductsResponse);
    rpc UpdateProduct(UpdateProductRequest) returns (UpdateProductResponse);
}

message GetProductRequest  { string id = 1; }
message GetProductResponse { bytes product_json = 1; bool found = 2; string cache_status = 3; }

message ListProductsRequest  { int32 page = 1; int32 page_size = 2; }
message ListProductsResponse { bytes products_json = 1; int32 total_count = 2; }

message UpdateProductRequest  { string id = 1; bytes update_json = 2; }
message UpdateProductResponse { bytes product_json = 1; bool ok = 2; }
```


---

## Consistent Hashing Ring Design

### Overview

The consistent hashing ring maps keys to cache nodes by hashing both keys and node identifiers onto a circular integer space (0 to 2^32-1). Each physical node is represented by multiple virtual nodes (tokens) to improve key distribution uniformity.

### Ring Structure

```go
// internal/cluster/ring.go

const DefaultVirtualNodes = 150  // virtual nodes per physical node

type Ring struct {
    mu           sync.RWMutex
    nodes        map[uint32]string   // token → nodeID
    sortedTokens []uint32            // sorted for binary search
    nodeAddrs    map[string]string   // nodeID → gRPC address
}

// Add inserts a node with its virtual tokens.
func (r *Ring) Add(nodeID, addr string) { ... }

// Remove removes a node and all its virtual tokens.
func (r *Ring) Remove(nodeID string) { ... }

// Get returns the nodeID responsible for the given key.
// Uses clockwise lookup: find the first token >= hash(key).
func (r *Ring) Get(key string) (nodeID string, ok bool) { ... }

// hash computes a uint32 token for a string using FNV-1a.
func hash(s string) uint32 { ... }
```

### Token Assignment

When a node is added, 150 virtual tokens are generated by hashing `"nodeID-0"` through `"nodeID-149"`. These tokens are inserted into the sorted token array. Key lookup uses `sort.Search` for O(log n) binary search.

### Minimal Remapping Property

When node N is removed, only keys whose tokens fall in the arc previously owned by N are remapped to N's successor. All other keys retain their assignments. This is the core property verified by Property 3 (see Correctness Properties).

### Node Health Integration

The ring is authoritative for key ownership. Node health is tracked separately in `coordinator.go`. When a node is marked unavailable, the coordinator does not reroute its keys to another node — instead, requests for those keys return a cache miss, allowing fall-through to the origin. This is a deliberate design choice that demonstrates why replication exists (see ADR 0001).


---

## Raft Leader Election State Machine

### States and Transitions

```mermaid
stateDiagram-v2
    [*] --> Follower : startup

    Follower --> Candidate : election timeout fires\n(no heartbeat received)
    Candidate --> Leader : receives votes from\nmajority (≥2 of 3)
    Candidate --> Follower : receives AppendEntries\nfrom valid leader\nOR higher term seen
    Candidate --> Candidate : election timeout fires\n(split vote — restart election)
    Leader --> Follower : higher term seen\nin any RPC response
```

### Timing Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Election timeout | 150–300ms (randomised) | Randomisation prevents split votes |
| Heartbeat interval | 50ms | Must be << election timeout |
| Max election time | 10s (requirement 4.2) | Worst case: multiple split votes |

### Implementation Notes

- **No log replication**: `AppendEntries` carries only `term` and `leader_id`. There are no log entries, commit indices, or snapshots.
- **Persistent state**: `currentTerm` and `votedFor` are persisted to disk (a simple JSON file per node) so a restarted node does not vote twice in the same term.
- **Majority quorum**: With 3 nodes, a leader needs 2 votes (including self-vote). A single node failure still allows election.
- **Coordinator announcement**: When a node transitions to Leader, it broadcasts its identity via `AppendEntries` heartbeats. The coordinator's gRPC address is registered in the ring so the API layer can route to it.

### State Machine Fields

```go
// internal/raft/state.go

type RaftNode struct {
    mu          sync.Mutex
    id          string
    state       NodeState       // Follower | Candidate | Leader
    currentTerm int64
    votedFor    string          // empty = not voted this term
    leaderID    string
    peers       []PeerConfig    // other nodes' gRPC addresses
    electionTimer *time.Timer
    stopCh      chan struct{}
}

type NodeState int
const (
    Follower  NodeState = iota
    Candidate
    Leader
)
```


---

## Sequence Diagrams

### Cache Hit

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Reverse Proxy
    participant A as API Layer
    participant Co as Coordinator
    participant N as Cache Node

    C->>P: GET /products/123 (HTTP)
    P->>P: Check proxy response cache
    P-->>C: 200 OK (cached response, X-Cache: HIT from proxy)

    Note over P: Proxy serves from its own cache.<br/>API Layer is never called.
```

### Cache Miss (full path)

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Reverse Proxy
    participant A as API Layer
    participant Co as Coordinator
    participant N as Cache Node
    participant DB as PostgreSQL

    C->>P: GET /products/123 (HTTP)
    P->>P: Check proxy cache — MISS
    P->>A: GetProduct(id=123) (gRPC)
    A->>Co: Get(key="product:123") (gRPC)
    Co->>Co: hash("product:123") → Node 2
    Co->>N: Get(key="product:123") (gRPC)
    N-->>Co: {found: false}
    Co-->>A: {found: false}
    A->>DB: SELECT * FROM products WHERE id='123'
    DB-->>A: product row
    A->>Co: Set(key="product:123", value=..., ttl=300) (gRPC)
    Co->>N: Set(...) (gRPC)
    N-->>Co: {ok: true}
    A-->>P: GetProductResponse{product_json, cache_status="MISS"}
    P->>P: Cache response for /products/123
    P-->>C: 200 OK (X-Cache: MISS)
```

### Node Failure (cache miss fallthrough)

```mermaid
sequenceDiagram
    participant A as API Layer
    participant Co as Coordinator
    participant N2 as Cache Node 2 (dead)
    participant DB as PostgreSQL

    A->>Co: Get(key="product:456") (gRPC)
    Co->>Co: hash("product:456") → Node 2
    Co->>N2: Health probe (gRPC, 500ms timeout)
    N2--xCo: No response (timeout)
    Co->>Co: Mark Node 2 unavailable
    Co-->>A: {found: false}  ← treated as cache miss
    A->>DB: SELECT * FROM products WHERE id='456'
    DB-->>A: product row
    Note over A: Does NOT populate cache<br/>(Node 2 is unavailable)
    A-->>A: Return product with X-Cache: MISS
```

### Write-Through Invalidation

```mermaid
sequenceDiagram
    participant C as Client
    participant P as Reverse Proxy
    participant A as API Layer
    participant Co as Coordinator
    participant N as Cache Node
    participant DB as PostgreSQL

    C->>P: PUT /products/123 (HTTP, body: {price_usd: 29.99})
    P->>A: UpdateProduct(id=123, update_json=...) (gRPC)
    A->>DB: UPDATE products SET price_usd=29.99 WHERE id='123'
    DB-->>A: updated row
    A->>A: InvalidationStrategy.OnWrite(cache, "product:123", newValue)
    Note over A: write-through: synchronous cache update
    A->>Co: Set(key="product:123", value=newValue, ttl=300) (gRPC)
    Co->>N: Set(...) (gRPC)
    N-->>Co: {ok: true}
    Co-->>A: {ok: true}
    A-->>P: UpdateProductResponse{product_json, ok: true}
    P->>P: Invalidate proxy cache for /products/123
    P-->>C: 200 OK (updated product)
```


---

## Docker Compose Service Topology

```yaml
# docker-compose.yml (structure — not exhaustive)

services:
  cache-node-1:
    build: .
    command: ["/app/cachenode", "--id=node1", "--port=9001", "--peers=node2:9002,node3:9003"]
    ports: ["9001:9001", "9101:9101"]   # 9101 = /metrics HTTP port
    environment:
      EVICTION_POLICY: lru
      CACHE_CAPACITY: "10000"
    healthcheck:
      test: ["CMD", "grpc_health_probe", "-addr=:9001"]
      interval: 5s
      timeout: 2s

  cache-node-2:
    # same as node-1 with --id=node2 --port=9002
    environment:
      EVICTION_POLICY: lru

  cache-node-3:
    # same as node-1 with --id=node3 --port=9003
    environment:
      EVICTION_POLICY: lru

  api:
    build: .
    command: ["/app/api", "--port=8081"]
    ports: ["8081:8081"]
    environment:
      INVALIDATION_STRATEGY: write-through
      CACHE_CLUSTER_ADDR: "cache-node-1:9001,cache-node-2:9002,cache-node-3:9003"
      DB_DSN: "postgres://user:pass@postgres:5432/products?sslmode=disable"
    depends_on: [cache-node-1, cache-node-2, cache-node-3, postgres]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/health"]

  proxy:
    build: .
    command: ["/app/proxy", "--port=8080", "--upstream=api:8081"]
    ports: ["8080:8080"]
    depends_on: [api]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: products
      POSTGRES_USER: user
      POSTGRES_PASSWORD: pass
    ports: ["5432:5432"]
    volumes: ["pgdata:/var/lib/postgresql/data"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U user -d products"]

  prometheus:
    image: prom/prometheus:v2.51.0
    volumes:
      - ./observability/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
    ports: ["9090:9090"]
    depends_on: [cache-node-1, cache-node-2, cache-node-3, api, proxy]

  grafana:
    image: grafana/grafana:10.4.0
    ports: ["3000:3000"]
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
    volumes:
      - ./observability/grafana/datasources:/etc/grafana/provisioning/datasources
      - ./observability/grafana/dashboards:/etc/grafana/provisioning/dashboards
    depends_on: [prometheus]

volumes:
  pgdata:
```

### Service Port Map

| Service | gRPC Port | HTTP Port | Metrics Port |
|---------|-----------|-----------|--------------|
| cache-node-1 | 9001 | — | 9101 |
| cache-node-2 | 9002 | — | 9102 |
| cache-node-3 | 9003 | — | 9103 |
| api | 8081 (gRPC) | 8081 (/health) | 8181 |
| proxy | — | 8080 | 8180 |
| postgres | — | 5432 | — |
| prometheus | — | 9090 | — |
| grafana | — | 3000 | — |


---

## Prometheus Metrics Design

All metrics use the `dcg_` prefix (distributed-caching-go). Labels are used to differentiate policies, strategies, nodes, and endpoints so all variants can be compared on the same Grafana panel.

### Cache Node Metrics

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `dcg_cache_hits_total` | Counter | `node_id`, `eviction_policy` | Total cache hits |
| `dcg_cache_misses_total` | Counter | `node_id`, `eviction_policy` | Total cache misses |
| `dcg_cache_evictions_total` | Counter | `node_id`, `eviction_policy` | Total evictions |
| `dcg_cache_entries` | Gauge | `node_id`, `eviction_policy` | Current entry count |
| `dcg_cache_routed_requests_total` | Counter | `node_id` | Requests routed to this node by coordinator |
| `dcg_cache_operation_duration_seconds` | Histogram | `node_id`, `operation` (get/set/delete) | Operation latency |

### API Layer Metrics

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `dcg_api_requests_total` | Counter | `endpoint`, `method`, `status_code` | Total HTTP requests |
| `dcg_api_request_duration_seconds` | Histogram | `endpoint`, `method` | Request latency (p99 derivable) |
| `dcg_api_cache_hits_total` | Counter | `endpoint`, `invalidation_strategy` | Cache hits at API layer |
| `dcg_api_cache_misses_total` | Counter | `endpoint`, `invalidation_strategy` | Cache misses at API layer |
| `dcg_api_db_queries_total` | Counter | `query_type` | DB queries issued |
| `dcg_api_invalidation_duration_seconds` | Histogram | `strategy` | Invalidation operation latency |

### Reverse Proxy Metrics

| Metric Name | Type | Labels | Description |
|-------------|------|--------|-------------|
| `dcg_proxy_hits_total` | Counter | — | Proxy cache hits |
| `dcg_proxy_misses_total` | Counter | — | Proxy cache misses |
| `dcg_proxy_request_duration_seconds` | Histogram | `status_code` | Proxied request latency (p99 derivable) |
| `dcg_proxy_cached_entries` | Gauge | — | Current cached response count |
| `dcg_proxy_upstream_errors_total` | Counter | `error_type` | Upstream errors (timeout, 5xx) |

### Benchmark Labels

During benchmark runs, the `eviction_policy` and `invalidation_strategy` labels are set to the active policy/strategy name. This allows Grafana to display all four eviction policies (or all three invalidation strategies) on the same panel simultaneously using label selectors.

Example Prometheus query for hit rate by eviction policy:
```
rate(dcg_cache_hits_total[1m]) / (rate(dcg_cache_hits_total[1m]) + rate(dcg_cache_misses_total[1m]))
```
Grouped by `eviction_policy` label.


---

## Correctness Properties

*A property is a characteristic or behavior that should hold true across all valid executions of a system — essentially, a formal statement about what the system should do. Properties serve as the bridge between human-readable specifications and machine-verifiable correctness guarantees.*

PBT is applicable here because the core components (cache, eviction policies, consistent hashing ring, invalidation strategies, proxy Cache-Control logic) are pure or near-pure functions with clear input/output behavior and large input spaces where varied inputs reveal edge cases. Infrastructure concerns (Raft timing, Docker health checks, Prometheus scraping) are covered by integration and smoke tests instead.

**Property-based testing library**: [`pgregory.net/rapid`](https://github.com/flyingmutant/rapid) — a Go PBT library with built-in generators and shrinking.

---

### Property 1: TTL expiry makes entries absent

*For any* key-value pair inserted with a TTL in [1, 86400] seconds, after the TTL has elapsed, a Get on that key SHALL return `(nil, false)`.

**Validates: Requirements 1.1, 1.2, 1.5**

---

### Property 2: Cache capacity is never exceeded

*For any* cache with a configured maximum capacity N, after any sequence of Set operations, the number of entries in the cache SHALL never exceed N.

**Validates: Requirements 1.3**

---

### Property 3: Upsert replaces value and resets TTL

*For any* key already present in the cache, performing a second Set with a different value and TTL SHALL result in Get returning the new value, and the entry's TTL SHALL reflect the new expiry (not the original).

**Validates: Requirements 1.7**

---

### Property 4: Get on absent key always returns miss

*For any* key that has never been inserted, or whose TTL has expired, Get SHALL return `(nil, false)` and SHALL never return `true`.

**Validates: Requirements 1.5**

---

### Property 5: LRU evicts the least recently accessed entry

*For any* full cache and any sequence of Get/Set accesses, when a new entry is inserted, the evicted key SHALL be the one with the oldest last-access timestamp among all entries present before the insertion.

**Validates: Requirements 2.1**

---

### Property 6: LFU evicts the lowest-frequency entry (tie-broken by timestamp)

*For any* full cache with known access frequency counts, when a new entry is inserted, the evicted key SHALL be the one with the minimum access count; if multiple keys share the minimum count, the one with the earliest last-access timestamp SHALL be evicted.

**Validates: Requirements 2.2**

---

### Property 7: TTL-only evicts the entry with the earliest expiry

*For any* full cache, when a new entry is inserted, the evicted key SHALL be the one whose `ExpiresAt` is earliest among all entries.

**Validates: Requirements 2.3**

---

### Property 8: Random eviction always removes an existing entry

*For any* full cache of size N, after inserting one new entry, the cache SHALL contain exactly N entries, and the evicted key SHALL have been one of the N entries present before the insertion (not the newly inserted key, and not a non-existent key).

**Validates: Requirements 2.4**

---

### Property 9: Consistent hashing is deterministic

*For any* key and any ring configuration, calling `ring.Get(key)` multiple times SHALL always return the same node ID.

**Validates: Requirements 3.2**

---

### Property 10: Consistent hashing minimises remapping on node removal

*For any* set of keys distributed across a 3-node ring, when one node is removed, only the keys previously assigned to that node SHALL change their assignment; all other keys SHALL map to the same node as before.

**Validates: Requirements 3.3**

---

### Property 11: Cache-aside always checks cache before DB

*For any* GET /products/{id} request, the cache SHALL be queried before the database; a database query SHALL only be issued when the cache returns a miss.

**Validates: Requirements 8.2**

---

### Property 12: Cache population on miss

*For any* product ID that exists in the database and is not in the cache, after a successful GET /products/{id}, the cache SHALL contain an entry for that key with TTL ≥ 1 second.

**Validates: Requirements 5.4, 8.3, 8.6**

---

### Property 13: X-Cache header reflects actual cache status

*For any* GET /products/{id} request, if the response was served from cache the response SHALL contain `X-Cache: HIT`; if it was served from the database the response SHALL contain `X-Cache: MISS`. These two cases are mutually exclusive.

**Validates: Requirements 5.6, 5.7**

---

### Property 14: Write-through preserves DB write on cache failure

*For any* product update using the write-through strategy, the database SHALL always contain the updated value regardless of whether the cache update succeeds or fails.

**Validates: Requirements 6.2**

---

### Property 15: Proxy caches full response keyed by URL

*For any* URL, after the first request is served from upstream, a second identical request SHALL be served from the proxy cache without calling upstream, and the response body, status code, and headers SHALL match the original upstream response.

**Validates: Requirements 7.1, 7.3**

---

### Property 16: Cache-Control no-store/no-cache/private responses are never cached

*For any* upstream response containing a `Cache-Control: no-store`, `no-cache`, or `private` directive, the proxy SHALL NOT store the response; subsequent requests for the same URL SHALL always be forwarded to upstream.

**Validates: Requirements 7.6**

---

### Property 17: Cache-Control max-age drives proxy TTL

*For any* upstream response with `Cache-Control: max-age=N` where N > 0, the proxy SHALL serve the cached response for requests within N seconds of caching, and SHALL treat the entry as expired for requests after N seconds.

**Validates: Requirements 7.7**

---

### Property 18: PUT with invalid input returns 400 or 404

*For any* PUT /products/{id} request with a malformed JSON body, the API SHALL return 400. *For any* PUT /products/{id} request where the product ID does not exist in the database, the API SHALL return 404.

**Validates: Requirements 5.10**


---

## Error Handling

### Cache Layer

| Condition | Behavior |
|-----------|----------|
| Key not found / expired | Return `(nil, false, nil)` — never an error |
| Cache at capacity | Evict one entry, then insert — never return capacity error to caller |
| Concurrent write conflict | Resolved by `sync.RWMutex` — no error surfaced |
| Invalid TTL (0 or negative) | TTL=0 treated as immediately expired; negative TTL rejected with error at Set time |

### Cache Cluster / Coordinator

| Condition | Behavior |
|-----------|----------|
| Target node unavailable (health probe timeout > 500ms) | Return cache miss to caller; do not reroute to another node |
| No coordinator elected | Return explicit error: `ErrNoCoordinator` |
| Routing mismatch (key sent to wrong node) | Return explicit error: `ErrRoutingMismatch` |
| gRPC call timeout | Treat as node unavailable; return cache miss |

### API Layer

| Condition | Behavior |
|-----------|----------|
| Product not found in DB | Return HTTP 404 |
| DB unavailable on cache miss | Return HTTP 503; do not populate cache |
| Invalid PUT request body | Return HTTP 400 |
| Cache update fails (write-through) | Log error, continue — DB write already committed; next read is a cache miss |
| Write-behind async invalidation fails | Log error; entry expires via natural TTL |
| X-Cache header cannot be added | Fail the request (per requirement 5.6) |

### Reverse Proxy

| Condition | Behavior |
|-----------|----------|
| Upstream timeout (> 30s) | Serve stale cached response if available; otherwise return 503 |
| `Cache-Control: no-store/no-cache/private` | Do not cache; forward every request |
| `Cache-Control: max-age=0` | Forward request; do not store response |
| Upstream returns 5xx | Do not cache the error response; return it to caller |

### Raft

| Condition | Behavior |
|-----------|----------|
| Split vote | Restart election with new randomised timeout |
| Node rejoins after failure | Resets to Follower state; participates in next election |
| Persistent state file missing on restart | Treat as fresh start (term=0, votedFor="") |


---

## Testing Strategy

### Dual Testing Approach

Unit tests cover specific examples, edge cases, and error conditions. Property-based tests verify universal properties across many generated inputs. Both are necessary: unit tests catch concrete bugs, property tests verify general correctness.

### Property-Based Testing

**Library**: [`pgregory.net/rapid`](https://github.com/flyingmutant/rapid)

Each property from the Correctness Properties section is implemented as a single `rapid.Check` test. Minimum 100 iterations per property (rapid's default is 100; increase to 1000 for critical properties like eviction correctness).

Each test is tagged with a comment referencing the design property:

```go
// Feature: distributed-caching-go, Property 5: LRU evicts the least recently accessed entry
func TestLRUEvictsLeastRecentlyAccessed(t *testing.T) {
    rapid.Check(t, func(t *rapid.T) {
        capacity := rapid.IntRange(2, 100).Draw(t, "capacity")
        // ... generate access sequence, verify eviction
    })
}
```

**Properties to implement as PBT** (from Correctness Properties section):

| Property | Package | Key Generator |
|----------|---------|---------------|
| 1 — TTL expiry | `internal/cache` | `rapid.StringN`, `rapid.IntRange(1, 86400)` |
| 2 — Capacity never exceeded | `internal/cache` | `rapid.IntRange(1, 1000)`, `rapid.SliceOf` |
| 3 — Upsert replaces value | `internal/cache` | `rapid.String`, `rapid.Bytes` |
| 4 — Absent key always miss | `internal/cache` | `rapid.String` |
| 5 — LRU eviction | `internal/eviction` | `rapid.IntRange`, access sequence |
| 6 — LFU eviction | `internal/eviction` | frequency map generator |
| 7 — TTL-only eviction | `internal/eviction` | TTL distribution generator |
| 8 — Random eviction | `internal/eviction` | `rapid.IntRange` |
| 9 — Consistent hashing determinism | `internal/cluster` | `rapid.String` |
| 10 — Minimal remapping | `internal/cluster` | key set + node removal |
| 11 — Cache before DB | `internal/api` | mock cache + mock DB |
| 12 — Cache population on miss | `internal/api` | product ID generator |
| 13 — X-Cache header | `internal/api` | product ID, cache state |
| 14 — Write-through DB preservation | `internal/invalidation` | product update generator |
| 15 — Proxy caches full response | `internal/proxy` | URL generator, response generator |
| 16 — no-store/no-cache/private not cached | `internal/proxy` | Cache-Control directive generator |
| 17 — max-age drives TTL | `internal/proxy` | `rapid.IntRange(1, 86400)`, mock clock |
| 18 — PUT invalid input | `internal/api` | invalid JSON generator, non-existent ID |

### Unit Tests

Unit tests focus on:
- Specific examples that demonstrate correct behavior (e.g., exact LRU eviction order with 3 entries)
- Integration points between components (e.g., coordinator routing to correct node)
- Edge cases not covered by generators (e.g., empty cache eviction, single-entry cache)
- Error conditions (e.g., DB unavailable returns 503, not 500)

Avoid writing unit tests that duplicate what property tests already cover (e.g., don't write 10 unit tests for LRU when the property test covers all orderings).

### Integration Tests

Integration tests run against real services (or Docker containers) and cover:
- Raft election completes within 10 seconds after coordinator failure (Requirement 4.2)
- Write-behind invalidation completes within 5 seconds (Requirement 6.3)
- Full `docker compose up` — all `/health` endpoints return 200 (Requirement 11.4)
- Prometheus scrapes all targets successfully (Requirement 9.5)
- Concurrent cache access under `go test -race` (Requirement 1.6)

### Smoke Tests

Smoke tests verify structural and configuration requirements:
- All four eviction policies satisfy the `EvictionPolicy` interface (compile-time)
- All three invalidation strategies satisfy the `InvalidationStrategy` interface (compile-time)
- Single `go.mod` at root
- `docker-compose.yml` defines all required services
- `.proto` files exist and generate valid Go bindings

### Benchmark Tests

Benchmark tests use Go's `testing.B` framework and run under real load:
- Eviction policy benchmark: same workload against all 4 policies, records hit rate, miss rate, eviction count, mean latency, p99 latency
- Invalidation strategy benchmark: same workload against all 3 strategies, records write latency, read latency, hit rate
- Results are printed as a table for direct comparison

### Build Order and Test Execution

Tests are run in build order to catch dependency issues early:

```
1. go test ./internal/cache/...       # cache core + eviction policies
2. go test ./internal/cluster/...     # consistent hashing + coordinator
3. go test ./internal/raft/...        # Raft state machine
4. go test ./internal/api/...         # API handlers + invalidation
5. go test ./internal/proxy/...       # reverse proxy
6. go test -race ./...                # race detector across all packages
7. go test -run Integration ./...     # integration tests (requires Docker)
```


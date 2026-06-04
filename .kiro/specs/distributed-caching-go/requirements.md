# Requirements Document

## Introduction

A locally runnable Go learning sandbox that demonstrates caching behavior at each layer of a distributed system. The sandbox implements five layers as real running Go services — in-memory cache, cache cluster, API layer, reverse proxy, and database layer — plus an observability stack. Two additional layers (DNS-level caching, load balancer caching) are covered through documentation only. The goal is to let a developer observe, benchmark, and compare caching strategies empirically rather than theoretically.

## Glossary

- **Sandbox**: The complete locally runnable system. Not production-grade.
- **In-Memory_Cache**: A cache built from scratch in Go using a hash map with TTL. Not a wrapper around Redis or Memcached.
- **Eviction_Policy**: A swappable strategy that determines which entry is removed when the cache is full. Implemented behind a common interface.
- **Cache_Cluster**: A set of three cache nodes that collectively own the full keyspace via consistent hashing.
- **Coordinator**: The Raft-elected leader node responsible for routing requests to the correct cache node.
- **Consistent_Hashing**: The key distribution algorithm that assigns keys to cache nodes and minimises remapping when nodes join or leave.
- **Raft**: The consensus algorithm used exclusively for leader election among cache nodes. Not used for write replication.
- **API_Layer**: A domain-specific HTTP/REST product catalog API with swappable cache invalidation strategies.
- **Invalidation_Strategy**: A swappable policy that determines how the cache is kept consistent with the source of truth after a write.
- **Reverse_Proxy**: A Go HTTP proxy that caches full HTTP responses keyed by URL with no domain knowledge.
- **Cache_Aside**: The pattern where the application checks the cache first; on a miss it fetches from PostgreSQL and populates the cache.
- **Source_of_Truth**: PostgreSQL — the persistent database holding authoritative product catalog data, run via Docker.
- **Observability_Stack**: Prometheus and Grafana run as Docker services. Every component exposes `/metrics`.
- **Benchmark**: A comparative measurement of policies or strategies under the same workload to surface trade-offs.
- **Docker_Compose**: The orchestration tool that starts all services (cache nodes, API layer, reverse proxy, PostgreSQL, Prometheus, Grafana) together.

---

## Requirements

### Requirement 1: In-Memory Cache Core

**User Story:** As a developer, I want a from-scratch in-memory cache with TTL support, so that I can understand how a cache is built without relying on external systems.

#### Acceptance Criteria

1. THE In-Memory_Cache SHALL store key-value pairs with per-entry TTL expiry, where TTL is expressed in seconds and must be in the range 1–86400 inclusive; a TTL of 0 SHALL cause the entry to be treated as immediately expired.
2. WHEN a key's TTL expires, THE In-Memory_Cache SHALL treat that key as absent on the next read, returning the zero value and `false` as the cache-miss signal.
3. WHEN the cache reaches its configured maximum capacity (between 1 and 1,000,000 entries inclusive), THE In-Memory_Cache SHALL evict one entry according to the active Eviction_Policy before accepting a new entry.
4. THE In-Memory_Cache SHALL expose a common Go interface that all four Eviction_Policy implementations satisfy, so that policies are swappable at construction time without changing call sites.
5. WHEN a Get operation is performed on a missing or expired key, THE In-Memory_Cache SHALL return the zero value and `false`; it SHALL never return `true` for a missing or expired key.
6. THE In-Memory_Cache SHALL be safe for concurrent use from multiple goroutines, with no data races detectable under the Go race detector (`go test -race`).
7. WHEN a Set operation is performed on a key that already exists in the cache, THE In-Memory_Cache SHALL upsert the entry, replacing the existing value and resetting the TTL.

---

### Requirement 2: Eviction Policies

**User Story:** As a developer, I want four swappable eviction policies behind a common interface, so that I can compare their behaviour empirically under the same workload.

#### Acceptance Criteria

1. WHEN the In-Memory_Cache is at maximum capacity and a new entry must be inserted, THE LRU Eviction_Policy SHALL evict the entry that was least recently accessed, where "accessed" means any Get or Set operation on that key.
2. WHEN the In-Memory_Cache is at maximum capacity and a new entry must be inserted, THE LFU Eviction_Policy SHALL evict the entry with the lowest access frequency (count of Get and Set operations); ties SHALL be broken by evicting the entry with the earliest last-access timestamp.
3. WHEN the In-Memory_Cache is at maximum capacity and a new entry must be inserted, THE TTL-only Eviction_Policy SHALL evict the entry with the earliest absolute expiry time.
4. WHEN the In-Memory_Cache is at maximum capacity and a new entry must be inserted, THE Random Eviction_Policy SHALL evict a uniformly random entry from the current set of entries.
5. WHEN a Benchmark is run against the In-Memory_Cache, THE Benchmark SHALL record hit rate, miss rate, eviction count, mean latency (ms), and p99 latency (ms) for each Eviction_Policy.
6. IF a policy raises an error or returns no result during a Benchmark run, THE Benchmark SHALL record the failure for that policy and continue recording metrics for the remaining policies rather than halting.
7. WHEN a Benchmark completes, THE Benchmark SHALL produce a report with one row per Eviction_Policy and columns for hit rate, miss rate, eviction count, mean latency (ms), and p99 latency (ms), enabling direct numerical comparison.

---

### Requirement 3: Cache Cluster and Consistent Hashing

**User Story:** As a developer, I want a three-node cache cluster using consistent hashing for key distribution, so that I can observe how keys are partitioned and what happens when a node fails.

#### Acceptance Criteria

1. THE Cache_Cluster SHALL consist of exactly three cache nodes, each running as a separate Go process.
2. WHILE all three cache nodes are reachable, THE Cache_Cluster SHALL use Consistent_Hashing to assign each key to exactly one node, so that a given key always routes to the same node.
3. WHEN a node joins or leaves the Cache_Cluster, THE Consistent_Hashing ring SHALL remap only the keys whose ownership changes due to updated token range boundaries; keys whose owning node is unchanged SHALL NOT be remapped.
4. WHEN a cache request arrives at the Coordinator, THE Coordinator SHALL check whether the target node owns the key according to the Consistent_Hashing ring and SHALL reject the request with an explicit routing-mismatch error if the ownership check fails.
5. WHEN a cache node has not responded to a health-check probe within 500ms, THE Cache_Cluster SHALL treat that node as unavailable and SHALL treat all requests for keys owned by that node as cache misses, allowing them to fall through to the origin without returning an error to the caller.
6. THE Cache_Cluster SHALL use gRPC with Protobuf message definitions for all internal communication between cache nodes and the Coordinator.
7. A cache node SHALL be considered reachable if and only if it responds to a gRPC health-check probe within 500ms; a node that fails to respond within this threshold SHALL be considered unavailable.

---

### Requirement 4: Raft Leader Election

**User Story:** As a developer, I want Raft-based leader election among the three cache nodes, so that I can observe how distributed consensus works in practice.

#### Acceptance Criteria

1. THE Cache_Cluster SHALL use the Raft consensus algorithm exclusively for electing a Coordinator from among the three nodes.
2. WHEN the current Coordinator node becomes unavailable, THE Cache_Cluster SHALL elect a new Coordinator via Raft within 10 seconds.
3. THE Raft implementation SHALL NOT replicate cache data across nodes — cache entries live on exactly one node as determined by Consistent_Hashing.
4. WHEN a Raft election completes, THE Cache_Cluster SHALL have exactly one active Coordinator, confirmed by a successful gRPC health-check response from that node.
5. WHEN a previously failed node rejoins the Cache_Cluster, THE Cache_Cluster SHALL allow that node to participate in future Raft elections as a follower eligible to vote and stand as a candidate.
6. WHILE no Coordinator has been elected (during an election or after a split), THE Cache_Cluster SHALL reject all incoming routing requests with an error indicating that no Coordinator is currently available.

---

### Requirement 5: API Layer — Product Catalog

**User Story:** As a developer, I want a domain-specific HTTP/REST product catalog API with caching middleware, so that I can observe cache key design, invalidation, and staleness in a realistic context.

#### Acceptance Criteria

1. THE API_Layer SHALL expose a `GET /products/{id}` endpoint that returns a single product by identifier as a JSON response.
2. THE API_Layer SHALL expose a `GET /products` endpoint that returns a paginated list of products, accepting `page` (default: 1) and `pageSize` (default: 20, maximum: 100) as query parameters.
3. THE API_Layer SHALL expose a `PUT /products/{id}` endpoint that updates a product and triggers cache invalidation according to the active Invalidation_Strategy.
4. IF a `GET /products/{id}` request results in a cache miss, THE API_Layer SHALL fetch the product from the Source_of_Truth, populate the cache with the result, and return the product to the caller.
5. THE API_Layer SHALL expose a common Go interface that all three Invalidation_Strategy implementations satisfy, so that strategies are swappable at construction time without changing handler code.
6. WHEN a `GET /products/{id}` request is served from cache, THE API_Layer SHALL include an `X-Cache: HIT` response header; IF the header cannot be added, THE API_Layer SHALL fail the request rather than serve the cached data without the header.
7. WHEN a `GET /products/{id}` request is served from the Source_of_Truth, THE API_Layer SHALL include an `X-Cache: MISS` response header.
8. WHEN a `GET /products/{id}` request is made for a product identifier that does not exist in the Source_of_Truth, THE API_Layer SHALL return a 404 response.
9. IF the Source_of_Truth is unavailable on a cache miss, THE API_Layer SHALL return a 503 response to the caller.
10. WHEN a `PUT /products/{id}` request is made with an invalid request body or a non-existent product identifier, THE API_Layer SHALL return a 400 or 404 response respectively.

---

### Requirement 6: Cache Invalidation Strategies

**User Story:** As a developer, I want three swappable cache invalidation strategies, so that I can compare their consistency and latency trade-offs empirically.

#### Acceptance Criteria

1. THE API_Layer SHALL implement a TTL-based Invalidation_Strategy that serves potentially stale data until the TTL expires, with no synchronous invalidation on write.
2. THE API_Layer SHALL implement a write-through Invalidation_Strategy that updates the cache synchronously on every write before returning a response to the caller; IF the cache update fails, the Source_of_Truth write SHALL still commit and the next read for that key SHALL be treated as a cache miss.
3. THE API_Layer SHALL implement a write-behind Invalidation_Strategy that invalidates the cache asynchronously after the write completes; the async invalidation SHALL complete within 5 seconds; IF async invalidation fails, the entry SHALL expire via its natural TTL.
4. WHEN a Benchmark is run against the API_Layer, THE Benchmark SHALL record write latency, read latency, and cache hit rate for each Invalidation_Strategy; IF one or more strategies produce zero recorded operations, THE Benchmark SHALL record the failure for those strategies and continue recording metrics for the remaining strategies rather than halting.
5. WHEN a Benchmark completes, THE Benchmark SHALL produce a report that includes write latency, read latency, and cache hit rate for each Invalidation_Strategy, enabling direct numerical comparison of all three results.

---

### Requirement 7: Reverse Proxy with HTTP Response Caching

**User Story:** As a developer, I want a reverse proxy that caches full HTTP responses, so that I can observe caching at the transport layer without any domain knowledge.

#### Acceptance Criteria

1. THE Reverse_Proxy SHALL cache full HTTP responses — including status code, response headers, and body — keyed by the full request URL including query string.
2. THE Reverse_Proxy SHALL have no knowledge of the product domain; it SHALL cache any response regardless of content type or path.
3. WHEN a cached response is available for a request URL, THE Reverse_Proxy SHALL serve the cached response without forwarding the request to the API_Layer.
4. WHEN no cached response is available for a request URL, THE Reverse_Proxy SHALL forward the request to the API_Layer, cache the response, and return it to the caller.
5. IF the API_Layer does not respond within 30 seconds, THE Reverse_Proxy SHALL serve a stale cached response if one exists; IF no cached response is available, THE Reverse_Proxy SHALL return a 5xx error to the caller.
6. THE Reverse_Proxy SHALL honour `Cache-Control` response headers from the API_Layer; responses containing `no-store`, `no-cache`, or `private` directives SHALL NOT be cached.
7. WHEN a `Cache-Control: max-age=N` directive is present and N is greater than zero, THE Reverse_Proxy SHALL expire the cached response after N seconds.
8. WHEN a `Cache-Control: max-age=0` directive is present, THE Reverse_Proxy SHALL forward the request to the API_Layer, return the response to the caller, and NOT store the response in the cache.

---

### Requirement 8: Database Layer and Cache-Aside

**User Story:** As a developer, I want PostgreSQL as the source of truth with a cache-aside pattern, so that I can observe how a persistent database integrates with an in-memory cache.

#### Acceptance Criteria

1. THE Source_of_Truth SHALL be a PostgreSQL instance run as a Docker service alongside the other components.
2. THE API_Layer SHALL always check the cache before querying the Source_of_Truth; a Source_of_Truth query SHALL only be issued when the cache returns a miss for the requested key.
3. WHEN the Source_of_Truth is queried on a cache miss, THE API_Layer SHALL populate the cache with the fetched value before returning the response to the caller.
4. THE Source_of_Truth SHALL be the authoritative record for all product data; the cache SHALL never be written to without a corresponding record existing in the Source_of_Truth.
5. WHERE a direct Source_of_Truth query is required that bypasses the Cache_Aside pattern, THE API_Layer SHALL permit such queries without cache interaction.
6. WHEN the cache is populated from the Source_of_Truth, THE API_Layer SHALL set a TTL of at least 1 second on the cached entry.
7. IF the Source_of_Truth is unavailable when a cache miss occurs, THE API_Layer SHALL return a 503 error to the caller and SHALL NOT populate the cache with a partial or error response.

---

### Requirement 9: Observability

**User Story:** As a developer, I want Prometheus metrics and Grafana dashboards across all components, so that I can visually compare eviction policies and invalidation strategies.

#### Acceptance Criteria

1. THE In-Memory_Cache SHALL expose a `/metrics` endpoint in Prometheus exposition format reporting cache hit count, miss count, eviction count, and current entry count, each labelled with the active Eviction_Policy name.
2. THE Cache_Cluster SHALL expose a `/metrics` endpoint per node reporting cache hit count, miss count, eviction count, current entry count, and the number of requests routed to that node, each labelled with the active Eviction_Policy name.
3. THE API_Layer SHALL expose a `/metrics` endpoint reporting cache hit count, miss count, and p99 HTTP request latency labelled by endpoint path.
4. THE Reverse_Proxy SHALL expose a `/metrics` endpoint reporting cache hit count, miss count, and p99 proxied request latency.
5. THE Observability_Stack SHALL include a Prometheus instance configured to scrape `/metrics` from all components.
6. THE Observability_Stack SHALL include a Grafana instance with dashboards provisioned at startup (without manual setup) that display hit rate, miss rate, eviction counts, and request latency for each component.
7. WHILE an Eviction_Policy Benchmark is running, THE Observability_Stack SHALL display live metrics in Grafana labelled by policy name so that all four policies can be compared on the same dashboard.
8. WHILE an Invalidation_Strategy Benchmark is running, THE Observability_Stack SHALL display live metrics in Grafana labelled by strategy name so that all three strategies can be compared on the same dashboard.

---

### Requirement 10: Communication Protocols

**User Story:** As a developer, I want HTTP/REST for external communication and gRPC for internal communication, so that I can observe the difference between the two protocols in the same system.

#### Acceptance Criteria

1. THE Sandbox SHALL use HTTP/REST for all client-to-Reverse_Proxy communication.
2. THE Reverse_Proxy SHALL use gRPC to communicate with the API_Layer.
3. THE Cache_Cluster SHALL use gRPC with Protobuf message definitions for all internal communication: cache node to Coordinator, and cache node to cache node.
4. THE Sandbox SHALL define all gRPC message types and service contracts in `.proto` files within the repository.
5. WHEN a `.proto` file is modified, THE Sandbox build process SHALL regenerate Go bindings from that file before compiling any dependent service.
6. IF proto compilation fails, THE Sandbox build process SHALL halt and emit an error message identifying the failing `.proto` file and the compilation error.

---

### Requirement 11: Project Structure and Orchestration

**User Story:** As a developer, I want a single Go module with Docker Compose orchestration, so that I can start the entire sandbox with one command and navigate the codebase consistently.

#### Acceptance Criteria

1. THE Sandbox SHALL be a single Go module with one `go.mod` file at the repository root.
2. THE Sandbox SHALL organise each service as a package under `internal/` within the single module.
3. THE Sandbox SHALL provide a `docker-compose.yml` at the repository root that starts all services: the three cache nodes, the API_Layer, the Reverse_Proxy, PostgreSQL, Prometheus, and Grafana.
4. WHEN `docker compose up` is run from the repository root, THE Sandbox SHALL start all services; each service SHALL respond with HTTP 200 on its `/health` endpoint when healthy; IF a service container exits with a non-zero status code, THE Sandbox SHALL allow the remaining healthy services to continue operating.
5. THE Sandbox SHALL provide shared packages containing at minimum: the Eviction_Policy interface, the Invalidation_Strategy interface, Prometheus metrics helpers, and configuration loading utilities, importable by all service packages.

---

### Requirement 12: Conceptual Documentation

**User Story:** As a developer, I want documentation covering DNS-level caching and load balancer caching, so that I understand where caching fits in the full request path even for layers not implemented as running services.

#### Acceptance Criteria

1. THE Sandbox SHALL include a documentation file covering DNS-level caching that addresses each of the following topics: how TTL works in DNS responses, how recursive resolvers cache records, the trade-offs of short vs long DNS TTLs, and how DNS caching differs from application-level caching.
2. THE Sandbox SHALL include a documentation file covering load balancer caching that addresses each of the following topics: how layer-7 load balancers can cache responses, the difference between connection-level and response-level caching, and an explicit comparison to the Reverse_Proxy layer implemented in the Sandbox (domain-agnostic HTTP response caching vs. infrastructure-level connection routing).
3. THE Sandbox documentation SHALL include a dedicated section containing a table or labeled list that assigns each layer (DNS, load balancer, reverse proxy, API, cache cluster, in-memory cache, database) an explicit implementation status of either "implemented as a running service" or "conceptual documentation only."

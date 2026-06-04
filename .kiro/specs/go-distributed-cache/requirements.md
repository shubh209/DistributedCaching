# Requirements Document

## Introduction

A Go-based distributed caching system designed as a learning resource and working implementation that demonstrates caching at every layer of a distributed system. The project covers DNS caching, reverse proxy caching, load balancer caching, API layer caching, in-memory caching, and database query caching — showing how each layer interacts and contributes to overall system performance. Drawing on lessons from a prior Java-based Raft cache (105x read speedup, 15 writes/sec, 100% success rate across a 3-node cluster), this project targets idiomatic Go patterns, observable cache behavior, and clear layer separation.

## Glossary

- **Cache_Node**: A single Go process that stores key-value pairs in memory and participates in the distributed cluster.
- **Cache_Cluster**: A group of Cache_Nodes that collectively serve cache reads and writes.
- **DNS_Cache**: A local TTL-based cache that stores resolved hostnames to IP address mappings, avoiding repeated DNS lookups.
- **Reverse_Proxy**: An HTTP intermediary that sits in front of backend services and caches full HTTP responses.
- **Load_Balancer**: A component that distributes incoming requests across Cache_Nodes using a consistent hashing strategy.
- **API_Cache**: A middleware layer within the HTTP API server that caches serialized responses keyed by request parameters.
- **DB_Cache**: A read-through cache layer that sits between the application and a backing store (e.g., SQLite or Redis), caching query results.
- **In_Memory_Store**: The core key-value store within each Cache_Node, backed by a concurrent map with TTL eviction.
- **Eviction_Policy**: The algorithm used to remove entries from the In_Memory_Store when capacity is reached (LRU, LFU, or TTL-based).
- **Consistent_Hash_Ring**: A ring-based data structure used by the Load_Balancer to deterministically map keys to Cache_Nodes with minimal remapping on node changes.
- **TTL**: Time-to-live; the duration after which a cached entry is considered stale and eligible for eviction.
- **Cache_Hit**: A cache lookup that returns a valid, non-expired value.
- **Cache_Miss**: A cache lookup that finds no entry or an expired entry, requiring a fetch from the origin.
- **Metrics_Collector**: A component that records hit rate, miss rate, eviction count, and latency per cache layer.
- **Health_Checker**: A background goroutine that periodically probes Cache_Nodes and marks them as available or unavailable.
- **Replication_Manager**: A component responsible for propagating writes to replica Cache_Nodes for fault tolerance.
- **Client**: Any HTTP or gRPC caller that interacts with the system through the API layer.

---

## Requirements

### Requirement 1: In-Memory Key-Value Store

**User Story:** As a developer learning distributed caching, I want a concurrent in-memory key-value store with TTL and eviction, so that I can understand the foundational data structure that all higher-level cache layers build upon.

#### Acceptance Criteria

1. THE In_Memory_Store SHALL store string keys mapped to byte-slice values with an associated TTL duration.
2. WHEN a key is inserted with a TTL, THE In_Memory_Store SHALL mark the entry as expired after the TTL elapses.
3. WHEN a Get operation is performed on an expired key, THE In_Memory_Store SHALL treat the key as absent and return a Cache_Miss.
4. WHEN the In_Memory_Store reaches its configured maximum capacity, THE In_Memory_Store SHALL evict entries according to the configured Eviction_Policy before accepting new writes.
5. THE In_Memory_Store SHALL support LRU and TTL-based Eviction_Policies, selectable at startup via configuration.
6. WHEN concurrent goroutines perform simultaneous reads and writes, THE In_Memory_Store SHALL preserve data consistency without data races.
7. THE In_Memory_Store SHALL expose Get, Set, Delete, and Flush operations.
8. FOR ALL keys inserted and retrieved without expiry or eviction, the value returned by Get SHALL equal the value provided to Set (round-trip property).

---

### Requirement 2: Cache Node HTTP API

**User Story:** As a developer, I want each Cache_Node to expose an HTTP API for cache operations, so that I can interact with the cache over the network and observe request/response behavior at the API layer.

#### Acceptance Criteria

1. THE Cache_Node SHALL expose HTTP endpoints: `GET /cache/{key}`, `PUT /cache/{key}`, and `DELETE /cache/{key}`.
2. WHEN a `GET /cache/{key}` request is received for an existing, non-expired key, THE Cache_Node SHALL respond with HTTP 200 and the cached value in the response body.
3. WHEN a `GET /cache/{key}` request is received for an absent or expired key, THE Cache_Node SHALL respond with HTTP 404.
4. WHEN a `PUT /cache/{key}` request is received with a valid body and optional TTL header, THE Cache_Node SHALL store the value and respond with HTTP 201.
5. IF a `PUT /cache/{key}` request body exceeds 1 MB, THEN THE Cache_Node SHALL respond with HTTP 413 and reject the write.
6. WHEN a `DELETE /cache/{key}` request is received, THE Cache_Node SHALL remove the entry and respond with HTTP 204.
7. THE Cache_Node SHALL expose a `GET /health` endpoint that returns HTTP 200 when the node is operational.
8. THE Cache_Node SHALL expose a `GET /metrics` endpoint returning hit count, miss count, eviction count, and average GET latency in JSON format.

---

### Requirement 3: API Layer Caching (Response Cache Middleware)

**User Story:** As a developer, I want the HTTP API server to cache serialized responses at the handler layer, so that I can observe how application-level caching reduces redundant computation and backend load.

#### Acceptance Criteria

1. THE API_Cache SHALL intercept GET requests before they reach the In_Memory_Store lookup and return a cached HTTP response when one exists for the same key.
2. WHEN an API_Cache entry exists for a key and has not expired, THE API_Cache SHALL serve the response without invoking the In_Memory_Store.
3. WHEN an API_Cache entry is absent or expired, THE API_Cache SHALL forward the request to the In_Memory_Store, cache the response, and return it to the Client.
4. THE API_Cache SHALL respect a configurable TTL that is independent of the In_Memory_Store TTL.
5. WHEN a `PUT` or `DELETE` request modifies a key, THE API_Cache SHALL invalidate any cached GET response for that key.
6. THE API_Cache SHALL record Cache_Hit and Cache_Miss counts separately from the In_Memory_Store counters and include them in the `/metrics` response.

---

### Requirement 4: Load Balancer with Consistent Hashing

**User Story:** As a developer, I want a load balancer that routes requests to Cache_Nodes using consistent hashing, so that I can understand how key-to-node mapping minimizes cache invalidation when the cluster topology changes.

#### Acceptance Criteria

1. THE Load_Balancer SHALL maintain a Consistent_Hash_Ring containing all available Cache_Nodes.
2. WHEN a request for a given key arrives, THE Load_Balancer SHALL route the request to the Cache_Node that owns that key on the Consistent_Hash_Ring.
3. WHEN a Cache_Node is added to the cluster, THE Load_Balancer SHALL remap only the keys previously owned by the node's neighbors on the ring, leaving all other key-to-node mappings unchanged.
4. WHEN a Cache_Node is removed from the cluster, THE Load_Balancer SHALL remap only the keys previously owned by that node to the next node on the ring.
5. THE Load_Balancer SHALL support a configurable number of virtual nodes per Cache_Node to control key distribution uniformity.
6. WHEN all Cache_Nodes are unavailable, THE Load_Balancer SHALL return a 503 response to the Client.
7. THE Load_Balancer SHALL cache the resolved node address for a given key for a configurable duration to reduce ring lookups on repeated requests (load balancer-level caching).

---

### Requirement 5: DNS Caching

**User Story:** As a developer, I want a DNS cache layer that stores hostname-to-IP resolutions with TTL, so that I can understand how DNS caching reduces lookup latency and external resolver load.

#### Acceptance Criteria

1. THE DNS_Cache SHALL intercept all outbound hostname resolutions made by Cache_Nodes and the Load_Balancer before forwarding to the system resolver.
2. WHEN a hostname has been resolved and the result is stored in the DNS_Cache with a TTL, THE DNS_Cache SHALL return the cached IP address for subsequent lookups until the TTL expires.
3. WHEN a DNS_Cache entry expires, THE DNS_Cache SHALL perform a fresh resolution on the next lookup and update the cache with the new result and TTL.
4. IF a DNS resolution fails, THEN THE DNS_Cache SHALL return the last known valid IP address if one exists and has not exceeded a configurable stale grace period.
5. THE DNS_Cache SHALL record hit count, miss count, and average resolution latency and expose these via the `/metrics` endpoint.
6. FOR ALL hostnames resolved and cached, the IP address returned during the TTL window SHALL equal the IP address returned by the initial resolution (consistency property).

---

### Requirement 6: Reverse Proxy Caching

**User Story:** As a developer, I want a reverse proxy that caches full HTTP responses from backend Cache_Nodes, so that I can observe how edge-layer caching reduces backend traffic for repeated identical requests.

#### Acceptance Criteria

1. THE Reverse_Proxy SHALL sit in front of the Cache_Node cluster and intercept all inbound HTTP requests from Clients.
2. WHEN a GET request arrives and a cached response exists for the same URL and query parameters, THE Reverse_Proxy SHALL return the cached response without forwarding the request to a Cache_Node.
3. WHEN a GET request arrives and no cached response exists, THE Reverse_Proxy SHALL forward the request to the appropriate Cache_Node, cache the response, and return it to the Client.
4. THE Reverse_Proxy SHALL respect `Cache-Control: no-cache` request headers by bypassing the cache and forwarding directly to the Cache_Node.
5. WHEN a PUT or DELETE request is forwarded to a Cache_Node, THE Reverse_Proxy SHALL invalidate any cached GET response for the affected key.
6. THE Reverse_Proxy SHALL add an `X-Cache: HIT` or `X-Cache: MISS` response header to every response to make cache behavior observable.
7. THE Reverse_Proxy SHALL record hit rate, miss rate, and total proxied request count and expose these via its own `/metrics` endpoint.

---

### Requirement 7: Database Query Caching

**User Story:** As a developer, I want a DB_Cache layer that caches query results from a backing store, so that I can understand how database-level caching reduces query latency and database load.

#### Acceptance Criteria

1. THE DB_Cache SHALL intercept read queries before they reach the backing store and return cached results when available.
2. WHEN a read query result is not present in the DB_Cache, THE DB_Cache SHALL execute the query against the backing store, cache the result with a configurable TTL, and return it to the caller.
3. WHEN a write operation modifies data that corresponds to a cached query result, THE DB_Cache SHALL invalidate the affected cache entries.
4. THE DB_Cache SHALL support a configurable maximum number of cached query results and evict the least-recently-used entry when the limit is reached.
5. THE DB_Cache SHALL record hit count, miss count, and average query latency (cached vs. uncached) and expose these via the `/metrics` endpoint.
6. FOR ALL read queries executed and cached, the result returned from the DB_Cache during the TTL window SHALL equal the result returned by the backing store at the time of caching (consistency property).

---

### Requirement 8: Replication and Fault Tolerance

**User Story:** As a developer, I want writes to be replicated across multiple Cache_Nodes, so that I can understand how replication provides fault tolerance and prevents data loss when a node fails.

#### Acceptance Criteria

1. WHEN a write is accepted by a Cache_Node, THE Replication_Manager SHALL propagate the write to a configurable number of replica Cache_Nodes before acknowledging success to the Client.
2. WHEN a replica Cache_Node is unavailable during replication, THE Replication_Manager SHALL retry the write up to a configurable maximum number of attempts before marking the replica as degraded.
3. WHEN a Cache_Node rejoins the cluster after a failure, THE Replication_Manager SHALL synchronize the rejoining node with writes it missed during its absence.
4. THE Health_Checker SHALL probe each Cache_Node at a configurable interval and update the Load_Balancer's Consistent_Hash_Ring when a node's availability changes.
5. WHEN a Cache_Node is marked unavailable by the Health_Checker, THE Load_Balancer SHALL stop routing new requests to that node within one health-check interval.
6. THE Cache_Cluster SHALL remain available for reads and writes as long as at least one Cache_Node is operational.

---

### Requirement 9: Observability and Metrics

**User Story:** As a developer learning distributed caching, I want unified metrics across all cache layers, so that I can measure the contribution of each layer to overall system performance and compare results to the prior Java implementation.

#### Acceptance Criteria

1. THE Metrics_Collector SHALL aggregate hit count, miss count, eviction count, and p50/p99 GET latency for each cache layer: In_Memory_Store, API_Cache, Load_Balancer, DNS_Cache, Reverse_Proxy, and DB_Cache.
2. THE Metrics_Collector SHALL expose all aggregated metrics at a single `GET /metrics` endpoint in JSON format.
3. WHEN a cache operation completes, THE Metrics_Collector SHALL record the operation's latency within 1 millisecond of completion.
4. THE Metrics_Collector SHALL compute and expose the overall Cache_Hit rate as a percentage across all layers combined.
5. WHERE Prometheus integration is enabled, THE Metrics_Collector SHALL expose metrics in Prometheus text format at `GET /metrics/prometheus`.

---

### Requirement 10: Configuration and Startup

**User Story:** As a developer, I want all cache layers to be configurable via a single YAML file, so that I can tune each layer independently and understand the effect of configuration on cache behavior.

#### Acceptance Criteria

1. THE Cache_Node SHALL read its configuration from a YAML file at a path specified by the `--config` CLI flag at startup.
2. THE Cache_Node SHALL validate the configuration file at startup and, IF a required field is missing or invalid, THEN THE Cache_Node SHALL log a descriptive error and exit with a non-zero status code.
3. THE Cache_Node configuration SHALL include: node ID, listen address, cluster peer addresses, In_Memory_Store capacity, Eviction_Policy, default TTL, replication factor, and health-check interval.
4. THE Load_Balancer configuration SHALL include: listen address, Cache_Node addresses, virtual node count, and node-address cache TTL.
5. THE Reverse_Proxy configuration SHALL include: listen address, upstream Load_Balancer address, response cache TTL, and maximum cached response size.
6. WHEN the configuration file is updated and the process receives a SIGHUP signal, THE Cache_Node SHALL reload non-structural configuration values (TTL, eviction policy, log level) without restarting.

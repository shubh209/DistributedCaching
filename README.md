# Distributed Caching — Go Learning Sandbox

A locally runnable Go sandbox that demonstrates caching at every layer of a distributed system. The goal is empirical learning: observe, benchmark, and compare caching strategies by watching live Prometheus/Grafana metrics while the system runs under real load.

## Quick Start

```bash
# Start everything
docker compose up --build

# Open Grafana dashboards
open http://localhost:3000

# Hit the API via the reverse proxy
curl http://localhost:8080/products/prod-001

# Run the eviction policy benchmark
docker compose run --rm benchmark --mode=eviction
```

## Layer Status

Each layer of the distributed system is either implemented as a running service or covered through conceptual documentation.

| Layer | Status | Details |
|---|---|---|
| **DNS Caching** | 📄 Conceptual only | [docs/dns-caching.md](docs/dns-caching.md) |
| **Load Balancer Caching** | 📄 Conceptual only | [docs/loadbalancer-caching.md](docs/loadbalancer-caching.md) |
| **Reverse Proxy** | ✅ Running service | HTTP response caching, Cache-Control semantics — port 8080 |
| **API Layer** | ✅ Running service | Product catalog, cache-aside, 3 invalidation strategies — port 8081 |
| **Cache Cluster** | ✅ Running service | 3 nodes, consistent hashing, Raft leader election — ports 9001-9003 |
| **In-Memory Cache** | ✅ Running service | Built from scratch, 4 swappable eviction policies |
| **Database** | ✅ Running service | PostgreSQL source of truth — port 5432 |

## Architecture

```
Client (curl / browser)
    ↓  HTTP/REST
Reverse Proxy :8080          ← full HTTP response cache, Cache-Control
    ↓  gRPC
API Layer :8081              ← product catalog, cache-aside, invalidation strategies
    ↓  gRPC                         ↓  SQL
Cache Cluster :9001-9003     PostgreSQL :5432
(3 nodes, Raft, consistent hashing)

Prometheus :9090  ←  scrapes /metrics from all services
Grafana :3000     ←  dashboards from Prometheus data
```

## What You Can Learn From This Project

### Eviction Policies
Run `--mode=eviction` to compare LRU, LFU, TTL-only, and Random side-by-side under the same Zipf-distributed workload. Watch hit rates on the Grafana eviction benchmark dashboard.

### Invalidation Strategies
Change `INVALIDATION_STRATEGY` in `docker-compose.yml` to `ttl`, `write-through`, or `write-behind`, then observe:
- How long stale data persists (TTL)
- Write latency increase from synchronous cache updates (write-through)
- The brief staleness window before async deletion completes (write-behind)

### Consistent Hashing
Kill `cache-node-2` with `docker compose stop cache-node-2` and observe:
- Cache miss spike for keys owned by node 2
- Keys on nodes 1 and 3 are completely unaffected
- When node 2 comes back, its keys gradually repopulate via cache-aside

### Raft Leader Election
Kill the coordinator node and observe:
- `ErrNoCoordinator` responses during the election window (~10 seconds)
- A new coordinator is elected and routing resumes automatically

## Services and Ports

| Service | Protocol | Port | Purpose |
|---|---|---|---|
| proxy | HTTP | 8080 | Client entry point |
| api | HTTP | 8081 | Product catalog API + metrics |
| cache-node-1 | gRPC | 9001 | Cache node + Raft |
| cache-node-1 | HTTP | 9101 | Metrics + health |
| cache-node-2 | gRPC | 9002 | Cache node + Raft |
| cache-node-2 | HTTP | 9102 | Metrics + health |
| cache-node-3 | gRPC | 9003 | Cache node + Raft |
| cache-node-3 | HTTP | 9103 | Metrics + health |
| postgres | TCP | 5432 | PostgreSQL |
| prometheus | HTTP | 9090 | Metrics collection |
| grafana | HTTP | 3000 | Dashboards |

## Configuration

All services are configured via environment variables in `docker-compose.yml`:

| Variable | Values | Default | Service |
|---|---|---|---|
| `EVICTION_POLICY` | `lru`, `lfu`, `ttl`, `random` | `lru` | cache nodes |
| `CACHE_CAPACITY` | integer | `10000` | cache nodes |
| `INVALIDATION_STRATEGY` | `ttl`, `write-through`, `write-behind` | `write-through` | api |
| `DB_DSN` | postgres DSN | — | api |

## Running Tests

```bash
# Unit tests (requires Go installed)
go test ./...

# With race detector
go test -race ./...
```

## Project Structure

```
.
├── cmd/                    # Service entry points (main.go per service)
├── internal/
│   ├── shared/             # Interfaces: Cache, EvictionPolicy, InvalidationStrategy
│   ├── cache/              # In-memory cache core
│   ├── eviction/           # LRU, LFU, TTL-only, Random
│   ├── cluster/            # Consistent hashing ring + coordinator
│   ├── raft/               # Leader election state machine
│   ├── api/                # Product catalog HTTP handlers
│   ├── invalidation/       # TTL, write-through, write-behind
│   ├── proxy/              # HTTP response caching
│   ├── metrics/            # Prometheus metric definitions
│   ├── benchmark/          # Eviction + invalidation benchmarks
│   └── db/                 # PostgreSQL queries
├── proto/                  # Protobuf definitions (CacheService, RaftService, APIService)
├── observability/          # Prometheus config + Grafana dashboards
└── docs/                   # Conceptual documentation
    ├── adr/                # Architecture Decision Records
    ├── dns-caching.md
    └── loadbalancer-caching.md
```

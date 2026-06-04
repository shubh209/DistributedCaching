package cluster

import (
	"context"
	"sync/atomic"

	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/shared"
)

// CacheNodeMetrics tracks hit/miss/eviction counts for Stats() and Prometheus.
// Uses atomic counters so the gRPC handlers can update them concurrently
// without holding a lock.
type CacheNodeMetrics struct {
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
}

// CacheServer implements the CacheService gRPC interface.
// It wraps the InMemoryCache and exposes it over the network.
// Once proto-gen runs, this struct will embed the generated
// UnimplementedCacheServiceServer and the method signatures will
// use the generated request/response types.
type CacheServer struct {
	nodeID  string
	cache   *cache.InMemoryCache
	policy  shared.EvictionPolicy
	metrics CacheNodeMetrics
	isLeader func() bool // injected by main.go — returns true if this node is the Raft leader
}

// NewCacheServer creates a CacheServer for the given node.
// isLeader is a function (not a bool) so it always reflects current state.
func NewCacheServer(nodeID string, c *cache.InMemoryCache, policy shared.EvictionPolicy, isLeader func() bool) *CacheServer {
	return &CacheServer{
		nodeID:   nodeID,
		cache:    c,
		policy:   policy,
		isLeader: isLeader,
	}
}

// Get retrieves a value from the in-memory cache.
// Returns found=false on a miss — never an error for a simple miss.
func (s *CacheServer) Get(ctx context.Context, key string) (value []byte, found bool, err error) {
	value, found, err = s.cache.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if found {
		s.metrics.hits.Add(1)
	} else {
		s.metrics.misses.Add(1)
	}
	return value, found, nil
}

// Set stores a key-value pair with the given TTL in seconds.
func (s *CacheServer) Set(ctx context.Context, key string, value []byte, ttlSeconds int64) error {
	ttl := secondsToDuration(ttlSeconds)
	return s.cache.Set(ctx, key, value, ttl)
}

// Delete removes a key from the cache. No-op if key doesn't exist.
func (s *CacheServer) Delete(ctx context.Context, key string) error {
	return s.cache.Delete(ctx, key)
}

// Health reports whether this node is alive and ready.
// is_coordinator is true when this node is the current Raft leader.
func (s *CacheServer) Health(_ context.Context) (healthy bool, isCoordinator bool) {
	return true, s.isLeader()
}

// Stats returns current cache metrics.
// The eviction_policy field enables Grafana to compare all 4 policies
// on the same dashboard using label selectors.
func (s *CacheServer) Stats(_ context.Context) (hits, misses, evictions, entries int64, policyName string) {
	return s.metrics.hits.Load(),
		s.metrics.misses.Load(),
		s.metrics.evictions.Load(),
		int64(s.cache.Len()),
		s.policy.Name()
}

// RecordEviction increments the eviction counter.
// Called by the eviction policy wrapper when an entry is evicted.
func (s *CacheServer) RecordEviction() {
	s.metrics.evictions.Add(1)
}

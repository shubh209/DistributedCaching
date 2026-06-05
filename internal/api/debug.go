package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/user/distributed-caching-go/internal/cluster"
	"github.com/user/distributed-caching-go/internal/shared"
)

// DebugHandler handles diagnostic endpoints for the dashboard.
type DebugHandler struct {
	cache       shared.Cache
	ring        *cluster.Ring
	coordinator *cluster.Coordinator
	nodeAddrs   map[string]string // nodeID → HTTP metrics address e.g. "node1:9101"
}

// NewDebugHandler creates a DebugHandler.
// nodeAddrs maps node IDs to their HTTP health/metrics ports.
func NewDebugHandler(
	cache shared.Cache,
	ring *cluster.Ring,
	coord *cluster.Coordinator,
	nodeAddrs map[string]string,
) *DebugHandler {
	return &DebugHandler{
		cache:       cache,
		ring:        ring,
		coordinator: coord,
		nodeAddrs:   nodeAddrs,
	}
}

// CacheDebugResponse is the response body for GET /cache/debug/{key}.
type CacheDebugResponse struct {
	Key          string `json:"key"`
	OwnerNode    string `json:"owner_node"`     // which node owns this key per consistent hashing
	OwnerAddr    string `json:"owner_addr"`     // gRPC address of the owner node
	InCache      bool   `json:"in_cache"`       // is it currently in the in-process cache?
	CacheStatus  string `json:"cache_status"`   // "HIT" or "MISS"
	IsCoordinator bool  `json:"is_coordinator"` // is the owning node the current coordinator?
}

// CacheDebug handles GET /cache/debug/{key}
// Returns the full routing decision for a cache key.
func (d *DebugHandler) CacheDebug(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "key is required")
		return
	}

	// Find which node owns this key
	nodeID, addr, ok := d.ring.Get(key)
	ownerNode := "unknown"
	ownerAddr := ""
	if ok {
		ownerNode = nodeID
		ownerAddr = addr
	}

	// Check if the key is in the local in-process cache
	_, inCache, _ := d.cache.Get(r.Context(), key)
	cacheStatus := "MISS"
	if inCache {
		cacheStatus = "HIT"
	}

	resp := CacheDebugResponse{
		Key:           key,
		OwnerNode:     ownerNode,
		OwnerAddr:     ownerAddr,
		InCache:       inCache,
		CacheStatus:   cacheStatus,
		IsCoordinator: d.coordinator.IsLeader(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// NodeStatus holds the health and stats for one cache node.
type NodeStatus struct {
	NodeID        string `json:"node_id"`
	Healthy       bool   `json:"healthy"`
	IsCoordinator bool   `json:"is_coordinator"`
	EntryCount    int64  `json:"entry_count"`
	HitCount      int64  `json:"hit_count"`
	MissCount     int64  `json:"miss_count"`
	EvictionCount int64  `json:"eviction_count"`
	Error         string `json:"error,omitempty"`
}

// ClusterStatusResponse is the response body for GET /cluster/status.
type ClusterStatusResponse struct {
	Nodes       []NodeStatus `json:"nodes"`
	Coordinator string       `json:"coordinator"` // nodeID of current coordinator, "" if none
}

// ClusterStatus handles GET /cluster/status
// Aggregates health from all cache nodes and returns a single response.
func (d *DebugHandler) ClusterStatus(w http.ResponseWriter, r *http.Request) {
	nodes := d.ring.Nodes()
	statuses := make([]NodeStatus, 0, len(nodes))
	coordinator := ""

	for _, nodeID := range nodes {
		httpAddr, ok := d.nodeAddrs[nodeID]
		if !ok {
			statuses = append(statuses, NodeStatus{NodeID: nodeID, Error: "address not configured"})
			continue
		}

		status := fetchNodeHealth(r.Context(), nodeID, httpAddr)
		if status.IsCoordinator {
			coordinator = nodeID
		}
		statuses = append(statuses, status)
	}

	resp := ClusterStatusResponse{
		Nodes:       statuses,
		Coordinator: coordinator,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// fetchNodeHealth calls a cache node's /health endpoint and parses the response.
// Returns an unhealthy status if the node is unreachable.
func fetchNodeHealth(ctx context.Context, nodeID, httpAddr string) NodeStatus {
	url := fmt.Sprintf("http://%s/health", httpAddr)

	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
	if err != nil {
		return NodeStatus{NodeID: nodeID, Healthy: false, Error: err.Error()}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return NodeStatus{NodeID: nodeID, Healthy: false, Error: "unreachable"}
	}
	defer resp.Body.Close()

	var health struct {
		Healthy       bool   `json:"healthy"`
		NodeID        string `json:"node_id"`
		IsCoordinator bool   `json:"is_coordinator"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return NodeStatus{NodeID: nodeID, Healthy: false, Error: "bad response"}
	}

	// Also fetch stats for entry/hit/miss counts
	statsURL := fmt.Sprintf("http://%s/metrics", httpAddr)
	stats := fetchNodeStats(ctx, nodeID, statsURL)
	stats.NodeID = nodeID
	stats.Healthy = health.Healthy
	stats.IsCoordinator = health.IsCoordinator
	return stats
}

// fetchNodeStats fetches the plain-text metrics from a node and parses key values.
func fetchNodeStats(ctx context.Context, nodeID, metricsURL string) NodeStatus {
	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", metricsURL, nil)
	if err != nil {
		return NodeStatus{NodeID: nodeID}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return NodeStatus{NodeID: nodeID}
	}
	defer resp.Body.Close()

	// Parse the simple text metrics format from our stub /metrics endpoint
	var stats NodeStatus
	stats.NodeID = nodeID

	var hits, misses, evictions, entries int64
	fmt.Fscanf(resp.Body,
		"# cache stats for %*s\ncache_hits %d\ncache_misses %d\ncache_evictions %d\ncache_entries %d\n",
		&hits, &misses, &evictions, &entries)

	stats.HitCount = hits
	stats.MissCount = misses
	stats.EvictionCount = evictions
	stats.EntryCount = entries
	return stats
}

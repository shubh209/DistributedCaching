package cluster

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Sentinel errors returned by the coordinator.
// Callers check for these specific errors to decide how to respond.
var (
	// ErrNoCoordinator is returned when no Raft leader has been elected yet.
	// This happens during an election or immediately after a leader dies.
	// Callers should retry after a short backoff.
	ErrNoCoordinator = errors.New("cluster: no coordinator elected, try again shortly")

	// ErrRoutingMismatch is returned when a request arrives at a node that
	// doesn't own the key according to the consistent hashing ring.
	// This indicates a stale routing table and the request should be re-routed.
	ErrRoutingMismatch = errors.New("cluster: routing mismatch — request sent to wrong node")
)

// RouteResult is returned by Coordinator.Route and tells the caller
// where to send the cache operation.
type RouteResult struct {
	NodeID string // the node that owns this key
	Addr   string // gRPC address of that node (e.g. "node1:9001")
}

// Coordinator routes incoming cache requests to the correct cache node
// using the consistent hashing ring and node health information.
//
// It is the single point of routing in the cluster. The API layer calls
// Route(key) to get the responsible node's address, then sends the cache
// operation directly to that node via gRPC.
//
// The coordinator role is held by the current Raft leader. When a new
// leader is elected, it calls SetLeader to announce itself.
type Coordinator struct {
	mu      sync.RWMutex
	ring    *Ring                   // consistent hashing ring
	health  map[string]*NodeHealth  // nodeID → health tracker
	isLeader atomic.Bool            // true if this node is the current Raft leader
}

// NewCoordinator creates a Coordinator backed by the given ring.
// nodeHealthMap maps nodeID → NodeHealth for all nodes in the cluster.
func NewCoordinator(ring *Ring, nodeHealthMap map[string]*NodeHealth) *Coordinator {
	return &Coordinator{
		ring:   ring,
		health: nodeHealthMap,
	}
}

// Route determines which node is responsible for the given key and
// returns its address if it is currently available.
//
// Returns:
//   - (result, nil)         — node is healthy, send the request there
//   - ("", ErrNoCoordinator) — no leader elected, caller should retry
//   - ("", nil) with found=false — node is unavailable, treat as cache miss
//
// The "unavailable = cache miss" behaviour is intentional (ADR 0001):
// we do NOT reroute to another node because that node doesn't have the key.
// The caller falls through to the database instead.
func (c *Coordinator) Route(key string) (result RouteResult, available bool, err error) {
	// If this node is not the current Raft leader, reject the request.
	// During an election window there is no valid coordinator.
	if !c.isLeader.Load() {
		return RouteResult{}, false, ErrNoCoordinator
	}

	// Ask the ring which node owns this key
	nodeID, addr, ok := c.ring.Get(key)
	if !ok {
		// Ring is empty — no nodes registered
		return RouteResult{}, false, ErrNoCoordinator
	}

	// Check health of the responsible node
	c.mu.RLock()
	nodeHealth, exists := c.health[nodeID]
	c.mu.RUnlock()

	if !exists || !nodeHealth.IsAvailable() {
		// Node is down — return cache miss signal, NOT an error.
		// The caller (API layer) will fall through to PostgreSQL.
		return RouteResult{}, false, nil
	}

	return RouteResult{NodeID: nodeID, Addr: addr}, true, nil
}

// ValidateOwnership checks whether the given nodeID actually owns the key
// according to the consistent hashing ring. Used to detect routing mismatches
// where a request arrives at the wrong node.
func (c *Coordinator) ValidateOwnership(key, nodeID string) error {
	ownerID, _, ok := c.ring.Get(key)
	if !ok {
		return ErrNoCoordinator
	}
	if ownerID != nodeID {
		return ErrRoutingMismatch
	}
	return nil
}

// SetLeader marks this coordinator as the active Raft leader.
// Called by the Raft state machine when this node wins an election.
func (c *Coordinator) SetLeader(isLeader bool) {
	c.isLeader.Store(isLeader)
}

// IsLeader returns whether this node is currently the Raft leader.
func (c *Coordinator) IsLeader() bool {
	return c.isLeader.Load()
}

// UpdateNodeHealth updates the health status of a node.
// Called by the health-check goroutine after each probe.
func (c *Coordinator) UpdateNodeHealth(nodeID string, available bool) {
	c.mu.RLock()
	nodeHealth, exists := c.health[nodeID]
	c.mu.RUnlock()

	if !exists {
		return
	}

	if available {
		nodeHealth.MarkAvailable()
	} else {
		nodeHealth.MarkUnavailable()
	}
}

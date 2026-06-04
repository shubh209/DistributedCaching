package cluster

import (
	"sync"
	"time"
)

const healthProbeTimeout = 500 * time.Millisecond // requirement 3.7

// NodeHealth tracks the availability of a single cache node.
// A node is considered reachable if it responded to a health probe
// within the last healthProbeTimeout window.
type NodeHealth struct {
	mu          sync.RWMutex
	nodeID      string
	addr        string
	available   bool      // current availability status
	lastProbeAt time.Time // when the last probe was sent
	lastSeenAt  time.Time // when the node last responded successfully
}

// NewNodeHealth creates a NodeHealth tracker for a node.
// Nodes start as available — they're marked unavailable only after a failed probe.
func NewNodeHealth(nodeID, addr string) *NodeHealth {
	return &NodeHealth{
		nodeID:    nodeID,
		addr:      addr,
		available: true,
	}
}

// MarkAvailable marks the node as reachable and updates the last-seen timestamp.
// Called by the health-check goroutine when a probe succeeds.
func (n *NodeHealth) MarkAvailable() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.available = true
	n.lastSeenAt = time.Now()
}

// MarkUnavailable marks the node as unreachable.
// Called by the health-check goroutine when a probe times out.
func (n *NodeHealth) MarkUnavailable() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.available = false
}

// IsAvailable returns whether the node is currently considered reachable.
func (n *NodeHealth) IsAvailable() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.available
}

// Addr returns the node's gRPC address.
func (n *NodeHealth) Addr() string {
	return n.addr
}

// NodeID returns the node's identifier.
func (n *NodeHealth) NodeID() string {
	return n.nodeID
}

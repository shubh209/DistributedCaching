package cluster

import (
	"testing"
)

// --- Consistent hashing ring tests ---

func TestRing_Get_IsDeterministic(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")
	r.Add("node2", "node2:9002")
	r.Add("node3", "node3:9003")

	// Same key should always return the same node
	key := "product:123"
	nodeID1, _, _ := r.Get(key)
	nodeID2, _, _ := r.Get(key)
	nodeID3, _, _ := r.Get(key)

	if nodeID1 != nodeID2 || nodeID2 != nodeID3 {
		t.Errorf("ring.Get is not deterministic: got %s, %s, %s", nodeID1, nodeID2, nodeID3)
	}
}

func TestRing_Get_EmptyRing_ReturnsFalse(t *testing.T) {
	r := NewRing()
	_, _, ok := r.Get("any-key")
	if ok {
		t.Error("expected ok=false for empty ring")
	}
}

func TestRing_Add_ThenGet_ReturnsValidNode(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")

	nodeID, addr, ok := r.Get("product:456")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if nodeID != "node1" {
		t.Errorf("expected node1, got %s", nodeID)
	}
	if addr != "node1:9001" {
		t.Errorf("expected node1:9001, got %s", addr)
	}
}

func TestRing_Remove_MinimalRemapping(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")
	r.Add("node2", "node2:9002")
	r.Add("node3", "node3:9003")

	// Record assignments for a set of keys before removal
	keys := make([]string, 100)
	before := make(map[string]string)
	for i := 0; i < 100; i++ {
		keys[i] = string(rune(i + 33)) // printable ASCII chars as keys
		nodeID, _, _ := r.Get(keys[i])
		before[keys[i]] = nodeID
	}

	// Remove node2
	r.Remove("node2")

	// Count remapped keys
	remapped := 0
	for _, k := range keys {
		nodeID, _, _ := r.Get(k)
		if before[k] == "node2" {
			// Keys owned by node2 MUST remap — that's expected
			continue
		}
		if nodeID != before[k] {
			// Keys NOT owned by node2 should NOT remap
			remapped++
		}
	}

	if remapped > 0 {
		t.Errorf("consistent hashing remapped %d keys that should have stayed put", remapped)
	}
}

func TestRing_MultipleNodes_DistributesKeys(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")
	r.Add("node2", "node2:9002")
	r.Add("node3", "node3:9003")

	counts := map[string]int{}
	for i := 0; i < 300; i++ {
		// Use varied keys to get a distribution
		key := string([]byte{byte(i % 256), byte(i / 256)})
		nodeID, _, _ := r.Get(key)
		counts[nodeID]++
	}

	// With 150 virtual nodes each and 300 keys, each node should get ~100 keys.
	// We allow ±50% variance as a loose sanity check.
	for nodeID, count := range counts {
		if count < 50 || count > 150 {
			t.Errorf("node %s got %d keys out of 300 — distribution too uneven", nodeID, count)
		}
	}
}

// --- Coordinator tests ---

func TestCoordinator_Route_UnavailableNode_ReturnsMiss(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")

	health := map[string]*NodeHealth{
		"node1": NewNodeHealth("node1", "node1:9001"),
	}
	health["node1"].MarkUnavailable()

	c := NewCoordinator(r, health)
	c.SetLeader(true)

	_, available, err := c.Route("any-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if available {
		t.Error("expected available=false when node is unavailable")
	}
}

func TestCoordinator_Route_NoLeader_ReturnsError(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")
	c := NewCoordinator(r, map[string]*NodeHealth{})
	// Don't call SetLeader(true) — default is false

	_, _, err := c.Route("key")
	if err != ErrNoCoordinator {
		t.Errorf("expected ErrNoCoordinator, got %v", err)
	}
}

func TestCoordinator_Route_HealthyNode_ReturnsResult(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")

	health := map[string]*NodeHealth{
		"node1": NewNodeHealth("node1", "node1:9001"),
	}
	// node1 starts as available by default

	c := NewCoordinator(r, health)
	c.SetLeader(true)

	result, available, err := c.Route("any-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Fatal("expected available=true for healthy node")
	}
	if result.NodeID != "node1" {
		t.Errorf("expected node1, got %s", result.NodeID)
	}
}

func TestCoordinator_ValidateOwnership_WrongNode_ReturnsError(t *testing.T) {
	r := NewRing()
	r.Add("node1", "node1:9001")
	r.Add("node2", "node2:9002")

	c := NewCoordinator(r, map[string]*NodeHealth{})

	// Find out which node owns the key
	ownerID, _, _ := r.Get("product:99")
	// Pick the other node
	wrongNode := "node1"
	if ownerID == "node1" {
		wrongNode = "node2"
	}

	err := c.ValidateOwnership("product:99", wrongNode)
	if err != ErrRoutingMismatch {
		t.Errorf("expected ErrRoutingMismatch, got %v", err)
	}
}

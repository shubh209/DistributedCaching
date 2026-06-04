package eviction

import "time"

// lruNode is a single node in the doubly linked list.
// Each node holds a key and pointers to its neighbours.
type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

// LRU implements the EvictionPolicy interface using the classic
// doubly linked list + hash map combination.
//
// The list is ordered by recency of access:
//   - HEAD.next = most recently used key
//   - TAIL.prev = least recently used key  ← this is what gets evicted
//
// Both lookup and reordering are O(1).
type LRU struct {
	nodes map[string]*lruNode // key → node in the linked list (O(1) lookup)
	head  *lruNode            // sentinel head — never holds real data
	tail  *lruNode            // sentinel tail — never holds real data
}

// NewLRU creates a new LRU eviction policy.
// We use sentinel head and tail nodes so we never have to handle
// nil-pointer edge cases when inserting or removing nodes.
func NewLRU() *LRU {
	head := &lruNode{}
	tail := &lruNode{}
	head.next = tail
	tail.prev = head

	return &LRU{
		nodes: make(map[string]*lruNode),
		head:  head,
		tail:  tail,
	}
}

// OnAccess is called every time an existing key is read or updated.
// We move that key's node to the front (just after head) to mark it
// as the most recently used.
func (l *LRU) OnAccess(key string) {
	node, ok := l.nodes[key]
	if !ok {
		return
	}
	l.moveToFront(node)
}

// OnInsert is called when a brand new key is added to the cache.
// We create a new node and place it at the front of the list.
func (l *LRU) OnInsert(key string, _ time.Time) {
	node := &lruNode{key: key}
	l.nodes[key] = node
	l.insertAtFront(node)
}

// OnEvict is called when a key is removed from the cache for any reason
// (TTL expiry, explicit delete, or capacity eviction).
// We remove the node from the list and the map to avoid memory leaks.
func (l *LRU) OnEvict(key string) {
	node, ok := l.nodes[key]
	if !ok {
		return
	}
	l.removeNode(node)
	delete(l.nodes, key)
}

// Evict returns the key that should be removed — the least recently used one.
// That's always the node just before the tail sentinel.
// Returns ok=false only if the list is empty.
func (l *LRU) Evict() (string, bool) {
	// tail.prev is the least recently used node
	// If tail.prev == head, the list is empty (only sentinels remain)
	if l.tail.prev == l.head {
		return "", false
	}
	return l.tail.prev.key, true
}

// Name returns the policy identifier used for Prometheus metric labels.
func (l *LRU) Name() string {
	return "lru"
}

// --- internal linked list helpers ---

// insertAtFront places a node immediately after the head sentinel.
// Before: head ↔ A ↔ B ↔ tail
// After:  head ↔ node ↔ A ↔ B ↔ tail
func (l *LRU) insertAtFront(node *lruNode) {
	node.prev = l.head
	node.next = l.head.next
	l.head.next.prev = node
	l.head.next = node
}

// removeNode detaches a node from wherever it is in the list.
// Before: A ↔ node ↔ B
// After:  A ↔ B  (node is detached)
func (l *LRU) removeNode(node *lruNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// moveToFront removes a node from its current position and puts it at the front.
// This is called on every cache hit to mark the key as most recently used.
func (l *LRU) moveToFront(node *lruNode) {
	l.removeNode(node)
	l.insertAtFront(node)
}

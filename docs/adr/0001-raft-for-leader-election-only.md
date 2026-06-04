# ADR 0001: Use Raft for Leader Election Only, Not Write Replication

## Status
Accepted

## Context
The cache cluster needs a coordination mechanism. Two options were considered:
- **Raft for leader election only**: One coordinator node is elected. Cache data lives on specific nodes via consistent hashing. No data replication.
- **Raft for write replication**: Every cache write is committed to all 3 nodes before acknowledgement. Any node can serve any key.

## Decision
Use Raft exclusively for leader election. Cache data is not replicated across nodes.

## Consequences
- **Simpler implementation**: Log replication, commit indices, and snapshot management are out of scope.
- **Node failure = cache miss**: If a node dies, its keys miss and fall through to the origin until the node recovers. This is acceptable in a learning sandbox and is itself instructive — it demonstrates *why* replication exists.
- **Consistent hashing remains the key distribution mechanism**: Keys are owned by a single node, not spread across all nodes.
- **Trade-off acknowledged**: Full Raft replication would provide durability and availability at the cost of write latency and implementation complexity. That trade-off is documented here rather than implemented.

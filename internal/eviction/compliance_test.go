package eviction

import "github.com/user/distributed-caching-go/internal/shared"

// Compile-time assertions: if any policy stops satisfying the EvictionPolicy
// interface, this file will fail to compile — catching the error immediately
// rather than at runtime.
//
// This pattern is idiomatic Go for verifying interface compliance without
// writing a runtime test. The blank identifier _ discards the value;
// we only care that the assignment compiles.
var (
	_ shared.EvictionPolicy = (*LRU)(nil)
	_ shared.EvictionPolicy = (*LFU)(nil)
	_ shared.EvictionPolicy = (*TTLOnly)(nil)
	_ shared.EvictionPolicy = (*Random)(nil)
)

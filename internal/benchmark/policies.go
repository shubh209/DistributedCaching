package benchmark

// policies.go provides constructor shims so the benchmark package
// can create eviction policies without a circular import.
// (benchmark imports cache, cache imports eviction — fine.
//  benchmark importing eviction directly is also fine here.)

import "github.com/user/distributed-caching-go/internal/eviction"
import "github.com/user/distributed-caching-go/internal/shared"

func newLRU() shared.EvictionPolicy    { return eviction.NewLRU() }
func newLFU() shared.EvictionPolicy    { return eviction.NewLFU() }
func newTTLOnly() shared.EvictionPolicy { return eviction.NewTTLOnly() }
func newRandom() shared.EvictionPolicy  { return eviction.NewRandom() }

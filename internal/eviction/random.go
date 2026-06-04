package eviction

import (
	"math/rand"
	"time"
)

// Random implements the EvictionPolicy interface by evicting a uniformly
// random entry from the cache. It requires no extra data structure —
// it works directly with the key set it maintains internally.
//
// Random eviction is the simplest possible policy and serves as the
// benchmark baseline. Research shows it often performs within 10-20% of
// LRU for many real workloads, making it a useful comparison point.
type Random struct {
	keys []string        // flat slice of all current keys — sampled on Evict()
	rng  *rand.Rand      // seeded random number generator
}

// NewRandom creates a new Random eviction policy.
func NewRandom() *Random {
	return &Random{
		keys: make([]string, 0),
		// Seed with current time so each run produces different eviction sequences.
		// This matters for benchmark fairness — we don't want a fixed seed that
		// accidentally always picks the best (or worst) entry.
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// OnAccess is a no-op for Random — access patterns are completely ignored.
func (r *Random) OnAccess(_ string) {}

// OnInsert adds the new key to the key slice.
func (r *Random) OnInsert(key string, _ time.Time) {
	r.keys = append(r.keys, key)
}

// OnEvict removes the key from the slice.
// We use swap-and-truncate (O(1)) instead of slice re-allocation (O(n)):
//   - find the key's index
//   - swap it with the last element
//   - truncate the slice by 1
// Order doesn't matter for random eviction, so this is safe.
func (r *Random) OnEvict(key string) {
	for i, k := range r.keys {
		if k == key {
			// Swap with last element and truncate — O(1) removal
			r.keys[i] = r.keys[len(r.keys)-1]
			r.keys = r.keys[:len(r.keys)-1]
			return
		}
	}
}

// Evict picks a uniformly random key from the current key set.
// Returns ok=false only if there are no keys to evict.
func (r *Random) Evict() (string, bool) {
	if len(r.keys) == 0 {
		return "", false
	}
	// Pick a random index in [0, len(keys))
	i := r.rng.Intn(len(r.keys))
	return r.keys[i], true
}

// Name returns the policy identifier used for Prometheus metric labels.
func (r *Random) Name() string {
	return "random"
}

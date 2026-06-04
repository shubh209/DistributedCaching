package cluster

import "time"

// secondsToDuration converts an int64 TTL in seconds to a time.Duration.
// Used when translating gRPC SetRequest.ttl_seconds to the cache's Set method.
func secondsToDuration(seconds int64) time.Duration {
	return time.Duration(seconds) * time.Second
}

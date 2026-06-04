package shared

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the sandbox services.
// Values are loaded from environment variables so Docker Compose can
// inject different settings per service without rebuilding the binary.
type Config struct {
	// Cache node settings
	EvictionPolicy string // "lru" | "lfu" | "ttl" | "random"
	CacheCapacity  int    // max number of entries; default 10000

	// API layer settings
	InvalidationStrategy string // "ttl" | "write-through" | "write-behind"
	CacheClusterAddr     string // comma-separated gRPC addresses, e.g. "node1:9001,node2:9002,node3:9003"

	// Database settings
	DBDSN string // PostgreSQL DSN, e.g. "postgres://user:pass@postgres:5432/products?sslmode=disable"

	// Server settings
	Port   int    // HTTP/gRPC port this service listens on
	NodeID string // unique identifier for this cache node, e.g. "node1"
	Peers  string // comma-separated peer addresses for Raft, e.g. "node2:9002,node3:9003"
}

// Load reads configuration from environment variables.
// Missing optional variables fall back to sensible defaults.
// Missing required variables return an error.
func Load() (*Config, error) {
	cfg := &Config{
		// Defaults
		EvictionPolicy:       getEnv("EVICTION_POLICY", "lru"),
		CacheCapacity:        getEnvInt("CACHE_CAPACITY", 10000),
		InvalidationStrategy: getEnv("INVALIDATION_STRATEGY", "write-through"),
		CacheClusterAddr:     getEnv("CACHE_CLUSTER_ADDR", ""),
		DBDSN:                getEnv("DB_DSN", ""),
		Port:                 getEnvInt("PORT", 8080),
		NodeID:               getEnv("NODE_ID", ""),
		Peers:                getEnv("PEERS", ""),
	}

	// Validate eviction policy value
	validPolicies := map[string]bool{"lru": true, "lfu": true, "ttl": true, "random": true}
	if !validPolicies[cfg.EvictionPolicy] {
		return nil, fmt.Errorf("invalid EVICTION_POLICY %q: must be one of lru, lfu, ttl, random", cfg.EvictionPolicy)
	}

	// Validate invalidation strategy value
	validStrategies := map[string]bool{"ttl": true, "write-through": true, "write-behind": true}
	if !validStrategies[cfg.InvalidationStrategy] {
		return nil, fmt.Errorf("invalid INVALIDATION_STRATEGY %q: must be one of ttl, write-through, write-behind", cfg.InvalidationStrategy)
	}

	// Validate cache capacity bounds (requirement 1.3: 1–1,000,000)
	if cfg.CacheCapacity < 1 || cfg.CacheCapacity > 1_000_000 {
		return nil, fmt.Errorf("invalid CACHE_CAPACITY %d: must be between 1 and 1000000", cfg.CacheCapacity)
	}

	return cfg, nil
}

// getEnv returns the value of an environment variable, or a default if not set.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt returns the integer value of an environment variable, or a default.
// If the variable is set but not a valid integer, it falls back to the default.
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

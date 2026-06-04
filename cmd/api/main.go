package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/user/distributed-caching-go/internal/api"
	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/cluster"
	"github.com/user/distributed-caching-go/internal/db"
	"github.com/user/distributed-caching-go/internal/eviction"
	"github.com/user/distributed-caching-go/internal/invalidation"
	"github.com/user/distributed-caching-go/internal/shared"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Load config from environment variables
	cfg, err := shared.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	log.Printf("api: invalidation strategy=%s", cfg.InvalidationStrategy)

	// --- Build cache (in-memory standalone or cluster-backed) ---
	// For now: standalone in-memory cache with LRU.
	// When the cluster gRPC transport is ready (Task 8 wiring),
	// this will be replaced with a cluster-backed cache client.
	policy := eviction.NewLRU()
	cacheInstance := cache.New(cfg.CacheCapacity, policy)

	// If cluster addresses are provided, wrap with coordinator routing.
	// This allows the API to be started with or without a cluster.
	var cacheBackend shared.Cache = cacheInstance
	if cfg.CacheClusterAddr != "" {
		ring := cluster.NewRing()
		// TODO: parse cluster addresses and add nodes to ring (done in Task 8 wiring)
		// For now fall back to local cache
		log.Printf("api: cluster addr configured (%s) — cluster routing will be enabled after proto-gen", cfg.CacheClusterAddr)
		_ = ring
	}

	// --- Connect to PostgreSQL ---
	if cfg.DBDSN == "" {
		log.Fatal("DB_DSN environment variable is required")
	}
	database, err := db.New(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatalf("api: db connect error: %v", err)
	}
	defer database.Close()
	log.Printf("api: connected to postgres")

	// --- Build invalidation strategy ---
	strategy := buildInvalidationStrategy(cfg.InvalidationStrategy)

	// --- Start HTTP server ---
	port := cfg.Port
	if port == 0 {
		port = 8081
	}
	srv := api.NewServer(port, cacheBackend, database, strategy)
	log.Printf("api: listening on :%d", port)

	if err := srv.Start(ctx); err != nil {
		log.Printf("api: server stopped: %v", err)
	}
}

// buildInvalidationStrategy constructs the strategy from the config string.
func buildInvalidationStrategy(name string) shared.InvalidationStrategy {
	switch name {
	case "write-through":
		return invalidation.NewWriteThrough()
	case "write-behind":
		return invalidation.NewWriteBehind()
	default:
		return invalidation.NewTTL() // default: ttl (no-op on write)
	}
}

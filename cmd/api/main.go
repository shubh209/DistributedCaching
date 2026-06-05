package main

import (
	"context"
	"log"
	"os/signal"
	"strings"
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

	cfg, err := shared.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	log.Printf("api: invalidation strategy=%s", cfg.InvalidationStrategy)

	// Build in-process cache
	policy := eviction.NewLRU()
	cacheInstance := cache.New(cfg.CacheCapacity, policy)
	var cacheBackend shared.Cache = cacheInstance

	// Build ring and coordinator for debug endpoints
	ring := cluster.NewRing()
	nodeHTTPAddrs := make(map[string]string) // nodeID → HTTP metrics addr

	if cfg.CacheClusterAddr != "" {
		for _, addr := range strings.Split(cfg.CacheClusterAddr, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			// addr is e.g. "cache-node-1:9001" (gRPC port)
			// HTTP metrics port = gRPC port + 100 (9001→9101, 9002→9102, 9003→9103)
			parts := strings.SplitN(addr, ":", 2)
			if len(parts) != 2 {
				continue
			}
			nodeID := strings.ReplaceAll(parts[0], "cache-", "") // "node1"
			ring.Add(nodeID, addr)

			// Derive HTTP metrics address from gRPC address
			var grpcPort int
			if _, err := strings.NewReader(parts[1]).Read(nil); err == nil {
				switch parts[1] {
				case "9001":
					nodeHTTPAddrs[nodeID] = parts[0] + ":9101"
				case "9002":
					nodeHTTPAddrs[nodeID] = parts[0] + ":9102"
				case "9003":
					nodeHTTPAddrs[nodeID] = parts[0] + ":9103"
				default:
					nodeHTTPAddrs[nodeID] = parts[0] + ":9101"
				}
			}
			_ = grpcPort
		}
		log.Printf("api: cluster configured with %d nodes", len(ring.Nodes()))
	}

	// Build a minimal coordinator (leader=true since API uses local cache in sandbox)
	healthMap := make(map[string]*cluster.NodeHealth)
	for _, nodeID := range ring.Nodes() {
		addr, _ := ring.NodeAddr(nodeID)
		healthMap[nodeID] = cluster.NewNodeHealth(nodeID, addr)
	}
	coord := cluster.NewCoordinator(ring, healthMap)
	coord.SetLeader(true) // API acts as coordinator for debug purposes

	// Connect to PostgreSQL
	if cfg.DBDSN == "" {
		log.Fatal("DB_DSN environment variable is required")
	}
	database, err := db.New(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatalf("api: db connect error: %v", err)
	}
	defer database.Close()
	log.Printf("api: connected to postgres")

	// Build invalidation strategy
	strategy := buildInvalidationStrategy(cfg.InvalidationStrategy)

	// Build debug handler
	debugH := api.NewDebugHandler(cacheBackend, ring, coord, nodeHTTPAddrs)

	// Start HTTP server
	port := cfg.Port
	if port == 0 {
		port = 8081
	}
	srv := api.NewServer(port, cacheBackend, database, strategy, debugH)
	log.Printf("api: listening on :%d", port)

	if err := srv.Start(ctx); err != nil {
		log.Printf("api: server stopped: %v", err)
	}
}

func buildInvalidationStrategy(name string) shared.InvalidationStrategy {
	switch name {
	case "write-through":
		return invalidation.NewWriteThrough()
	case "write-behind":
		return invalidation.NewWriteBehind()
	default:
		return invalidation.NewTTL()
	}
}

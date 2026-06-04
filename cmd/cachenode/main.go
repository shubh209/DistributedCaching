package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/user/distributed-caching-go/internal/cache"
	"github.com/user/distributed-caching-go/internal/cluster"
	"github.com/user/distributed-caching-go/internal/eviction"
	"github.com/user/distributed-caching-go/internal/raft"
	"github.com/user/distributed-caching-go/internal/shared"
)

func main() {
	// --- Parse command-line flags ---
	nodeID := flag.String("id", "", "unique node identifier (e.g. node1)")
	grpcPort := flag.Int("port", 9001, "gRPC server port")
	metricsPort := flag.Int("metrics-port", 9101, "HTTP metrics/health port")
	peersFlag := flag.String("peers", "", "comma-separated peer addresses (e.g. node2:9002,node3:9003)")
	stateDir := flag.String("state-dir", "/tmp/raft", "directory for Raft persistent state files")
	flag.Parse()

	if *nodeID == "" {
		log.Fatal("--id is required (e.g. --id=node1)")
	}

	// --- Load config from environment variables ---
	cfg, err := shared.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// --- Build eviction policy from config ---
	policy := buildEvictionPolicy(cfg.EvictionPolicy)
	log.Printf("[%s] eviction policy: %s, capacity: %d", *nodeID, policy.Name(), cfg.CacheCapacity)

	// --- Create in-memory cache ---
	c := cache.New(cfg.CacheCapacity, policy)

	// --- Parse peers ---
	peers := parsePeers(*peersFlag)

	// --- Create Raft node ---
	if err := os.MkdirAll(*stateDir, 0700); err != nil {
		log.Fatalf("cannot create state dir: %v", err)
	}
	raftNode, err := raft.NewRaftNode(*nodeID, peers, *stateDir)
	if err != nil {
		log.Fatalf("raft init error: %v", err)
	}

	// --- Create coordinator and consistent hashing ring ---
	ring := cluster.NewRing()
	// Register self
	selfAddr := fmt.Sprintf("%s:%d", *nodeID, *grpcPort)
	ring.Add(*nodeID, selfAddr)
	// Register peers
	for _, p := range peers {
		ring.Add(p.ID, p.Addr)
	}

	healthMap := make(map[string]*cluster.NodeHealth)
	for _, nodeID := range ring.Nodes() {
		addr, _ := ring.NodeAddr(nodeID)
		healthMap[nodeID] = cluster.NewNodeHealth(nodeID, addr)
	}

	coordinator := cluster.NewCoordinator(ring, healthMap)

	// --- Wire Raft leader changes to coordinator ---
	// When this node wins or loses an election, update the coordinator.
	go func() {
		for leaderID := range raftNode.LeaderCh() {
			isLeader := leaderID == *nodeID
			coordinator.SetLeader(isLeader)
			if isLeader {
				log.Printf("[%s] became coordinator (term %d)", *nodeID, raftNode.CurrentTerm())
			} else {
				log.Printf("[%s] new leader: %s", *nodeID, leaderID)
			}
		}
	}()

	// --- Create gRPC cache server ---
	cacheServer := cluster.NewCacheServer(
		*nodeID,
		c,
		policy,
		coordinator.IsLeader,
	)

	// --- Start gRPC transport and Raft ---
	transport := cluster.NewGRPCTransport(500 * time.Millisecond)
	raftNode.Start(transport)
	log.Printf("[%s] raft started, listening on gRPC :%d", *nodeID, *grpcPort)

	// --- Start HTTP server for /health and /metrics ---
	go startHTTPServer(*nodeID, *metricsPort, cacheServer, coordinator)

	// --- Wait for shutdown signal ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("[%s] received %s — shutting down", *nodeID, sig)
	raftNode.Stop()
}

// buildEvictionPolicy constructs the EvictionPolicy from the config string.
func buildEvictionPolicy(name string) shared.EvictionPolicy {
	switch name {
	case "lfu":
		return eviction.NewLFU()
	case "ttl":
		return eviction.NewTTLOnly()
	case "random":
		return eviction.NewRandom()
	default:
		return eviction.NewLRU() // default: lru
	}
}

// parsePeers splits a comma-separated peer list into PeerConfig structs.
// Expected format: "node2:9002,node3:9003"
func parsePeers(peersFlag string) []raft.PeerConfig {
	if peersFlag == "" {
		return nil
	}
	parts := strings.Split(peersFlag, ",")
	peers := make([]raft.PeerConfig, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Extract nodeID from address: "node2:9002" → ID="node2", Addr="node2:9002"
		colonIdx := strings.Index(p, ":")
		id := p
		if colonIdx > 0 {
			id = p[:colonIdx]
		}
		peers = append(peers, raft.PeerConfig{ID: id, Addr: p})
	}
	return peers
}

// startHTTPServer starts the HTTP server for /health and /metrics.
// /health returns 200 OK when the node is ready to serve.
// /metrics will be wired to Prometheus in Task 15.
func startHTTPServer(nodeID string, port int, srv *cluster.CacheServer, coord *cluster.Coordinator) {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		healthy, isCoord := srv.Health(r.Context())
		if !healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"healthy":false,"node_id":%q}`, nodeID)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"healthy":true,"node_id":%q,"is_coordinator":%v}`, nodeID, isCoord)
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Prometheus metrics endpoint — wired in Task 15.
		// Returns a stub response until then.
		hits, misses, evictions, entries, policyName := srv.Stats(r.Context())
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# cache stats for %s (policy=%s)\n", nodeID, policyName)
		fmt.Fprintf(w, "cache_hits %d\ncache_misses %d\ncache_evictions %d\ncache_entries %d\n",
			hits, misses, evictions, entries)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[%s] HTTP server listening on %s", nodeID, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[%s] HTTP server error: %v", nodeID, err)
	}
}

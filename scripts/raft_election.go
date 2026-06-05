//go:build ignore

package main

import (
	"fmt"
	"time"

	"github.com/user/distributed-caching-go/internal/raft"
)

type inMemoryTransport struct {
	nodes map[string]*raft.RaftNode
}

func (t *inMemoryTransport) RequestVote(peerAddr string, req raft.RequestVoteRequest) (raft.RequestVoteResponse, error) {
	node, ok := t.nodes[peerAddr]
	if !ok {
		return raft.RequestVoteResponse{}, fmt.Errorf("node %s not found", peerAddr)
	}
	return node.HandleRequestVote(req), nil
}

func (t *inMemoryTransport) AppendEntries(peerAddr string, req raft.AppendEntriesRequest) (raft.AppendEntriesResponse, error) {
	node, ok := t.nodes[peerAddr]
	if !ok {
		return raft.AppendEntriesResponse{}, fmt.Errorf("node %s not found", peerAddr)
	}
	return node.HandleAppendEntries(req), nil
}

func safeStop(n *raft.RaftNode) {
	defer func() { recover() }()
	n.Stop()
}

func makeCluster(stateDir string) ([]*raft.RaftNode, *inMemoryTransport) {
	transport := &inMemoryTransport{nodes: make(map[string]*raft.RaftNode)}
	ids := []string{"node1", "node2", "node3"}
	nodes := make([]*raft.RaftNode, 3)

	for i, id := range ids {
		peers := make([]raft.PeerConfig, 0, 2)
		for _, pid := range ids {
			if pid != id {
				peers = append(peers, raft.PeerConfig{ID: pid, Addr: pid})
			}
		}
		node, err := raft.NewRaftNode(id, peers, stateDir)
		if err != nil {
			panic(err)
		}
		nodes[i] = node
		transport.nodes[id] = node
	}
	return nodes, transport
}

func waitForLeader(leaderChs []<-chan string, timeout time.Duration) (string, time.Duration, bool) {
	start := time.Now()
	deadline := time.After(timeout)
	merged := make(chan string, len(leaderChs))
	for _, ch := range leaderChs {
		go func(c <-chan string) {
			select {
			case id := <-c:
				merged <- id
			case <-time.After(timeout):
			}
		}(ch)
	}
	select {
	case id := <-merged:
		return id, time.Since(start), true
	case <-deadline:
		return "", timeout, false
	}
}

func main() {
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("  Raft Leader Election Timing Test (in-memory transport)")
	fmt.Println("═══════════════════════════════════════════════════════════════")

	// ── Test 1: Initial election ──────────────────────────────────────────
	fmt.Println("\n▶ Test 1: Initial election (3 nodes, fresh start)")
	{
		nodes, transport := makeCluster(fmt.Sprintf("/tmp/raft1_%d", time.Now().UnixNano()))
		chs := make([]<-chan string, 3)
		for i, n := range nodes {
			chs[i] = n.LeaderCh()
		}
		for _, n := range nodes {
			n.Start(transport)
		}
		leader, elapsed, ok := waitForLeader(chs, 5*time.Second)
		if ok {
			fmt.Printf("  Leader: %s elected in %v\n", leader, elapsed.Round(time.Millisecond))
			fmt.Printf("  ✦ Initial election time: %v\n", elapsed.Round(time.Millisecond))
		} else {
			fmt.Println("  No leader within 5s")
		}
		for _, n := range nodes {
			safeStop(n)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// ── Test 2: Re-election after leader failure ───────────────────────────
	fmt.Println("\n▶ Test 2: Re-election after leader failure (5 trials)")
	{
		times := make([]time.Duration, 0, 5)

		for trial := 0; trial < 5; trial++ {
			stateDir := fmt.Sprintf("/tmp/raft2_%d_%d", time.Now().UnixNano(), trial)
			nodes, transport := makeCluster(stateDir)
			chs := make([]<-chan string, 3)
			for i, n := range nodes {
				chs[i] = n.LeaderCh()
			}
			for _, n := range nodes {
				n.Start(transport)
			}

			// Wait for first leader
			firstLeader, _, ok := waitForLeader(chs, 5*time.Second)
			if !ok {
				fmt.Printf("    Trial %d: no initial leader\n", trial+1)
				for _, n := range nodes {
					safeStop(n)
				}
				continue
			}

			// Find and kill the leader node
			leaderIdx := -1
			for i, n := range nodes {
				if n.State() == raft.Leader {
					leaderIdx = i
					break
				}
			}
			// Fallback: match by ID
			if leaderIdx == -1 {
				ids := []string{"node1", "node2", "node3"}
				for i, id := range ids {
					if id == firstLeader {
						leaderIdx = i
						break
					}
				}
			}

			if leaderIdx == -1 {
				fmt.Printf("    Trial %d: could not identify leader node\n", trial+1)
				for _, n := range nodes {
					safeStop(n)
				}
				continue
			}

			// Kill the leader
			safeStop(nodes[leaderIdx])

			// Collect channels of surviving nodes
			survivorChs := make([]<-chan string, 0, 2)
			for i, ch := range chs {
				if i != leaderIdx {
					survivorChs = append(survivorChs, ch)
				}
			}

			// Measure re-election time by polling node states
			reStart := time.Now()
			reElected := false
			var newLeader string
			for elapsed := time.Duration(0); elapsed < 5*time.Second; elapsed += 20*time.Millisecond {
				time.Sleep(20 * time.Millisecond)
				for i, n := range nodes {
					if i == leaderIdx { continue }
					if n.State() == raft.Leader {
						newLeader = fmt.Sprintf("node%d", i+1)
						reElected = true
						break
					}
				}
				if reElected { break }
			}
			reElapsed := time.Since(reStart)

			if reElected {
				times = append(times, reElapsed)
				fmt.Printf("    Trial %d: %s elected in %v (after %s failed)\n",
					trial+1, newLeader, reElapsed.Round(time.Millisecond), firstLeader)
			} else {
				fmt.Printf("    Trial %d: no re-election within 5s\n", trial+1)
			}

			for _, n := range nodes {
				safeStop(n)
			}
			time.Sleep(50 * time.Millisecond)
		}

		if len(times) > 0 {
			var sum time.Duration
			min, max := times[0], times[0]
			for _, t := range times {
				sum += t
				if t < min {
					min = t
				}
				if t > max {
					max = t
				}
			}
			avg := sum / time.Duration(len(times))
			fmt.Printf("\n  ✦ Re-election timing across %d trials:\n", len(times))
			fmt.Printf("     Min: %v\n", min.Round(time.Millisecond))
			fmt.Printf("     Max: %v\n", max.Round(time.Millisecond))
			fmt.Printf("     Avg: %v\n", avg.Round(time.Millisecond))
		}
	}

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("  ✅ Raft election test complete")
	fmt.Println("═══════════════════════════════════════════════════════════════")
}

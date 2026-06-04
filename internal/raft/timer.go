package raft

import (
	"math/rand"
	"time"
)

const (
	// electionTimeoutMin and Max define the randomised window for election timeouts.
	// Randomisation is critical: if all nodes had the same timeout they would all
	// start elections simultaneously, causing split votes where nobody wins.
	electionTimeoutMin = 150 * time.Millisecond
	electionTimeoutMax = 300 * time.Millisecond

	// heartbeatInterval is how often the leader sends AppendEntries heartbeats.
	// Must be significantly shorter than electionTimeoutMin so followers don't
	// time out while the leader is healthy.
	heartbeatInterval = 50 * time.Millisecond
)

// randomElectionTimeout returns a duration uniformly distributed in [150ms, 300ms].
// Each call returns a different value, ensuring nodes rarely time out simultaneously.
func randomElectionTimeout() time.Duration {
	delta := electionTimeoutMax - electionTimeoutMin
	jitter := time.Duration(rand.Int63n(int64(delta)))
	return electionTimeoutMin + jitter
}

// Start begins the Raft election loop. This is the heart of the state machine.
// It runs in a goroutine and drives all state transitions.
//
// The loop has two modes depending on current state:
//   - Follower/Candidate: waits for election timeout, then starts an election
//   - Leader: sends heartbeats every 50ms to prevent followers from timing out
func (n *RaftNode) Start(transport Transport) {
	go n.runLoop(transport)
}

// runLoop is the main Raft event loop. It runs until Stop() is called.
func (n *RaftNode) runLoop(transport Transport) {
	electionTimer := time.NewTimer(randomElectionTimeout())
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer electionTimer.Stop()
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-n.stopCh:
			return

		case <-heartbeatTicker.C:
			n.mu.Lock()
			state := n.state
			n.mu.Unlock()
			if state == Leader {
				n.sendHeartbeats(transport)
			}

		case <-electionTimer.C:
			n.mu.Lock()
			state := n.state
			// If we received a heartbeat recently, the leader is still alive.
			// Skip the election and reset the timer.
			sinceHeartbeat := time.Since(n.lastHeartbeat)
			n.mu.Unlock()

			if state == Leader {
				electionTimer.Reset(randomElectionTimeout())
				continue
			}

			if sinceHeartbeat < electionTimeoutMin {
				// Recent heartbeat — leader is alive, don't start election
				electionTimer.Reset(randomElectionTimeout())
				continue
			}

			// No recent heartbeat — start an election
			n.startElection(transport)
			electionTimer.Reset(randomElectionTimeout())
		}
	}
}

// startElection transitions the node to Candidate, increments the term,
// and sends RequestVote RPCs to all peers in parallel.
//
// Election outcome:
//   - Majority votes received  → become Leader
//   - Higher term seen         → revert to Follower (someone else won)
//   - Timeout fires again      → start another election (split vote recovery)
func (n *RaftNode) startElection(transport Transport) {
	n.mu.Lock()
	n.becomeCandidate()
	term := n.ps.CurrentTerm
	candidateID := n.id
	peers := n.peers
	n.mu.Unlock()

	votes := 1 // vote for ourselves
	majority := (len(peers)+1)/2 + 1 // e.g. with 3 nodes: majority = 2

	// Request votes from all peers concurrently.
	// We collect results via a channel to avoid blocking.
	type voteResult struct {
		granted bool
		term    int64
	}
	resultCh := make(chan voteResult, len(peers))

	for _, peer := range peers {
		go func(p PeerConfig) {
			resp, err := transport.RequestVote(p.Addr, RequestVoteRequest{
				Term:        term,
				CandidateID: candidateID,
			})
			if err != nil {
				resultCh <- voteResult{granted: false}
				return
			}
			resultCh <- voteResult{granted: resp.VoteGranted, term: resp.Term}
		}(peer)
	}

	// Collect vote results. Stop early if we win or lose definitively.
	for i := 0; i < len(peers); i++ {
		result := <-resultCh

		n.mu.Lock()
		// If we see a higher term, we've been superseded — step down immediately.
		if result.term > n.ps.CurrentTerm {
			n.becomeFollower(result.term)
			n.mu.Unlock()
			return
		}

		if result.granted {
			votes++
			if votes >= majority && n.state == Candidate {
				// We won the election!
				n.becomeLeader()
				n.mu.Unlock()
				// Send immediate heartbeats to assert leadership right away,
				// rather than waiting for the next heartbeat tick.
				n.sendHeartbeats(transport)
				return
			}
		}
		n.mu.Unlock()
	}
	// If we get here, we didn't get enough votes (split vote).
	// The election timer will fire again with a new random timeout,
	// and we'll try again in a new term.
}

// sendHeartbeats sends AppendEntries (heartbeat) RPCs to all peers.
// This resets their election timers and keeps them as followers.
func (n *RaftNode) sendHeartbeats(transport Transport) {
	n.mu.Lock()
	term := n.ps.CurrentTerm
	leaderID := n.id
	peers := n.peers
	n.mu.Unlock()

	for _, peer := range peers {
		go func(p PeerConfig) {
			resp, err := transport.AppendEntries(p.Addr, AppendEntriesRequest{
				Term:     term,
				LeaderID: leaderID,
			})
			if err != nil {
				return
			}
			// If a peer has a higher term, we've been superseded — step down.
			n.mu.Lock()
			if resp.Term > n.ps.CurrentTerm {
				n.becomeFollower(resp.Term)
			}
			n.mu.Unlock()
		}(peer)
	}
}



package raft

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// NodeState represents the three possible states a Raft node can be in.
type NodeState int

const (
	Follower  NodeState = iota // passive — waiting for heartbeats from a leader
	Candidate                  // actively requesting votes to become leader
	Leader                     // elected leader — sends heartbeats, acts as coordinator
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "follower"
	case Candidate:
		return "candidate"
	case Leader:
		return "leader"
	default:
		return "unknown"
	}
}

// PeerConfig holds the connection details for a peer node.
type PeerConfig struct {
	ID   string // e.g. "node2"
	Addr string // gRPC address e.g. "node2:9002"
}

// persistentState is the subset of Raft state that must survive crashes.
// If a node restarts, it must not vote twice in the same term.
// We save this to a JSON file after every update.
type persistentState struct {
	CurrentTerm int64  `json:"current_term"` // monotonically increasing election counter
	VotedFor    string `json:"voted_for"`    // nodeID we voted for this term; "" = not yet voted
}

// RaftNode is the core Raft state machine.
// It manages state transitions, election timers, and RPC handling.
type RaftNode struct {
	mu    sync.Mutex // protects all fields below
	id    string     // this node's unique identifier e.g. "node1"
	peers []PeerConfig

	// Raft state
	state    NodeState
	leaderID string // who the current leader is (empty if unknown)

	// Persistent state (survives restarts)
	ps       persistentState
	stateDir string // directory where persistent state file is saved

	// Channels for coordination
	stopCh      chan struct{} // closed to shut down background goroutines
	leaderCh    chan string   // sends leaderID when a new leader is elected

	// lastHeartbeat tracks when we last received a valid AppendEntries from the leader.
	// The runLoop compares this against the election timeout to decide when to start
	// an election. Updated by HandleAppendEntries under the lock.
	lastHeartbeat time.Time
}

// NewRaftNode creates a Raft node. Call Start() to begin the election loop.
func NewRaftNode(id string, peers []PeerConfig, stateDir string) (*RaftNode, error) {
	n := &RaftNode{
		id:       id,
		peers:    peers,
		state:    Follower,
		stateDir: stateDir,
		stopCh:   make(chan struct{}),
		leaderCh: make(chan string, 1),
	}

	// Load persistent state from disk if it exists.
	// This prevents a restarted node from voting twice in the same term.
	if err := n.loadState(); err != nil {
		return nil, fmt.Errorf("raft: failed to load persistent state: %w", err)
	}

	return n, nil
}

// --- Persistent state management ---

func (n *RaftNode) statePath() string {
	return fmt.Sprintf("%s/raft-%s.json", n.stateDir, n.id)
}

// loadState reads persistent state from disk.
// If the file doesn't exist, start fresh (term=0, no vote) — safe for a new node.
func (n *RaftNode) loadState() error {
	data, err := os.ReadFile(n.statePath())
	if os.IsNotExist(err) {
		// Fresh start — no previous state
		n.ps = persistentState{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &n.ps)
}

// saveState writes persistent state to disk.
// Must be called (under the lock) whenever currentTerm or votedFor changes.
func (n *RaftNode) saveState() error {
	data, err := json.Marshal(n.ps)
	if err != nil {
		return err
	}
	// Write to a temp file then rename — atomic on most filesystems.
	// Prevents a crash mid-write from corrupting the state file.
	tmp := n.statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, n.statePath())
}

// --- State transition helpers (must be called with lock held) ---

// becomeFollower transitions to Follower state.
// Called when we see a higher term in any RPC, or when we lose an election.
func (n *RaftNode) becomeFollower(term int64) {
	n.state = Follower
	if term > n.ps.CurrentTerm {
		n.ps.CurrentTerm = term
		n.ps.VotedFor = "" // new term = new vote
		_ = n.saveState()
	}
}

// becomeCandidate transitions to Candidate state and starts a new election term.
// Called when the election timer fires and we haven't heard from a leader.
func (n *RaftNode) becomeCandidate() {
	n.state = Candidate
	n.ps.CurrentTerm++   // increment term for the new election
	n.ps.VotedFor = n.id // vote for ourselves
	_ = n.saveState()
}

// becomeLeader transitions to Leader state.
// Called when we receive votes from a majority of nodes.
func (n *RaftNode) becomeLeader() {
	n.state = Leader
	n.leaderID = n.id
	// Notify observers (e.g. the coordinator) that a new leader is elected.
	select {
	case n.leaderCh <- n.id:
	default:
	}
}

// --- Public accessors ---

// State returns the current node state.
func (n *RaftNode) State() NodeState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state
}

// CurrentTerm returns the current election term.
func (n *RaftNode) CurrentTerm() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ps.CurrentTerm
}

// LeaderCh returns a channel that receives the leader ID whenever a new
// leader is elected. The coordinator listens on this to enable routing.
func (n *RaftNode) LeaderCh() <-chan string {
	return n.leaderCh
}

// Stop shuts down the Raft node's background goroutines.
func (n *RaftNode) Stop() {
	close(n.stopCh)
}

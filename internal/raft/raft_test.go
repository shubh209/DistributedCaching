package raft

import (
	"testing"
)

// --- RPC handler unit tests ---
// These test the Raft RPC logic in isolation without real network connections.

// mockTransport is a no-op transport used for unit tests.
// Real network transport is tested in integration tests (Task 21).
type mockTransport struct{}

func (m *mockTransport) RequestVote(_ string, _ RequestVoteRequest) (RequestVoteResponse, error) {
	return RequestVoteResponse{}, nil
}
func (m *mockTransport) AppendEntries(_ string, _ AppendEntriesRequest) (AppendEntriesResponse, error) {
	return AppendEntriesResponse{}, nil
}

func newTestNode(t *testing.T, id string) *RaftNode {
	n, err := NewRaftNode(id, []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestHandleRequestVote_HigherTerm_GrantsVote verifies that a node grants a vote
// when the candidate has a higher or equal term and the node hasn't voted yet.
func TestHandleRequestVote_HigherTerm_GrantsVote(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	resp := n.HandleRequestVote(RequestVoteRequest{
		Term:        1,
		CandidateID: "node2",
	})

	if !resp.VoteGranted {
		t.Error("expected vote to be granted for higher term candidate")
	}
	if n.ps.VotedFor != "node2" {
		t.Errorf("expected votedFor='node2', got '%s'", n.ps.VotedFor)
	}
}

// TestHandleRequestVote_LowerTerm_DeniesVote verifies that a node rejects
// a vote request from a candidate with a lower term.
func TestHandleRequestVote_LowerTerm_DeniesVote(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Set our term higher than the candidate's
	n.ps.CurrentTerm = 5

	resp := n.HandleRequestVote(RequestVoteRequest{
		Term:        3, // lower than our term=5
		CandidateID: "node2",
	})

	if resp.VoteGranted {
		t.Error("expected vote to be denied for lower term candidate")
	}
}

// TestHandleRequestVote_AlreadyVoted_DeniesSecondVote verifies that a node
// cannot vote twice in the same term (the double-vote prevention property).
func TestHandleRequestVote_AlreadyVoted_DeniesSecondVote(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Vote for node2 first
	resp1 := n.HandleRequestVote(RequestVoteRequest{Term: 1, CandidateID: "node2"})
	if !resp1.VoteGranted {
		t.Fatal("first vote should be granted")
	}

	// Try to vote for node3 in the same term — should be denied
	resp2 := n.HandleRequestVote(RequestVoteRequest{Term: 1, CandidateID: "node3"})
	if resp2.VoteGranted {
		t.Error("second vote in same term should be denied")
	}
}

// TestHandleAppendEntries_ValidLeader_AcceptsAndUpdatesLeader verifies that
// a follower accepts a heartbeat from a valid leader and records the leader ID.
func TestHandleAppendEntries_ValidLeader_Accepts(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	resp := n.HandleAppendEntries(AppendEntriesRequest{
		Term:     1,
		LeaderID: "node2",
	})

	if !resp.Success {
		t.Error("expected Success=true for valid leader heartbeat")
	}
	if n.leaderID != "node2" {
		t.Errorf("expected leaderID='node2', got '%s'", n.leaderID)
	}
}

// TestHandleAppendEntries_StaleTerm_Rejects verifies that a node rejects
// heartbeats from a leader with a lower term (stale/zombie leader).
func TestHandleAppendEntries_StaleTerm_Rejects(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n.ps.CurrentTerm = 5

	resp := n.HandleAppendEntries(AppendEntriesRequest{
		Term:     3, // stale — lower than our term=5
		LeaderID: "node2",
	})

	if resp.Success {
		t.Error("expected Success=false for stale leader heartbeat")
	}
}

// TestHandleAppendEntries_HigherTerm_StepsDown verifies that a node steps
// down from any state when it sees a higher term in an AppendEntries.
func TestHandleAppendEntries_HigherTerm_StepsDownToFollower(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Simulate being a candidate in term 2
	n.ps.CurrentTerm = 2
	n.state = Candidate

	n.HandleAppendEntries(AppendEntriesRequest{
		Term:     5, // higher term — new leader elected
		LeaderID: "node3",
	})

	if n.state != Follower {
		t.Errorf("expected state=Follower after seeing higher term, got %s", n.state)
	}
	if n.ps.CurrentTerm != 5 {
		t.Errorf("expected currentTerm=5, got %d", n.ps.CurrentTerm)
	}
}

// TestBecomeCandidate_IncrementsTermAndVotesSelf verifies that transitioning
// to Candidate increments the term and records a self-vote.
func TestBecomeCandidate_IncrementsTermAndVotesSelf(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	n.ps.CurrentTerm = 3
	n.becomeCandidate()
	n.mu.Unlock()

	if n.ps.CurrentTerm != 4 {
		t.Errorf("expected term=4 after becoming candidate, got %d", n.ps.CurrentTerm)
	}
	if n.ps.VotedFor != "node1" {
		t.Errorf("expected votedFor='node1' (self-vote), got '%s'", n.ps.VotedFor)
	}
	if n.state != Candidate {
		t.Errorf("expected state=Candidate, got %s", n.state)
	}
}

// TestBecomeFollower_HigherTerm_ClearsVote verifies that transitioning to
// Follower with a higher term clears the previous vote (new term = new vote).
func TestBecomeFollower_HigherTerm_ClearsVote(t *testing.T) {
	n, err := NewRaftNode("node1", []PeerConfig{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	n.ps.CurrentTerm = 2
	n.ps.VotedFor = "node2"
	n.becomeFollower(5) // higher term
	n.mu.Unlock()

	if n.ps.VotedFor != "" {
		t.Errorf("expected votedFor='' after new term, got '%s'", n.ps.VotedFor)
	}
	if n.ps.CurrentTerm != 5 {
		t.Errorf("expected term=5, got %d", n.ps.CurrentTerm)
	}
}

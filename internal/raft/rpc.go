package raft

import "time"

// --- RPC message types ---
// These are the Go structs for Raft's two RPC messages.
// They will be replaced by the generated protobuf types in Task 7,
// but are defined here so the Raft logic compiles independently.

// RequestVoteRequest is sent by a candidate to ask peers for their vote.
type RequestVoteRequest struct {
	Term        int64  // candidate's current term
	CandidateID string // who is asking for the vote
	// LastLogIndex and LastLogTerm are always 0 in our implementation
	// because we don't use log replication (ADR 0001).
}

// RequestVoteResponse is the peer's reply to a vote request.
type RequestVoteResponse struct {
	Term        int64 // peer's current term (so candidate can update itself)
	VoteGranted bool  // true if the peer voted for the candidate
}

// AppendEntriesRequest is sent by the leader as a heartbeat.
// In full Raft this also carries log entries, but we use it heartbeat-only.
type AppendEntriesRequest struct {
	Term     int64  // leader's current term
	LeaderID string // so followers know who the current leader is
	// No Entries field — we don't replicate data via Raft (ADR 0001)
}

// AppendEntriesResponse is the follower's reply to a heartbeat.
type AppendEntriesResponse struct {
	Term    int64 // follower's current term (so leader can detect if it's stale)
	Success bool  // true if the follower accepted the heartbeat
}

// --- Transport interface ---
// Transport abstracts the network layer so Raft logic can be tested
// without real gRPC connections. The real gRPC transport is implemented
// in Task 8 when we wire up the gRPC servers.

// Transport defines how a Raft node sends RPCs to its peers.
type Transport interface {
	RequestVote(peerAddr string, req RequestVoteRequest) (RequestVoteResponse, error)
	AppendEntries(peerAddr string, req AppendEntriesRequest) (AppendEntriesResponse, error)
}

// --- RPC handlers ---
// These are called when THIS node RECEIVES an RPC from a peer.

// HandleRequestVote processes an incoming vote request from a candidate.
//
// Vote is granted if ALL of the following are true:
//  1. Candidate's term >= our current term (they're not behind)
//  2. We haven't already voted for someone else this term
func (n *RaftNode) HandleRequestVote(req RequestVoteRequest) RequestVoteResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	// If the candidate is in an older term, reject immediately.
	if req.Term < n.ps.CurrentTerm {
		return RequestVoteResponse{Term: n.ps.CurrentTerm, VoteGranted: false}
	}

	// If we see a higher term, update our term and revert to follower.
	// This handles the case where we were a leader or candidate in an old term.
	if req.Term > n.ps.CurrentTerm {
		n.becomeFollower(req.Term)
	}

	// Grant vote if we haven't voted for anyone else this term.
	alreadyVoted := n.ps.VotedFor != "" && n.ps.VotedFor != req.CandidateID
	if alreadyVoted {
		return RequestVoteResponse{Term: n.ps.CurrentTerm, VoteGranted: false}
	}

	// Grant the vote
	n.ps.VotedFor = req.CandidateID
	_ = n.saveState()

	return RequestVoteResponse{Term: n.ps.CurrentTerm, VoteGranted: true}
}

// HandleAppendEntries processes an incoming heartbeat from the leader.
//
// On a valid heartbeat:
//   - Reset our election timer (we heard from a valid leader)
//   - Update our term if the leader's term is higher
//   - Record who the current leader is
//   - Revert to Follower if we were a Candidate (leader was elected)
func (n *RaftNode) HandleAppendEntries(req AppendEntriesRequest) AppendEntriesResponse {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Reject heartbeats from stale leaders (lower term = old leader, ignore them)
	if req.Term < n.ps.CurrentTerm {
		return AppendEntriesResponse{Term: n.ps.CurrentTerm, Success: false}
	}

	// Valid heartbeat from current or newer leader — update our state
	if req.Term > n.ps.CurrentTerm {
		n.becomeFollower(req.Term)
	}

	// If we were a candidate and a leader appeared, step back to follower.
	// This handles split vote recovery — someone else won.
	if n.state == Candidate {
		n.state = Follower
	}

	// Record the current leader so the coordinator knows who to defer to
	n.leaderID = req.LeaderID

	// Reset the election timer by updating a timestamp.
	// The runLoop checks this to decide if the timer should fire.
	n.lastHeartbeat = time.Now()

	return AppendEntriesResponse{Term: n.ps.CurrentTerm, Success: true}
}

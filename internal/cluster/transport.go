package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/user/distributed-caching-go/internal/raft"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// grpcTransport implements raft.Transport using real gRPC connections.
// It is the bridge between the Raft state machine logic (pure Go)
// and the network (gRPC calls to peer nodes).
//
// In tests, the mock transport from raft_test.go is used instead.
// In production (Docker Compose), this real transport is used.
type grpcTransport struct {
	dialTimeout time.Duration
}

// NewGRPCTransport creates a transport that dials peers with the given timeout.
func NewGRPCTransport(dialTimeout time.Duration) raft.Transport {
	return &grpcTransport{dialTimeout: dialTimeout}
}

// RequestVote dials the peer and calls its RaftService.RequestVote RPC.
// Returns the peer's response or an error if the call fails.
func (t *grpcTransport) RequestVote(peerAddr string, req raft.RequestVoteRequest) (raft.RequestVoteResponse, error) {
	conn, err := t.dial(peerAddr)
	if err != nil {
		return raft.RequestVoteResponse{}, fmt.Errorf("transport: dial %s: %w", peerAddr, err)
	}
	defer conn.Close()

	// Once proto-gen runs, this will use the generated RaftServiceClient.
	// For now we document the intended call pattern:
	//
	//   client := raftv1.NewRaftServiceClient(conn)
	//   ctx, cancel := context.WithTimeout(context.Background(), t.dialTimeout)
	//   defer cancel()
	//   resp, err := client.RequestVote(ctx, &raftv1.RequestVoteRequest{
	//       Term:        req.Term,
	//       CandidateId: req.CandidateID,
	//   })
	//
	// The stub below keeps the code compilable before proto-gen runs.
	_ = conn
	return raft.RequestVoteResponse{}, fmt.Errorf("transport: proto-gen required — run make proto-gen")
}

// AppendEntries dials the peer and calls its RaftService.AppendEntries RPC.
func (t *grpcTransport) AppendEntries(peerAddr string, req raft.AppendEntriesRequest) (raft.AppendEntriesResponse, error) {
	conn, err := t.dial(peerAddr)
	if err != nil {
		return raft.AppendEntriesResponse{}, fmt.Errorf("transport: dial %s: %w", peerAddr, err)
	}
	defer conn.Close()

	// Once proto-gen runs:
	//   client := raftv1.NewRaftServiceClient(conn)
	//   ctx, cancel := context.WithTimeout(context.Background(), t.dialTimeout)
	//   defer cancel()
	//   resp, err := client.AppendEntries(ctx, &raftv1.AppendEntriesRequest{
	//       Term:     req.Term,
	//       LeaderId: req.LeaderID,
	//   })
	_ = conn
	return raft.AppendEntriesResponse{}, fmt.Errorf("transport: proto-gen required — run make proto-gen")
}

// dial opens a gRPC connection to the given address.
// We use insecure credentials because this is a learning sandbox
// on a private Docker network — not a production system.
func (t *grpcTransport) dial(addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), t.dialTimeout)
	defer cancel()
	return grpc.DialContext(ctx, addr, //nolint:staticcheck
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

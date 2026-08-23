package grpc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"golang.org/x/crypto/hkdf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Cross-replica command routing.
//
// A runner's control stream terminates in one process. Everything that sends a
// runner a command goes through ConnectionManager.SendCommand, and until now
// that resolved the runner against an in-process map and reported "runner not
// connected" for anything held elsewhere. The registry (migration 014) records
// which process holds each stream; this file is the one hop that gets the
// command there.
//
// The routing lives here rather than at the eleven call sites deliberately.
// Every sender - the task manager, the session manager, the permission
// manager, the tunnel router, the admin stream handler - already calls this
// method, so putting the hop behind it fixes all of them without touching any
// of them, including the files this change may not edit.

// peerRequestTimeout bounds one hop. SendCommand has no context to inherit -
// the signature is shared by four interfaces across three packages - so the
// hop carries its own deadline. It is short: the peer's own work is a
// non-blocking channel send.
const peerRequestTimeout = 5 * time.Second

// peerCredentialMetadataKey carries the peer credential. Deliberately not
// x-runner-token: the two are different authorities on the same listener.
const peerCredentialMetadataKey = "x-marionette-peer"

// peerCredentialInfo is the HKDF context. Binding the derivation to a purpose
// string means the peer credential cannot be replayed as anything else derived
// from the same master key.
const peerCredentialInfo = "marionette-internal-router-v1"

// ErrReplicaUnreachable reports that the runner is held by another replica
// which could not be reached, or refused the command.
//
// It is deliberately distinct from ErrRunnerNotFound: "nobody has this runner"
// and "the process that has it is not answering" call for different operator
// responses, and callers that log the error should not conflate them.
var ErrReplicaUnreachable = errors.New("replica holding the runner is unreachable")

// RunnerLocator answers which process holds a runner's control stream.
// core.ReplicaRegistry implements it.
type RunnerLocator interface {
	// Locate reports the peer holding runnerID. False means nobody holds it,
	// this process holds it, or the holder's heartbeat has expired - in every
	// one of those cases there is no hop to make.
	Locate(runnerID string) (peer RunnerPeer, ok bool)
}

// RunnerPeer is another replica, addressed.
type RunnerPeer struct {
	ReplicaID string
	Addr      string
}

// PeerCredential is the shared secret a replica presents to its peers.
//
// It is derived from MARIONETTE_MASTER_KEY, which every replica necessarily
// already shares - they serve the same admin API - so cross-replica routing
// adds no new required configuration. A runner never holds it, which is what
// makes it safe to serve this method on the port runners dial.
type PeerCredential string

// DerivePeerCredential derives the peer credential from the master key.
// An empty master key yields an empty credential, which disables the internal
// router: it refuses to serve and refuses to dial, rather than accepting
// anyone. A single-process deployment never notices, because it never hops.
func DerivePeerCredential(masterKey string) PeerCredential {
	if masterKey == "" {
		return ""
	}

	reader := hkdf.New(sha256.New, []byte(masterKey), nil, []byte(peerCredentialInfo))
	out := make([]byte, 32)
	if _, err := io.ReadFull(reader, out); err != nil {
		// hkdf.New's reader cannot fail for 32 bytes of SHA-256 output; the
		// branch exists so a future change cannot silently produce a short
		// credential.
		return ""
	}
	return PeerCredential(hex.EncodeToString(out))
}

// Equal compares credentials in constant time.
func (c PeerCredential) Equal(other string) bool {
	if c == "" || other == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c), []byte(other)) == 1
}

// authenticate is what the interceptor calls. Kept as a method so the HMAC
// import is used from one place and the rule is stated once.
func (c PeerCredential) authenticate(ctx context.Context) error {
	if c == "" {
		return status.Error(codes.Unimplemented,
			"cross-replica routing is not configured on this server (no master key)")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get(peerCredentialMetadataKey)
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing peer credential")
	}
	if !hmac.Equal([]byte(c), []byte(values[0])) {
		return status.Error(codes.Unauthenticated, "invalid peer credential")
	}
	return nil
}

// CommandForwarder hands a command to the replica holding the runner.
type CommandForwarder interface {
	Forward(peer RunnerPeer, runnerID string, cmd *pb.ServerCommand) error
}

// PeerForwarder is the gRPC client half of the hop.
//
// Connections are cached per address: a replica talks to the same handful of
// peers for its whole life, and dialling per command would put a TCP handshake
// in front of every cross-replica ExecuteTask.
type PeerForwarder struct {
	credential PeerCredential
	originID   string
	dialOpts   []grpc.DialOption
	logger     *zap.Logger

	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

// NewPeerForwarder builds the forwarder. dialOpts default to insecure
// credentials, which matches the listener when TLS is off; a deployment with
// mTLS on :9090 passes its own.
func NewPeerForwarder(cred PeerCredential, originID string, logger *zap.Logger, dialOpts ...grpc.DialOption) *PeerForwarder {
	if len(dialOpts) == 0 {
		dialOpts = []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	return &PeerForwarder{
		credential: cred,
		originID:   originID,
		dialOpts:   dialOpts,
		logger:     logger,
		conns:      make(map[string]*grpc.ClientConn),
	}
}

// Forward sends one command to one peer and maps its answer back onto the
// error vocabulary the local send already uses.
//
// It never retries. The hop preserves at-most-once delivery, which is not a
// nicety: the agent has no run-id ledger, so a second ExecuteTask for the same
// run executes the prompt again.
func (f *PeerForwarder) Forward(peer RunnerPeer, runnerID string, cmd *pb.ServerCommand) error {
	if f.credential == "" {
		return fmt.Errorf("%w: no peer credential configured (set MARIONETTE_MASTER_KEY)", ErrReplicaUnreachable)
	}

	conn, err := f.connect(peer.Addr)
	if err != nil {
		return fmt.Errorf("%w: dialling %s: %w", ErrReplicaUnreachable, peer.Addr, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), peerRequestTimeout)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, peerCredentialMetadataKey, string(f.credential))

	resp, err := pb.NewInternalRouterServiceClient(conn).DeliverCommand(ctx, &pb.DeliverCommandRequest{
		RunnerId:        runnerID,
		Command:         cmd,
		OriginReplicaId: f.originID,
	})
	if err != nil {
		// The peer may be gone rather than merely busy; drop the cached
		// connection so the next attempt redials instead of reusing a dead
		// one.
		f.drop(peer.Addr)
		return fmt.Errorf("%w: %s: %w", ErrReplicaUnreachable, peer.ReplicaID, err)
	}

	switch resp.GetStatus() {
	case pb.DeliveryStatus_DELIVERY_STATUS_DELIVERED:
		return nil
	case pb.DeliveryStatus_DELIVERY_STATUS_NOT_CONNECTED:
		// The registry was stale. This is the same fact a local miss reports,
		// so it gets the same error and the caller's existing compensation
		// runs unchanged.
		return ErrRunnerNotFound
	case pb.DeliveryStatus_DELIVERY_STATUS_QUEUE_FULL:
		return ErrCommandQueueFull
	case pb.DeliveryStatus_DELIVERY_STATUS_DISCONNECTED:
		return ErrRunnerDisconnected
	default:
		return fmt.Errorf("%w: %s returned %s: %s",
			ErrReplicaUnreachable, peer.ReplicaID, resp.GetStatus(), resp.GetMessage())
	}
}

func (f *PeerForwarder) connect(addr string) (*grpc.ClientConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if conn, ok := f.conns[addr]; ok {
		return conn, nil
	}

	conn, err := grpc.NewClient(addr, f.dialOpts...)
	if err != nil {
		return nil, err
	}
	f.conns[addr] = conn
	return conn, nil
}

func (f *PeerForwarder) drop(addr string) {
	f.mu.Lock()
	conn, ok := f.conns[addr]
	delete(f.conns, addr)
	f.mu.Unlock()

	if ok {
		_ = conn.Close()
	}
}

// Close releases every cached peer connection.
func (f *PeerForwarder) Close() {
	f.mu.Lock()
	conns := f.conns
	f.conns = make(map[string]*grpc.ClientConn)
	f.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
}

// InternalRouterService is the server half of the hop: it writes a command to
// a stream this process holds.
type InternalRouterService struct {
	pb.UnimplementedInternalRouterServiceServer

	connManager *ConnectionManager
	credential  PeerCredential
	metrics     *RoutingMetrics
	logger      *zap.Logger
}

// NewInternalRouterService builds the service. Nil metrics is fine.
func NewInternalRouterService(
	cm *ConnectionManager,
	cred PeerCredential,
	metrics *RoutingMetrics,
	logger *zap.Logger,
) *InternalRouterService {
	return &InternalRouterService{connManager: cm, credential: cred, metrics: metrics, logger: logger}
}

// DeliverCommand writes a command to a runner this process holds.
//
// It resolves the runner against the local connection map and nothing else.
// The sender already consulted the registry; a second lookup here would only
// add a way for the two to disagree, and the local map is the authority on
// what this process can actually write to.
func (s *InternalRouterService) DeliverCommand(
	ctx context.Context,
	req *pb.DeliverCommandRequest,
) (*pb.DeliverCommandResponse, error) {
	if err := s.credential.authenticate(ctx); err != nil {
		return nil, err
	}

	if req.GetRunnerId() == "" || req.GetCommand() == nil {
		return nil, status.Error(codes.InvalidArgument, "runner_id and command are required")
	}

	err := s.connManager.sendLocal(req.GetRunnerId(), req.GetCommand())
	switch {
	case err == nil:
		s.logger.Debug("delivered a forwarded command",
			zap.String("runner_id", req.GetRunnerId()),
			zap.String("origin_replica_id", req.GetOriginReplicaId()),
		)
		return &pb.DeliverCommandResponse{Status: pb.DeliveryStatus_DELIVERY_STATUS_DELIVERED}, nil
	case errors.Is(err, ErrRunnerNotFound):
		// The routing pointer was stale: the runner moved or hung up between
		// the sender's lookup and this call. Reported, not hidden - the sender
		// turns it into the same "runner not connected" its callers already
		// compensate for.
		//
		// This is the registry disagreeing with reality, which is the one
		// number that says whether the design's assumptions hold. It should be
		// near zero.
		s.metrics.conflictSeen()
		return &pb.DeliverCommandResponse{
			Status:  pb.DeliveryStatus_DELIVERY_STATUS_NOT_CONNECTED,
			Message: "this replica does not hold the runner",
		}, nil
	case errors.Is(err, ErrCommandQueueFull):
		return &pb.DeliverCommandResponse{
			Status:  pb.DeliveryStatus_DELIVERY_STATUS_QUEUE_FULL,
			Message: err.Error(),
		}, nil
	case errors.Is(err, ErrRunnerDisconnected):
		return &pb.DeliverCommandResponse{
			Status:  pb.DeliveryStatus_DELIVERY_STATUS_DISCONNECTED,
			Message: err.Error(),
		}, nil
	default:
		return nil, status.Errorf(codes.Internal, "delivering command: %v", err)
	}
}

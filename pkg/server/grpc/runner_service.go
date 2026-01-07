// Package grpc provides the gRPC server for runner communication.
package grpc

import (
	"context"
	"encoding/json"
	"io"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RunnerManagerInterface defines the interface for runner lifecycle management.
// This interface is implemented by core.RunnerManager.
type RunnerManagerInterface interface {
	// OnConnect is called when a runner connects.
	OnConnect(ctx context.Context, runnerID string) error
	// OnDisconnect is called when a runner disconnects.
	OnDisconnect(ctx context.Context, runnerID string) error
	// OnHeartbeat is called when a heartbeat is received from a runner.
	OnHeartbeat(ctx context.Context, runnerID string, hb *pb.Heartbeat) error
	// SetStatus updates a runner's status.
	SetStatus(ctx context.Context, runnerID, status string) error
}

// MessageRouterInterface defines the interface for routing runner messages.
type MessageRouterInterface interface {
	// HandleMessage routes a message from a runner to the appropriate handler.
	HandleMessage(ctx context.Context, runnerID string, msg *pb.RunnerMessage) error
}

// Default log batch configuration.
const (
	DefaultLogBatchSize = 100
)

// RunnerService implements the RunnerServiceServer interface.
type RunnerService struct {
	pb.UnimplementedRunnerServiceServer
	logger           *zap.Logger
	store            store.Store
	tokenSvc         *auth.RunnerTokenService
	connManager      *ConnectionManager
	runnerManager    RunnerManagerInterface
	router           MessageRouterInterface
	registry         *core.RunnerRegistry
	logSubscriberMgr core.LogSubscriberManagerInterface
	tunnelMgr        *core.TunnelManager
	frameHub         *core.FrameHub
}

// RunnerServiceOption is a functional option for RunnerService.
type RunnerServiceOption func(*RunnerService)

// NewRunnerService creates a new RunnerService with the given options.
func NewRunnerService(logger *zap.Logger, opts ...RunnerServiceOption) *RunnerService {
	svc := &RunnerService{
		logger: logger,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// WithStore sets the store for the RunnerService.
func WithStore(s store.Store) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.store = s
	}
}

// WithTokenService sets the token service for the RunnerService.
func WithTokenService(ts *auth.RunnerTokenService) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.tokenSvc = ts
	}
}

// WithConnectionManager sets the connection manager for the RunnerService.
func WithConnectionManager(cm *ConnectionManager) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.connManager = cm
	}
}

// WithRunnerManager sets the runner manager for the RunnerService.
func WithRunnerManager(rm RunnerManagerInterface) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.runnerManager = rm
	}
}

// WithRouter sets the message router for the RunnerService.
func WithRouter(r MessageRouterInterface) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.router = r
	}
}

// WithRegistry sets the runner registry for the RunnerService.
func WithRegistry(reg *core.RunnerRegistry) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.registry = reg
	}
}

// WithLogSubscriberManager sets the log subscriber manager for the RunnerService.
func WithLogSubscriberManager(lsm core.LogSubscriberManagerInterface) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.logSubscriberMgr = lsm
	}
}

// RegisterRunner handles runner registration.
// Validates the runner token and creates/updates the runner in the database.
func (s *RunnerService) RegisterRunner(ctx context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	s.logger.Info("RegisterRunner called",
		zap.String("name", req.GetName()),
		zap.String("hostname", req.GetHostname()),
	)

	// Check if registry is configured
	if s.registry == nil {
		s.logger.Error("registry not configured")
		return &pb.RegisterRunnerResponse{
			Accepted: false,
			Message:  "server configuration error: registry not configured",
		}, status.Error(codes.Internal, "registry not configured")
	}

	// Build registration request
	regReq := &core.RegisterRequest{
		Token:        req.GetToken(),
		Name:         req.GetName(),
		Hostname:     req.GetHostname(),
		SandboxMode:  req.GetSandboxMode(),
		SandboxTypes: req.GetSandboxTypes(),
		Capabilities: req.GetCapabilities(),
		Labels:       req.GetLabels(),
	}

	// Register via registry
	result, err := s.registry.Register(ctx, regReq)
	if err != nil {
		s.logger.Warn("runner registration failed",
			zap.String("name", req.GetName()),
			zap.Error(err),
		)
		return &pb.RegisterRunnerResponse{
			Accepted: false,
			Message:  err.Error(),
		}, status.Errorf(codes.InvalidArgument, "registration failed: %v", err)
	}

	msg := "runner registered"
	if !result.IsNew {
		msg = "runner re-registered"
	}

	s.logger.Info(msg,
		zap.String("runner_id", result.RunnerID),
		zap.String("pool_name", result.PoolName),
		zap.Bool("is_new", result.IsNew),
	)

	return &pb.RegisterRunnerResponse{
		RunnerId: result.RunnerID,
		Accepted: true,
		Message:  msg,
	}, nil
}

// GetRunnerStatus returns the status of a runner.
func (s *RunnerService) GetRunnerStatus(ctx context.Context, req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
	s.logger.Debug("GetRunnerStatus called",
		zap.String("runner_id", req.RunnerId),
	)

	// First check if runner is connected (live status from ConnectionManager)
	if s.connManager != nil {
		if conn, exists := s.connManager.Get(req.RunnerId); exists {
			return &pb.RunnerStatus{
				RunnerId: req.RunnerId,
				Status:   conn.Status,
			}, nil
		}
	}

	// Runner not connected - check database for last known status
	if s.store != nil {
		runner, err := s.store.GetRunner(ctx, req.RunnerId)
		if err != nil {
			s.logger.Warn("failed to get runner status",
				zap.String("runner_id", req.RunnerId),
				zap.Error(err),
			)
			return &pb.RunnerStatus{
				RunnerId: req.RunnerId,
				Status:   "unknown",
			}, nil
		}
		return &pb.RunnerStatus{
			RunnerId: req.RunnerId,
			Status:   runner.Status,
		}, nil
	}

	// No connManager or store configured
	return &pb.RunnerStatus{
		RunnerId: req.RunnerId,
		Status:   "unknown",
	}, nil
}

// createConnection creates a new RunnerConnection from a runner and stream.
func (s *RunnerService) createConnection(runner *store.Runner, stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) *RunnerConnection {
	return &RunnerConnection{
		RunnerID:    runner.ID,
		Name:        runner.Name,
		Hostname:    runner.Hostname,
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
		commandCh:   make(chan *pb.ServerCommand, commandBufferSize),
		stream:      stream,
	}
}

// StreamLogs handles the log upload stream from runners.
// Receives log entries, batches them for efficient persistence, and broadcasts to subscribers.
func (s *RunnerService) StreamLogs(stream grpc.ClientStreamingServer[pb.StreamLogsMessage, pb.StreamLogsResponse]) error {
	ctx := stream.Context()

	var runnerID string
	var sessionID string
	var logsReceived, logsStored, logsDropped int64

	// Batch for efficient inserts
	batch := make([]*store.Log, 0, DefaultLogBatchSize)

	// Flush helper function
	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		if s.store == nil {
			s.logger.Warn("store not configured, dropping logs",
				zap.Int("count", len(batch)),
			)
			logsDropped += int64(len(batch))
			batch = batch[:0]
			return
		}

		if err := s.store.CreateLogs(ctx, batch); err != nil {
			s.logger.Error("batch log insert failed",
				zap.Error(err),
				zap.Int("count", len(batch)),
			)
			logsDropped += int64(len(batch))
		} else {
			logsStored += int64(len(batch))
			// Broadcast to subscribers
			if s.logSubscriberMgr != nil {
				for _, log := range batch {
					s.logSubscriberMgr.Broadcast(log)
				}
			}
		}
		batch = batch[:0]
	}

	s.logger.Debug("StreamLogs stream opened")

	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// Normal stream close
				flushBatch() // Flush remaining logs
				s.logger.Debug("StreamLogs stream closed normally",
					zap.String("runner_id", runnerID),
					zap.Int64("logs_received", logsReceived),
					zap.Int64("logs_stored", logsStored),
					zap.Int64("logs_dropped", logsDropped),
				)
				return stream.SendAndClose(&pb.StreamLogsResponse{
					LogsReceived: logsReceived,
					LogsStored:   logsStored,
					LogsDropped:  logsDropped,
				})
			}
			// Abnormal error
			flushBatch() // Try to flush what we have
			s.logger.Error("StreamLogs stream error",
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
			return status.Errorf(codes.Internal, "stream error: %v", err)
		}

		// Handle init message (first message)
		if init := msg.GetInit(); init != nil {
			runnerID = init.GetRunnerId()
			sessionID = init.GetSessionId()
			s.logger.Debug("StreamLogs init received",
				zap.String("runner_id", runnerID),
				zap.String("session_id", sessionID),
				zap.String("task_id", init.GetTaskId()),
			)
			// TODO: Validate runner is authorized for this session
			continue
		}

		// Handle log entry
		if entry := msg.GetLogEntry(); entry != nil {
			logsReceived++

			// Convert to store.Log
			log := s.convertLogEntry(runnerID, entry)
			batch = append(batch, log)

			// Flush when batch is full
			if len(batch) >= DefaultLogBatchSize {
				flushBatch()
			}
		}
	}
}

// convertLogEntry converts a protobuf LogEntry to a store.Log.
func (s *RunnerService) convertLogEntry(runnerID string, entry *pb.LogEntry) *store.Log {
	// Convert metadata map to JSON
	var metadata json.RawMessage = []byte("{}")
	if len(entry.GetMetadata()) > 0 {
		if jsonBytes, err := json.Marshal(entry.GetMetadata()); err == nil {
			metadata = jsonBytes
		}
	}

	// Use entry's runner_id if provided, otherwise use stream's runner_id
	entryRunnerID := entry.GetRunnerId()
	if entryRunnerID == "" {
		entryRunnerID = runnerID
	}

	return &store.Log{
		ID:        id.Log(),
		SessionID: entry.GetSessionId(),
		TaskID:    entry.GetTaskId(),
		RunID:     entry.GetRunId(),
		RunnerID:  entryRunnerID,
		Stream:    entry.GetStream(),
		Level:     entry.GetLevel(),
		Content:   entry.GetContent(),
		Sequence:  entry.GetSequence(),
		TenantID:  stringPtrOrNil(entry.GetTenantId()),
		Metadata:  metadata,
		CreatedAt: time.Now(),
	}
}

// stringPtrOrNil returns a pointer to the string if non-empty, otherwise nil.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

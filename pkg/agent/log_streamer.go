package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// LogStreamer abstracts log streaming to the server.
// It handles the gRPC client streaming for logs.
type LogStreamer interface {
	// Start initializes a new log stream with the server.
	// Must be called before Send.
	Start(ctx context.Context, init *pb.StreamLogsInit) error

	// Send sends a log entry to the server.
	// Returns an error if the stream is not active.
	Send(entry *pb.LogEntry) error

	// Close closes the stream and returns the server's response.
	Close() (*pb.StreamLogsResponse, error)

	// IsActive returns true if the stream is currently active.
	IsActive() bool
}

// GRPCLogStreamer implements LogStreamer using gRPC client streaming.
type GRPCLogStreamer struct {
	client   pb.RunnerServiceClient
	runnerID string
	logger   *zap.Logger

	mu       sync.Mutex
	stream   pb.RunnerService_StreamLogsClient
	active   atomic.Bool
	sequence atomic.Int64
}

// NewGRPCLogStreamer creates a new GRPCLogStreamer.
func NewGRPCLogStreamer(client pb.RunnerServiceClient, runnerID string, logger *zap.Logger) *GRPCLogStreamer {
	return &GRPCLogStreamer{
		client:   client,
		runnerID: runnerID,
		logger:   logger.Named("log-streamer"),
	}
}

// Start initializes a new log stream with the server.
func (s *GRPCLogStreamer) Start(ctx context.Context, init *pb.StreamLogsInit) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active.Load() {
		return ErrStreamAlreadyActive
	}

	// Create the stream with metadata
	stream, err := s.client.StreamLogs(ctx, grpc.WaitForReady(true))
	if err != nil {
		return err
	}

	// Copy init message to avoid modifying caller's data
	initCopy := &pb.StreamLogsInit{
		SessionId: init.SessionId,
		TaskId:    init.TaskId,
		RunId:     init.RunId,
		RunnerId:  s.runnerID,
	}

	// Send init message first
	initMsg := &pb.StreamLogsMessage{
		Payload: &pb.StreamLogsMessage_Init{
			Init: initCopy,
		},
	}
	if err := stream.Send(initMsg); err != nil {
		return err
	}

	s.stream = stream
	s.active.Store(true)
	s.sequence.Store(0)

	s.logger.Debug("log stream started",
		zap.String("session_id", init.SessionId),
		zap.String("task_id", init.TaskId),
		zap.String("run_id", init.RunId),
	)

	return nil
}

// Send sends a log entry to the server.
func (s *GRPCLogStreamer) Send(entry *pb.LogEntry) error {
	// Quick check without lock for fast path
	if !s.active.Load() {
		return ErrStreamNotActive
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Re-check under lock to avoid race with Close()
	if !s.active.Load() || s.stream == nil {
		return ErrStreamNotActive
	}

	// Set sequence and timestamp
	entry.Sequence = s.sequence.Add(1)
	entry.TimestampUnixMs = time.Now().UnixMilli()
	entry.RunnerId = s.runnerID

	msg := &pb.StreamLogsMessage{
		Payload: &pb.StreamLogsMessage_LogEntry{
			LogEntry: entry,
		},
	}

	return s.stream.Send(msg)
}

// Close closes the stream and returns the server's response.
func (s *GRPCLogStreamer) Close() (*pb.StreamLogsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active.Load() {
		return &pb.StreamLogsResponse{}, nil
	}

	s.active.Store(false)

	if s.stream == nil {
		return &pb.StreamLogsResponse{}, nil
	}

	// Close send direction and receive response
	resp, err := s.stream.CloseAndRecv()
	s.stream = nil

	if err != nil {
		s.logger.Warn("error closing log stream", zap.Error(err))
		return nil, err
	}

	s.logger.Debug("log stream closed",
		zap.Int64("logs_received", resp.LogsReceived),
		zap.Int64("logs_stored", resp.LogsStored),
		zap.Int64("logs_dropped", resp.LogsDropped),
	)

	return resp, nil
}

// IsActive returns true if the stream is currently active.
func (s *GRPCLogStreamer) IsActive() bool {
	return s.active.Load()
}

// Verify GRPCLogStreamer implements LogStreamer.
var _ LogStreamer = (*GRPCLogStreamer)(nil)

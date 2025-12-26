package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
)

// LogStreamerConfig holds configuration for the log streamer.
type LogStreamerConfig struct {
	// BatchSize is the maximum number of log entries per batch.
	BatchSize int
	// FlushInterval is how often to flush buffered logs.
	FlushInterval time.Duration
	// BufferSize is the size of the internal buffer channel.
	BufferSize int
}

// DefaultLogStreamerConfig returns sensible defaults.
func DefaultLogStreamerConfig() LogStreamerConfig {
	return LogStreamerConfig{
		BatchSize:     100,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    1000,
	}
}

// LogStreamer handles streaming logs from the executor to the server.
type LogStreamer struct {
	client   *Client
	config   LogStreamerConfig
	logger   *zap.Logger
	runnerID string
	tenantID string

	// Current task context
	mu        sync.RWMutex
	sessionID string
	taskID    string
	runID     string
	sequence  atomic.Int64

	// Buffering
	buffer   chan *pb.LogEntry
	stopC    chan struct{}
	stoppedC chan struct{}
	running  bool
}

// NewLogStreamer creates a new log streamer.
func NewLogStreamer(client *Client, runnerID, tenantID string, logger *zap.Logger) *LogStreamer {
	return NewLogStreamerWithConfig(client, runnerID, tenantID, DefaultLogStreamerConfig(), logger)
}

// NewLogStreamerWithConfig creates a new log streamer with custom configuration.
func NewLogStreamerWithConfig(client *Client, runnerID, tenantID string, config LogStreamerConfig, logger *zap.Logger) *LogStreamer {
	return &LogStreamer{
		client:   client,
		config:   config,
		logger:   logger.Named("log-streamer"),
		runnerID: runnerID,
		tenantID: tenantID,
		buffer:   make(chan *pb.LogEntry, config.BufferSize),
	}
}

// SetTask sets the current task context for log entries.
func (s *LogStreamer) SetTask(sessionID, taskID, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
	s.taskID = taskID
	s.runID = runID
	s.sequence.Store(0)
}

// ClearTask clears the current task context.
func (s *LogStreamer) ClearTask() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = ""
	s.taskID = ""
	s.runID = ""
}

// Start begins the log streaming loop.
func (s *LogStreamer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.stopC = make(chan struct{})
	s.stoppedC = make(chan struct{})
	s.mu.Unlock()

	go s.streamLoop(ctx)
	return nil
}

// Stop stops the log streaming loop.
func (s *LogStreamer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopC)
	s.mu.Unlock()

	<-s.stoppedC
}

// HandleOutput implements executor.OutputHandler.
func (s *LogStreamer) HandleOutput(stream string, data []byte) {
	s.mu.RLock()
	sessionID := s.sessionID
	taskID := s.taskID
	runID := s.runID
	s.mu.RUnlock()

	if sessionID == "" || taskID == "" || runID == "" {
		// No task set, drop the log
		return
	}

	entry := &pb.LogEntry{
		SessionId:       sessionID,
		TaskId:          taskID,
		RunId:           runID,
		Stream:          stream,
		Level:           "info",
		Content:         string(data),
		Sequence:        s.sequence.Add(1),
		TimestampUnixMs: time.Now().UnixMilli(),
		RunnerId:        s.runnerID,
		TenantId:        s.tenantID,
	}

	// Non-blocking send to buffer
	select {
	case s.buffer <- entry:
		// OK
	default:
		// Buffer full, log warning
		s.logger.Warn("log buffer full, dropping entry",
			zap.String("task_id", taskID),
			zap.Int64("sequence", entry.Sequence),
		)
	}
}

// HandlePermissionRequest implements executor.OutputHandler.
// This is a placeholder - actual permission handling will be implemented in G4.
func (s *LogStreamer) HandlePermissionRequest(_ context.Context, _ interface{}) (bool, error) {
	// TODO: Implement in G4
	return true, nil
}

// Log sends a system log entry.
func (s *LogStreamer) Log(level, message string, metadata map[string]string) {
	s.mu.RLock()
	sessionID := s.sessionID
	taskID := s.taskID
	runID := s.runID
	s.mu.RUnlock()

	if sessionID == "" {
		return
	}

	entry := &pb.LogEntry{
		SessionId:       sessionID,
		TaskId:          taskID,
		RunId:           runID,
		Stream:          "system",
		Level:           level,
		Content:         message,
		Sequence:        s.sequence.Add(1),
		TimestampUnixMs: time.Now().UnixMilli(),
		Metadata:        metadata,
		RunnerId:        s.runnerID,
		TenantId:        s.tenantID,
	}

	select {
	case s.buffer <- entry:
	default:
		// Buffer full
	}
}

// Flush sends all buffered logs immediately.
func (s *LogStreamer) Flush(ctx context.Context) error {
	s.mu.RLock()
	sessionID := s.sessionID
	taskID := s.taskID
	runID := s.runID
	s.mu.RUnlock()

	if sessionID == "" {
		return nil
	}

	// Drain buffer
	var entries []*pb.LogEntry
	for {
		select {
		case entry := <-s.buffer:
			entries = append(entries, entry)
		default:
			// Buffer empty
			if len(entries) > 0 {
				return s.sendBatch(ctx, sessionID, taskID, runID, entries)
			}
			return nil
		}
	}
}

// streamLoop runs the main streaming loop.
func (s *LogStreamer) streamLoop(ctx context.Context) {
	defer close(s.stoppedC)

	ticker := time.NewTicker(s.config.FlushInterval)
	defer ticker.Stop()

	var batch []*pb.LogEntry

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, flush remaining
			s.flushBatch(ctx, batch)
			return

		case <-s.stopC:
			// Stopped, flush remaining
			s.flushBatch(ctx, batch)
			return

		case entry := <-s.buffer:
			batch = append(batch, entry)
			if len(batch) >= s.config.BatchSize {
				s.flushBatch(ctx, batch)
				batch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				s.flushBatch(ctx, batch)
				batch = nil
			}
		}
	}
}

// flushBatch sends a batch of log entries to the server.
func (s *LogStreamer) flushBatch(ctx context.Context, batch []*pb.LogEntry) {
	if len(batch) == 0 {
		return
	}

	s.mu.RLock()
	sessionID := s.sessionID
	taskID := s.taskID
	runID := s.runID
	s.mu.RUnlock()

	if sessionID == "" {
		return
	}

	if err := s.sendBatch(ctx, sessionID, taskID, runID, batch); err != nil {
		s.logger.Warn("failed to send log batch",
			zap.Error(err),
			zap.Int("count", len(batch)),
		)
	}
}

// sendBatch sends a batch of log entries via gRPC.
func (s *LogStreamer) sendBatch(ctx context.Context, sessionID, taskID, runID string, entries []*pb.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Create stream
	streamCtx := s.client.AttachMetadata(ctx)
	stream, err := s.client.StreamLogs(streamCtx)
	if err != nil {
		return err
	}

	// Send init message
	initMsg := &pb.StreamLogsMessage{
		Payload: &pb.StreamLogsMessage_Init{
			Init: &pb.StreamLogsInit{
				SessionId: sessionID,
				TaskId:    taskID,
				RunId:     runID,
				RunnerId:  s.runnerID,
				TenantId:  s.tenantID,
			},
		},
	}

	if err := stream.Send(initMsg); err != nil {
		return err
	}

	// Send log entries
	for _, entry := range entries {
		msg := &pb.StreamLogsMessage{
			Payload: &pb.StreamLogsMessage_LogEntry{
				LogEntry: entry,
			},
		}
		if err := stream.Send(msg); err != nil {
			return err
		}
	}

	// Close and receive response
	resp, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}

	if resp.LogsDropped > 0 {
		s.logger.Warn("server dropped logs",
			zap.Int64("dropped", resp.LogsDropped),
			zap.Int64("stored", resp.LogsStored),
		)
	}

	return nil
}

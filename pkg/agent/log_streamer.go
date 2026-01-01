package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// LogStreamer buffers and streams raw log entries from agent execution.
// It provides thread-safe buffering with configurable flush intervals.
type LogStreamer struct {
	mu sync.Mutex

	// Identification
	sessionID string
	taskID    string
	runID     string
	runnerID  string
	tenantID  *string

	// Buffering
	buffer    []*store.RawLog
	bufferCap int

	// Sequence tracking (atomic for thread safety)
	sequence atomic.Int64

	// Flush configuration
	flushInterval time.Duration
	flushTimer    *time.Timer

	// Output handler
	handler LogHandler

	// State
	closed bool
}

// LogHandler receives flushed log entries.
type LogHandler interface {
	// HandleLogs is called when logs are flushed.
	// Implementations should handle persistence or streaming.
	HandleLogs(ctx context.Context, logs []*store.RawLog) error
}

// LogHandlerFunc is an adapter to allow the use of ordinary functions as LogHandler.
type LogHandlerFunc func(ctx context.Context, logs []*store.RawLog) error

// HandleLogs calls f(ctx, logs).
func (f LogHandlerFunc) HandleLogs(ctx context.Context, logs []*store.RawLog) error {
	return f(ctx, logs)
}

// LogStreamerOption configures a LogStreamer.
type LogStreamerOption func(*LogStreamer)

// WithBufferCapacity sets the buffer capacity before auto-flush.
func WithBufferCapacity(cap int) LogStreamerOption {
	return func(s *LogStreamer) {
		s.bufferCap = cap
	}
}

// WithFlushInterval sets the time-based flush interval.
func WithFlushInterval(d time.Duration) LogStreamerOption {
	return func(s *LogStreamer) {
		s.flushInterval = d
	}
}

// WithTenantID sets the tenant ID for log entries.
func WithTenantID(tenantID string) LogStreamerOption {
	return func(s *LogStreamer) {
		s.tenantID = &tenantID
	}
}

// NewLogStreamer creates a new LogStreamer.
func NewLogStreamer(
	sessionID, taskID, runID, runnerID string,
	handler LogHandler,
	opts ...LogStreamerOption,
) *LogStreamer {
	s := &LogStreamer{
		sessionID:     sessionID,
		taskID:        taskID,
		runID:         runID,
		runnerID:      runnerID,
		handler:       handler,
		bufferCap:     100,             // Default: flush after 100 entries
		flushInterval: 1 * time.Second, // Default: flush every second
	}

	for _, opt := range opts {
		opt(s)
	}

	// Start flush timer
	s.flushTimer = time.AfterFunc(s.flushInterval, s.timerFlush)

	return s
}

// Write writes raw bytes to the log stream.
// This is the main method called from executor output handlers.
func (s *LogStreamer) Write(stream string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrStreamerClosed
	}

	// Create log entry
	log := &store.RawLog{
		ID:        id.RawLog(),
		SessionID: s.sessionID,
		TaskID:    s.taskID,
		RunID:     s.runID,
		RunnerID:  s.runnerID,
		Stream:    stream,
		Content:   append([]byte{}, data...), // Copy data
		Sequence:  s.sequence.Add(1),
		TenantID:  s.tenantID,
		CreatedAt: time.Now(),
	}

	s.buffer = append(s.buffer, log)

	// Check if buffer is full
	if len(s.buffer) >= s.bufferCap {
		return s.flushLocked(context.Background())
	}

	return nil
}

// WriteString is a convenience method for writing string content.
func (s *LogStreamer) WriteString(stream, content string) error {
	return s.Write(stream, []byte(content))
}

// Flush forces a flush of buffered logs.
func (s *LogStreamer) Flush(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked(ctx)
}

// flushLocked flushes the buffer while holding the lock.
func (s *LogStreamer) flushLocked(ctx context.Context) error {
	if len(s.buffer) == 0 {
		return nil
	}

	// Get logs to flush
	logs := s.buffer
	s.buffer = nil

	// Reset timer
	s.resetTimerLocked()

	// Call handler (outside lock would be better but we need to preserve order)
	if s.handler != nil {
		if err := s.handler.HandleLogs(ctx, logs); err != nil {
			// On error, put logs back in buffer (best effort)
			s.buffer = append(logs, s.buffer...)
			return err
		}
	}

	return nil
}

// timerFlush is called by the flush timer.
func (s *LogStreamer) timerFlush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	_ = s.flushLocked(context.Background())
}

// resetTimerLocked resets the flush timer while holding the lock.
func (s *LogStreamer) resetTimerLocked() {
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = time.AfterFunc(s.flushInterval, s.timerFlush)
	}
}

// Close flushes remaining logs and stops the streamer.
func (s *LogStreamer) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Stop timer
	if s.flushTimer != nil {
		s.flushTimer.Stop()
		s.flushTimer = nil
	}

	// Final flush
	return s.flushLocked(ctx)
}

// Stats returns current streamer statistics.
func (s *LogStreamer) Stats() LogStreamerStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	return LogStreamerStats{
		BufferedCount: len(s.buffer),
		TotalSequence: s.sequence.Load(),
		SessionID:     s.sessionID,
		TaskID:        s.taskID,
		RunID:         s.runID,
	}
}

// LogStreamerStats contains streamer statistics.
type LogStreamerStats struct {
	BufferedCount int
	TotalSequence int64
	SessionID     string
	TaskID        string
	RunID         string
}

// Errors
var (
	ErrStreamerClosed = &StreamerError{msg: "log streamer is closed"}
)

// StreamerError represents a log streamer error.
type StreamerError struct {
	msg string
}

func (e *StreamerError) Error() string {
	return e.msg
}

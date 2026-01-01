package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLogHandler captures logs for testing.
type mockLogHandler struct {
	mu   sync.Mutex
	logs []*store.RawLog
	err  error
}

func (m *mockLogHandler) HandleLogs(_ context.Context, logs []*store.RawLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.logs = append(m.logs, logs...)
	return nil
}

func (m *mockLogHandler) getLogs() []*store.RawLog {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*store.RawLog{}, m.logs...)
}

func (m *mockLogHandler) logCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.logs)
}

func TestNewLogStreamer(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler)
	defer func() { _ = s.Close(context.Background()) }()

	assert.NotNil(t, s)
	assert.Equal(t, "sess_1", s.sessionID)
	assert.Equal(t, "task_1", s.taskID)
	assert.Equal(t, "run_1", s.runID)
	assert.Equal(t, "runner_1", s.runnerID)
}

func TestLogStreamer_Write(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(10), // High capacity to prevent auto-flush
	)
	defer func() { _ = s.Close(context.Background()) }()

	err := s.Write("stdout", []byte("hello world"))
	require.NoError(t, err)

	stats := s.Stats()
	assert.Equal(t, 1, stats.BufferedCount)
	assert.Equal(t, int64(1), stats.TotalSequence)
}

func TestLogStreamer_WriteString(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(10),
	)
	defer func() { _ = s.Close(context.Background()) }()

	err := s.WriteString("stderr", "error message")
	require.NoError(t, err)

	stats := s.Stats()
	assert.Equal(t, 1, stats.BufferedCount)
}

func TestLogStreamer_Flush(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write some logs
	require.NoError(t, s.Write("stdout", []byte("line 1")))
	require.NoError(t, s.Write("stdout", []byte("line 2")))
	require.NoError(t, s.Write("stderr", []byte("error")))

	// Manual flush
	err := s.Flush(context.Background())
	require.NoError(t, err)

	// Check handler received logs
	logs := handler.getLogs()
	assert.Len(t, logs, 3)

	// Verify log content
	assert.Equal(t, "stdout", logs[0].Stream)
	assert.Equal(t, []byte("line 1"), logs[0].Content)
	assert.Equal(t, int64(1), logs[0].Sequence)

	assert.Equal(t, "stderr", logs[2].Stream)
	assert.Equal(t, []byte("error"), logs[2].Content)
	assert.Equal(t, int64(3), logs[2].Sequence)

	// Buffer should be empty
	stats := s.Stats()
	assert.Equal(t, 0, stats.BufferedCount)
}

func TestLogStreamer_AutoFlush_ByCapacity(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(3), // Small capacity for testing
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write 3 logs - should trigger auto-flush
	require.NoError(t, s.Write("stdout", []byte("1")))
	require.NoError(t, s.Write("stdout", []byte("2")))
	require.NoError(t, s.Write("stdout", []byte("3")))

	// Should have flushed
	assert.Equal(t, 3, handler.logCount())

	// Buffer should be empty
	stats := s.Stats()
	assert.Equal(t, 0, stats.BufferedCount)
}

func TestLogStreamer_AutoFlush_ByTimer(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),                // High capacity
		WithFlushInterval(50*time.Millisecond), // Short interval
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write a log
	require.NoError(t, s.Write("stdout", []byte("test")))

	// Wait for timer flush
	time.Sleep(100 * time.Millisecond)

	// Should have flushed
	assert.Equal(t, 1, handler.logCount())
}

func TestLogStreamer_Close(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)

	// Write some logs
	require.NoError(t, s.Write("stdout", []byte("before close")))

	// Close should flush
	err := s.Close(context.Background())
	require.NoError(t, err)

	// Logs should be flushed
	assert.Equal(t, 1, handler.logCount())

	// Write after close should fail
	err = s.Write("stdout", []byte("after close"))
	assert.ErrorIs(t, err, ErrStreamerClosed)
}

func TestLogStreamer_Close_Idempotent(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler)

	// Close multiple times should not error
	require.NoError(t, s.Close(context.Background()))
	require.NoError(t, s.Close(context.Background()))
}

func TestLogStreamer_SequenceOrdering(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write multiple logs
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Write("stdout", []byte("log")))
	}

	require.NoError(t, s.Flush(context.Background()))

	logs := handler.getLogs()
	require.Len(t, logs, 10)

	// Verify sequence is monotonically increasing
	for i, log := range logs {
		assert.Equal(t, int64(i+1), log.Sequence)
	}
}

func TestLogStreamer_WithTenantID(t *testing.T) {
	handler := &mockLogHandler{}
	tenantID := "tenant_123"
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithTenantID(tenantID),
		WithBufferCapacity(10),
	)
	defer func() { _ = s.Close(context.Background()) }()

	require.NoError(t, s.Write("stdout", []byte("test")))
	require.NoError(t, s.Flush(context.Background()))

	logs := handler.getLogs()
	require.Len(t, logs, 1)
	require.NotNil(t, logs[0].TenantID)
	assert.Equal(t, tenantID, *logs[0].TenantID)
}

func TestLogStreamer_Stats(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Initial stats
	stats := s.Stats()
	assert.Equal(t, 0, stats.BufferedCount)
	assert.Equal(t, int64(0), stats.TotalSequence)
	assert.Equal(t, "sess_1", stats.SessionID)
	assert.Equal(t, "task_1", stats.TaskID)
	assert.Equal(t, "run_1", stats.RunID)

	// After writes
	require.NoError(t, s.Write("stdout", []byte("1")))
	require.NoError(t, s.Write("stdout", []byte("2")))

	stats = s.Stats()
	assert.Equal(t, 2, stats.BufferedCount)
	assert.Equal(t, int64(2), stats.TotalSequence)
}

func TestLogStreamer_ConcurrentWrites(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(1000), // Large buffer
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Concurrent writes
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = s.Write("stdout", []byte("concurrent log"))
			}
		}()
	}
	wg.Wait()

	require.NoError(t, s.Flush(context.Background()))

	// Should have all logs
	assert.Equal(t, 100, handler.logCount())

	// Sequence should be unique (100 unique sequences)
	logs := handler.getLogs()
	seqs := make(map[int64]bool)
	for _, log := range logs {
		seqs[log.Sequence] = true
	}
	assert.Len(t, seqs, 100)
}

func TestLogStreamer_DataCopied(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write with a buffer that we'll modify
	data := []byte("original")
	require.NoError(t, s.Write("stdout", data))

	// Modify original data
	data[0] = 'X'

	require.NoError(t, s.Flush(context.Background()))

	// Log should have original content (data was copied)
	logs := handler.getLogs()
	require.Len(t, logs, 1)
	assert.Equal(t, []byte("original"), logs[0].Content)
}

func TestLogStreamer_LogIDsUnique(t *testing.T) {
	handler := &mockLogHandler{}
	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(100),
	)
	defer func() { _ = s.Close(context.Background()) }()

	// Write multiple logs
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Write("stdout", []byte("log")))
	}

	require.NoError(t, s.Flush(context.Background()))

	// All IDs should be unique and have rlog_ prefix
	logs := handler.getLogs()
	ids := make(map[string]bool)
	for _, log := range logs {
		assert.True(t, len(log.ID) > 5)
		assert.Equal(t, "rlog_", log.ID[:5])
		ids[log.ID] = true
	}
	assert.Len(t, ids, 10)
}

func TestLogHandlerFunc(t *testing.T) {
	var called bool
	var receivedLogs []*store.RawLog

	handler := LogHandlerFunc(func(_ context.Context, logs []*store.RawLog) error {
		called = true
		receivedLogs = logs
		return nil
	})

	s := NewLogStreamer("sess_1", "task_1", "run_1", "runner_1", handler,
		WithBufferCapacity(10),
	)
	defer func() { _ = s.Close(context.Background()) }()

	require.NoError(t, s.Write("stdout", []byte("test")))
	require.NoError(t, s.Flush(context.Background()))

	assert.True(t, called)
	assert.Len(t, receivedLogs, 1)
}

func TestStreamError(t *testing.T) {
	err := ErrStreamerClosed
	assert.Equal(t, "log streamer is closed", err.Error())
}

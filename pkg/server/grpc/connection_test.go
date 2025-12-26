package grpc

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewConnectionManager(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	require.NotNil(t, cm)
	assert.Equal(t, 0, cm.Count())
}

func TestConnectionManager_Register(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
	}

	err := cm.Register("run_123", conn)
	require.NoError(t, err)
	assert.Equal(t, 1, cm.Count())

	// Verify connection is stored
	stored, ok := cm.Get("run_123")
	require.True(t, ok)
	assert.Equal(t, "test-runner", stored.Name)
}

func TestConnectionManager_Register_AlreadyConnected(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}

	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	// Try to register again
	err = cm.Register("run_123", conn)
	require.Error(t, err)
	assert.Equal(t, ErrRunnerAlreadyConnected, err)
	assert.Equal(t, 1, cm.Count())
}

func TestConnectionManager_Unregister(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}

	err := cm.Register("run_123", conn)
	require.NoError(t, err)
	assert.Equal(t, 1, cm.Count())

	cm.Unregister("run_123")
	assert.Equal(t, 0, cm.Count())

	// Verify connection is removed
	_, ok := cm.Get("run_123")
	assert.False(t, ok)
}

func TestConnectionManager_Unregister_NotFound(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Unregistering non-existent runner should not panic
	cm.Unregister("run_nonexistent")
	assert.Equal(t, 0, cm.Count())
}

func TestConnectionManager_Get(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Not found
	_, ok := cm.Get("run_nonexistent")
	assert.False(t, ok)

	// Found
	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	stored, ok := cm.Get("run_123")
	require.True(t, ok)
	assert.Equal(t, "run_123", stored.RunnerID)
	assert.Equal(t, "test-runner", stored.Name)
}

func TestConnectionManager_List(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Empty list
	list := cm.List()
	assert.Empty(t, list)

	// Add some connections
	for i := 0; i < 3; i++ {
		conn := &RunnerConnection{
			RunnerID:    "run_" + string(rune('A'+i)),
			Name:        "runner-" + string(rune('A'+i)),
			Hostname:    "localhost",
			Status:      RunnerStatusIdle,
			ConnectedAt: time.Now(),
		}
		err := cm.Register(conn.RunnerID, conn)
		require.NoError(t, err)
	}

	list = cm.List()
	assert.Len(t, list, 3)
}

func TestConnectionManager_UpdateStatus(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	// Update status
	err = cm.UpdateStatus("run_123", RunnerStatusBusy)
	require.NoError(t, err)

	// Verify status changed
	stored, ok := cm.Get("run_123")
	require.True(t, ok)
	assert.Equal(t, RunnerStatusBusy, stored.GetStatus())
}

func TestConnectionManager_UpdateStatus_NotFound(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	err := cm.UpdateStatus("run_nonexistent", RunnerStatusBusy)
	require.Error(t, err)
	assert.Equal(t, ErrRunnerNotFound, err)
}

func TestConnectionManager_UpdateLastSeen(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	initialTime := time.Now().Add(-1 * time.Hour)
	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: initialTime,
		LastSeen:    initialTime,
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	// Update last seen
	err = cm.UpdateLastSeen("run_123")
	require.NoError(t, err)

	// Verify last seen changed
	stored, ok := cm.Get("run_123")
	require.True(t, ok)
	assert.True(t, stored.GetLastSeen().After(initialTime))
}

func TestConnectionManager_UpdateLastSeen_NotFound(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	err := cm.UpdateLastSeen("run_nonexistent")
	require.Error(t, err)
	assert.Equal(t, ErrRunnerNotFound, err)
}

func TestConnectionManager_GetByStatus(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Add some idle and busy runners
	for i := 0; i < 3; i++ {
		conn := &RunnerConnection{
			RunnerID:    "idle_" + string(rune('A'+i)),
			Name:        "idle-runner-" + string(rune('A'+i)),
			Hostname:    "localhost",
			Status:      RunnerStatusIdle,
			ConnectedAt: time.Now(),
		}
		err := cm.Register(conn.RunnerID, conn)
		require.NoError(t, err)
	}

	for i := 0; i < 2; i++ {
		conn := &RunnerConnection{
			RunnerID:    "busy_" + string(rune('A'+i)),
			Name:        "busy-runner-" + string(rune('A'+i)),
			Hostname:    "localhost",
			Status:      RunnerStatusBusy,
			ConnectedAt: time.Now(),
		}
		err := cm.Register(conn.RunnerID, conn)
		require.NoError(t, err)
	}

	idle := cm.GetByStatus(RunnerStatusIdle)
	assert.Len(t, idle, 3)

	busy := cm.GetByStatus(RunnerStatusBusy)
	assert.Len(t, busy, 2)
}

func TestConnectionManager_Count(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	assert.Equal(t, 0, cm.Count())

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	assert.Equal(t, 1, cm.Count())

	cm.Unregister("run_123")
	assert.Equal(t, 0, cm.Count())
}

func TestConnectionManager_IsConnected(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	assert.False(t, cm.IsConnected("run_123"))

	conn := &RunnerConnection{
		RunnerID:    "run_123",
		Name:        "test-runner",
		Hostname:    "localhost",
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	assert.True(t, cm.IsConnected("run_123"))
	assert.False(t, cm.IsConnected("run_456"))
}

func TestConnectionManager_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	var wg sync.WaitGroup
	numWorkers := 10
	numOperations := 100

	// Concurrent registrations and unregistrations
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				runnerID := "run_" + string(rune('A'+workerID)) + "_" + string(rune('0'+j%10))
				conn := &RunnerConnection{
					RunnerID:    runnerID,
					Name:        "runner",
					Hostname:    "localhost",
					Status:      RunnerStatusIdle,
					ConnectedAt: time.Now(),
				}
				_ = cm.Register(runnerID, conn)
				_ = cm.UpdateStatus(runnerID, RunnerStatusBusy)
				_ = cm.UpdateLastSeen(runnerID)
				_, _ = cm.Get(runnerID)
				_ = cm.List()
				_ = cm.GetByStatus(RunnerStatusIdle)
				_ = cm.Count()
				_ = cm.IsConnected(runnerID)
				cm.Unregister(runnerID)
			}
		}(i)
	}

	wg.Wait()
	// Test passed if no race conditions or deadlocks occurred
	// Verify the manager is still in a valid state
	assert.GreaterOrEqual(t, cm.Count(), 0)
}

func TestRunnerConnection_UpdateStatus(t *testing.T) {
	conn := &RunnerConnection{
		RunnerID: "run_123",
		Status:   RunnerStatusIdle,
	}

	assert.Equal(t, RunnerStatusIdle, conn.GetStatus())

	conn.UpdateStatus(RunnerStatusBusy)
	assert.Equal(t, RunnerStatusBusy, conn.GetStatus())
}

func TestRunnerConnection_UpdateLastSeen(t *testing.T) {
	initialTime := time.Now().Add(-1 * time.Hour)
	conn := &RunnerConnection{
		RunnerID: "run_123",
		LastSeen: initialTime,
	}

	assert.Equal(t, initialTime, conn.GetLastSeen())

	conn.UpdateLastSeen()
	assert.True(t, conn.GetLastSeen().After(initialTime))
}

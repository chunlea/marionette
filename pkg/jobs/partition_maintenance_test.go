package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockPartitioner is a mock implementation of LogPartitioner for testing.
type mockPartitioner struct {
	mu            sync.Mutex
	maintainCalls int
	dropCalls     int
	lastDaysAhead int
	lastRetention int
	maintainErr   error
	dropErr       error
}

func (m *mockPartitioner) MaintainLogPartitions(_ context.Context, daysAhead int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maintainCalls++
	m.lastDaysAhead = daysAhead
	return m.maintainErr
}

func (m *mockPartitioner) DropOldLogPartitions(_ context.Context, retentionDays int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropCalls++
	m.lastRetention = retentionDays
	return m.dropErr
}

func (m *mockPartitioner) counts() (maintain, drop int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maintainCalls, m.dropCalls
}

func (m *mockPartitioner) setMaintainErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maintainErr = err
}

func TestPartitionMaintainer_Defaults(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{})

	assert.Equal(t, 24*time.Hour, job.interval)
	assert.Equal(t, 7, job.daysAhead)
	// Retention defaults to disabled: log archiving is not wired yet, so
	// dropping partitions would destroy the only copy.
	assert.Equal(t, 0, job.retentionDays)
}

func TestPartitionMaintainer_RunsImmediatelyOnStart(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval:  time.Hour, // long enough that only the startup run can fire
		DaysAhead: 3,
		Logger:    zap.NewNop(),
	})

	ctx := context.Background()
	require.NoError(t, job.Start(ctx))
	t.Cleanup(func() { _ = job.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		maintain, _ := p.counts()
		return maintain >= 1
	}, 2*time.Second, 10*time.Millisecond, "maintenance did not run at startup")

	_, drop := p.counts()
	assert.Zero(t, drop, "retention must not run when RetentionDays is 0")

	p.mu.Lock()
	assert.Equal(t, 3, p.lastDaysAhead)
	p.mu.Unlock()
}

func TestPartitionMaintainer_RunsOnInterval(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval:  50 * time.Millisecond,
		DaysAhead: 7,
		Logger:    zap.NewNop(),
	})

	require.NoError(t, job.Start(context.Background()))
	t.Cleanup(func() { _ = job.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		maintain, _ := p.counts()
		return maintain >= 3
	}, 3*time.Second, 10*time.Millisecond, "maintenance did not repeat on the interval")

	assert.False(t, job.LastRun().IsZero())
	totalRuns, _ := job.Stats()
	assert.GreaterOrEqual(t, totalRuns, int64(3))
}

func TestPartitionMaintainer_RetentionEnabled(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval:      time.Hour,
		RetentionDays: 14,
		Logger:        zap.NewNop(),
	})

	result, err := job.RunNow(context.Background())
	require.NoError(t, err)
	assert.True(t, result.Dropped)

	maintain, drop := p.counts()
	assert.Equal(t, 1, maintain)
	assert.Equal(t, 1, drop)

	p.mu.Lock()
	assert.Equal(t, 14, p.lastRetention)
	p.mu.Unlock()
}

func TestPartitionMaintainer_RunNowPropagatesErrors(t *testing.T) {
	sentinel := errors.New("relation does not exist")

	t.Run("maintain error", func(t *testing.T) {
		p := &mockPartitioner{maintainErr: sentinel}
		job := NewPartitionMaintainer(p, PartitionMaintainerConfig{Logger: zap.NewNop()})

		_, err := job.RunNow(context.Background())
		require.ErrorIs(t, err, sentinel)

		// Retention must not run when partition creation failed.
		_, drop := p.counts()
		assert.Zero(t, drop)
	})

	t.Run("drop error", func(t *testing.T) {
		p := &mockPartitioner{dropErr: sentinel}
		job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
			RetentionDays: 7,
			Logger:        zap.NewNop(),
		})

		_, err := job.RunNow(context.Background())
		require.ErrorIs(t, err, sentinel)
	})
}

func TestPartitionMaintainer_SurvivesErrorsAndRecovers(t *testing.T) {
	p := &mockPartitioner{maintainErr: errors.New("transient")}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval: 20 * time.Millisecond,
		Logger:   zap.NewNop(),
	})

	require.NoError(t, job.Start(context.Background()))
	t.Cleanup(func() { _ = job.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		_, errs := job.Stats()
		return errs >= 2
	}, 3*time.Second, 10*time.Millisecond, "consecutive errors were not recorded")

	p.setMaintainErr(nil)

	require.Eventually(t, func() bool {
		_, errs := job.Stats()
		return errs == 0
	}, 3*time.Second, 10*time.Millisecond, "error counter did not reset after recovery")
}

func TestPartitionMaintainer_StartStop(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval: 50 * time.Millisecond,
		Logger:   zap.NewNop(),
	})

	ctx := context.Background()
	require.NoError(t, job.Start(ctx))
	assert.True(t, job.IsRunning())

	// Starting twice is an error, not a second goroutine.
	assert.Error(t, job.Start(ctx))

	require.NoError(t, job.Stop(ctx))
	assert.False(t, job.IsRunning())

	// Stopping an already-stopped job is a no-op.
	require.NoError(t, job.Stop(ctx))
}

func TestPartitionMaintainer_StopsOnContextCancel(t *testing.T) {
	p := &mockPartitioner{}
	job := NewPartitionMaintainer(p, PartitionMaintainerConfig{
		Interval: 20 * time.Millisecond,
		Logger:   zap.NewNop(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, job.Start(ctx))
	cancel()

	require.Eventually(t, func() bool {
		return !job.IsRunning()
	}, 2*time.Second, 10*time.Millisecond, "job did not stop when the context was canceled")
}

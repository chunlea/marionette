package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChecker_NewChecker(t *testing.T) {
	c := NewChecker()
	assert.NotNil(t, c)
	assert.Equal(t, 0, c.CheckCount())
}

func TestChecker_Register(t *testing.T) {
	c := NewChecker()

	c.Register("test1", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK}
	})
	assert.Equal(t, 1, c.CheckCount())

	c.Register("test2", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK}
	})
	assert.Equal(t, 2, c.CheckCount())

	// Re-registering should replace
	c.Register("test1", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusFail}
	})
	assert.Equal(t, 2, c.CheckCount())
}

func TestChecker_Unregister(t *testing.T) {
	c := NewChecker()

	c.Register("test1", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK}
	})
	assert.Equal(t, 1, c.CheckCount())

	c.Unregister("test1")
	assert.Equal(t, 0, c.CheckCount())

	// Unregistering non-existent should not panic
	c.Unregister("non-existent")
	assert.Equal(t, 0, c.CheckCount())
}

func TestChecker_CheckLiveness(t *testing.T) {
	c := NewChecker()

	// Liveness should always return OK
	resp := c.CheckLiveness(context.Background())
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Checks)
}

func TestChecker_CheckReadiness_NoChecks(t *testing.T) {
	c := NewChecker()

	resp := c.CheckReadiness(context.Background())
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Checks)
}

func TestChecker_CheckReadiness_AllPass(t *testing.T) {
	c := NewChecker()

	c.Register("check1", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK, Message: "all good"}
	})
	c.Register("check2", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK}
	})

	resp := c.CheckReadiness(context.Background())
	assert.Equal(t, StatusOK, resp.Status)
	assert.Len(t, resp.Checks, 2)

	assert.Equal(t, StatusOK, resp.Checks["check1"].Status)
	assert.Equal(t, "all good", resp.Checks["check1"].Message)
	assert.NotEmpty(t, resp.Checks["check1"].Latency)

	assert.Equal(t, StatusOK, resp.Checks["check2"].Status)
	assert.NotEmpty(t, resp.Checks["check2"].Latency)
}

func TestChecker_CheckReadiness_OneFails(t *testing.T) {
	c := NewChecker()

	c.Register("check1", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusOK}
	})
	c.Register("check2", func(_ context.Context) CheckResult {
		return CheckResult{Status: StatusFail, Message: "database down"}
	})

	resp := c.CheckReadiness(context.Background())
	assert.Equal(t, StatusFail, resp.Status)
	assert.Len(t, resp.Checks, 2)

	assert.Equal(t, StatusOK, resp.Checks["check1"].Status)
	assert.Equal(t, StatusFail, resp.Checks["check2"].Status)
	assert.Equal(t, "database down", resp.Checks["check2"].Message)
}

func TestChecker_CheckReadiness_ConcurrentExecution(t *testing.T) {
	c := NewChecker()

	// Register checks that take some time
	executed := make(chan string, 3)

	c.Register("slow1", func(_ context.Context) CheckResult {
		time.Sleep(50 * time.Millisecond)
		executed <- "slow1"
		return CheckResult{Status: StatusOK}
	})
	c.Register("slow2", func(_ context.Context) CheckResult {
		time.Sleep(50 * time.Millisecond)
		executed <- "slow2"
		return CheckResult{Status: StatusOK}
	})
	c.Register("slow3", func(_ context.Context) CheckResult {
		time.Sleep(50 * time.Millisecond)
		executed <- "slow3"
		return CheckResult{Status: StatusOK}
	})

	start := time.Now()
	resp := c.CheckReadiness(context.Background())
	elapsed := time.Since(start)

	assert.Equal(t, StatusOK, resp.Status)
	assert.Len(t, resp.Checks, 3)

	// Should complete in ~50ms (concurrent), not ~150ms (sequential)
	assert.Less(t, elapsed, 150*time.Millisecond, "checks should run concurrently")

	close(executed)
	var executedChecks []string
	for name := range executed {
		executedChecks = append(executedChecks, name)
	}
	assert.Len(t, executedChecks, 3)
}

func TestChecker_CheckReadiness_ContextCancellation(t *testing.T) {
	c := NewChecker()

	c.Register("blocking", func(ctx context.Context) CheckResult {
		select {
		case <-ctx.Done():
			return CheckResult{Status: StatusFail, Message: "context canceled"}
		case <-time.After(5 * time.Second):
			return CheckResult{Status: StatusOK}
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resp := c.CheckReadiness(ctx)
	assert.Equal(t, StatusFail, resp.Status)
	assert.Contains(t, resp.Checks["blocking"].Message, "context")
}

// Mock implementations for testing

type mockPinger struct {
	err error
}

func (m *mockPinger) Ping(_ context.Context) error {
	return m.err
}

type mockConnectionCounter struct {
	count int
}

func (m *mockConnectionCounter) Count() int {
	return m.count
}

func TestDatabaseCheck_Success(t *testing.T) {
	store := &mockPinger{err: nil}
	check := DatabaseCheck(store)

	result := check(context.Background())
	assert.Equal(t, StatusOK, result.Status)
	assert.Empty(t, result.Message)
}

func TestDatabaseCheck_Failure(t *testing.T) {
	store := &mockPinger{err: errors.New("connection refused")}
	check := DatabaseCheck(store)

	result := check(context.Background())
	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "connection refused", result.Message)
}

func TestDatabaseCheck_Timeout(t *testing.T) {
	// Create a slow pinger that respects context
	store := &slowPinger{}
	check := DatabaseCheck(store)

	// Use a very short timeout (shorter than slowPinger's 100ms delay)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result := check(ctx)
	// The check should fail due to context deadline exceeded
	assert.Equal(t, StatusFail, result.Status)
	assert.Contains(t, result.Message, "context deadline exceeded")
}

type slowPinger struct{}

func (s *slowPinger) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func TestConnectionManagerCheck_Success(t *testing.T) {
	cm := &mockConnectionCounter{count: 5}
	check := ConnectionManagerCheck(cm)

	result := check(context.Background())
	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "5 runners connected", result.Message)
}

func TestConnectionManagerCheck_ZeroConnections(t *testing.T) {
	cm := &mockConnectionCounter{count: 0}
	check := ConnectionManagerCheck(cm)

	result := check(context.Background())
	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "0 runners connected", result.Message)
}

func TestConnectionManagerCheck_OneConnection(t *testing.T) {
	cm := &mockConnectionCounter{count: 1}
	check := ConnectionManagerCheck(cm)

	result := check(context.Background())
	assert.Equal(t, StatusOK, result.Status)
	assert.Equal(t, "1 runner connected", result.Message)
}

func TestConnectionManagerCheck_NilManager(t *testing.T) {
	check := ConnectionManagerCheck(nil)

	result := check(context.Background())
	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "connection manager not initialized", result.Message)
}

func TestCustomCheck_Success(t *testing.T) {
	check := CustomCheck(func(_ context.Context) error {
		return nil
	})

	result := check(context.Background())
	assert.Equal(t, StatusOK, result.Status)
	assert.Empty(t, result.Message)
}

func TestCustomCheck_Failure(t *testing.T) {
	check := CustomCheck(func(_ context.Context) error {
		return errors.New("something went wrong")
	})

	result := check(context.Background())
	assert.Equal(t, StatusFail, result.Status)
	assert.Equal(t, "something went wrong", result.Message)
}

func TestFormatConnectionCount(t *testing.T) {
	tests := []struct {
		count    int
		expected string
	}{
		{0, "0 runners connected"},
		{1, "1 runner connected"},
		{2, "2 runners connected"},
		{10, "10 runners connected"},
		{100, "100 runners connected"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatConnectionCount(tt.count)
			assert.Equal(t, tt.expected, result)
		})
	}
}

package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDispatcher(t *testing.T) {
	config := Config{
		WorkerCount:           2,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "test-agent",
	}

	d := NewDispatcher(config, nil)
	require.NotNil(t, d)
	defer d.Stop()

	assert.NotNil(t, d.client)
	assert.NotNil(t, d.matcher)
	assert.NotNil(t, d.logger)
}

func TestDispatcher_DispatchSync_Success(t *testing.T) {
	// Create test server
	var receivedBody []byte
	var receivedHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		WorkerCount:           1,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "Marionette-Webhook/1.0",
	}

	d := NewDispatcher(config, zap.NewNop())
	defer d.Stop()

	webhook := &WebhookInfo{
		ID:             "whk_123",
		URL:            server.URL,
		Headers:        map[string]string{"X-Custom": "value"},
		TimeoutSeconds: 30,
	}

	payload, err := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, map[string]string{"status": "pending"})
	require.NoError(t, err)

	result := d.DispatchSync(context.Background(), webhook, payload, "whev_123", "test-secret")

	assert.True(t, result.Success)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Nil(t, result.Error)
	assert.Greater(t, result.Duration, time.Duration(0))

	// Verify headers
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "Marionette-Webhook/1.0", receivedHeaders.Get("User-Agent"))
	assert.Equal(t, "whev_123", receivedHeaders.Get(IDHeader))
	assert.Equal(t, "value", receivedHeaders.Get("X-Custom"))
	assert.NotEmpty(t, receivedHeaders.Get(SignatureHeader))
	assert.NotEmpty(t, receivedHeaders.Get(TimestampHeader))

	// Verify payload
	var received Payload
	err = json.Unmarshal(receivedBody, &received)
	require.NoError(t, err)
	assert.Equal(t, "task.created", received.Event)
}

func TestDispatcher_DispatchSync_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := Config{
		WorkerCount:           1,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "test",
	}

	d := NewDispatcher(config, zap.NewNop())
	defer d.Stop()

	webhook := &WebhookInfo{URL: server.URL}
	payload, _ := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, nil)

	result := d.DispatchSync(context.Background(), webhook, payload, "whev_123", "secret")

	assert.False(t, result.Success)
	assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
	assert.Error(t, result.Error)
}

func TestDispatcher_DispatchSync_PayloadTooLarge(t *testing.T) {
	config := Config{
		WorkerCount:           1,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        100, // Very small limit
		UserAgent:             "test",
	}

	d := NewDispatcher(config, zap.NewNop())
	defer d.Stop()

	webhook := &WebhookInfo{URL: "http://localhost"}
	// Create a large payload
	largeData := make([]byte, 200)
	payload, _ := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, largeData)

	result := d.DispatchSync(context.Background(), webhook, payload, "whev_123", "secret")

	assert.False(t, result.Success)
	assert.Contains(t, result.Error.Error(), "exceeds max")
}

func TestDispatcher_Dispatch_Async(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		WorkerCount:           2,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "test",
	}

	d := NewDispatcher(config, zap.NewNop())

	webhook := &WebhookInfo{URL: server.URL}
	payload, _ := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, nil)

	// Dispatch async
	done := make(chan DeliveryResult, 1)
	err := d.Dispatch(context.Background(), webhook, payload, "whev_123", "secret", func(result DeliveryResult) {
		done <- result
	})
	require.NoError(t, err)

	// Wait for result
	select {
	case result := <-done:
		assert.True(t, result.Success)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for delivery")
	}

	d.Stop()
	assert.Equal(t, int32(1), callCount.Load())
}

func TestDispatcher_Dispatch_ContextCanceled(t *testing.T) {
	config := Config{
		WorkerCount:           1,
		BatchSize:             1, // Small buffer
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "test",
	}

	d := NewDispatcher(config, zap.NewNop())
	defer d.Stop()

	webhook := &WebhookInfo{URL: "http://localhost"}
	payload, _ := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, nil)

	// Fill the buffer
	_ = d.Dispatch(context.Background(), webhook, payload, "whev_1", "secret", nil)

	// Now try with canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := d.Dispatch(ctx, webhook, payload, "whev_2", "secret", nil)
	assert.Error(t, err)
}

func TestDispatcher_Stop(t *testing.T) {
	config := Config{
		WorkerCount:           2,
		BatchSize:             10,
		DefaultTimeoutSeconds: 30,
		MaxPayloadSize:        1024 * 1024,
		UserAgent:             "test",
	}

	d := NewDispatcher(config, zap.NewNop())

	// Stop should not panic and be idempotent
	d.Stop()
	d.Stop() // Should not panic
}

func TestBuildPayload(t *testing.T) {
	t.Run("with map data", func(t *testing.T) {
		data := map[string]string{"key": "value"}
		payload, err := BuildPayload("task.created", ResourceInfo{ID: "task_123", Type: "task"}, data)
		require.NoError(t, err)

		assert.Equal(t, "task.created", payload.Event)
		assert.Equal(t, "task_123", payload.Resource.ID)
		assert.Equal(t, "task", payload.Resource.Type)
		assert.NotZero(t, payload.Timestamp)

		var decoded map[string]string
		err = json.Unmarshal(payload.Data, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "value", decoded["key"])
	})

	t.Run("with struct data", func(t *testing.T) {
		type testData struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		data := testData{Name: "test", Status: "active"}
		payload, err := BuildPayload("session.created", ResourceInfo{ID: "sess_123", Type: "session"}, data)
		require.NoError(t, err)

		var decoded testData
		err = json.Unmarshal(payload.Data, &decoded)
		require.NoError(t, err)
		assert.Equal(t, "test", decoded.Name)
		assert.Equal(t, "active", decoded.Status)
	})

	t.Run("with nil data", func(t *testing.T) {
		payload, err := BuildPayload("task.completed", ResourceInfo{ID: "task_123", Type: "task"}, nil)
		require.NoError(t, err)
		assert.Equal(t, json.RawMessage("null"), payload.Data)
	})
}

func TestCalculateNextRetry(t *testing.T) {
	baseDelay := time.Minute

	tests := []struct {
		attempt     int
		minExpected time.Duration
		maxExpected time.Duration
	}{
		{0, time.Minute, 2 * time.Minute},
		{1, 2 * time.Minute, 3 * time.Minute},
		{2, 4 * time.Minute, 5 * time.Minute},
		{3, 8 * time.Minute, 9 * time.Minute},
		{10, 1024 * time.Minute, 1025 * time.Minute}, // Should be capped at 24h
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			now := time.Now()
			next := CalculateNextRetry(tt.attempt, baseDelay)

			// Next retry should be in the future
			assert.True(t, next.After(now))

			// Check delay is within expected range (accounting for 24h cap)
			delay := next.Sub(now)
			if tt.minExpected > 24*time.Hour {
				assert.LessOrEqual(t, delay, 24*time.Hour+time.Second)
			} else {
				assert.GreaterOrEqual(t, delay, tt.minExpected-time.Second)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		statusCode  int
		shouldRetry bool
	}{
		// Should retry
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{408, true},
		{429, true},
		// Should not retry
		{200, false},
		{201, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{405, false},
		{422, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := ShouldRetry(tt.statusCode)
			assert.Equal(t, tt.shouldRetry, result, "statusCode=%d", tt.statusCode)
		})
	}
}

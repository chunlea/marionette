package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
)

func TestLogStreamNotConfigured(t *testing.T) {
	srv, _, token := testServer(t) // No log stream service

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/task_test123/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
	assert.Contains(t, rec.Body.String(), "log streaming not configured")
}

func TestEventStreamNotConfigured(t *testing.T) {
	srv, _, token := testServer(t) // No event stream service

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
	assert.Contains(t, rec.Body.String(), "event streaming not configured")
}

func TestLogStreamMissingTaskID(t *testing.T) {
	logSvc := NewMockLogStreamService()
	srv, _, token := testServer(t, WithLogStreamService(logSvc))

	// This hits the handler with an empty task ID (chi allows this)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs//stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	// Handler returns 400 for missing/empty task ID
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "task ID is required")
}

func TestLogStreamWebSocket(t *testing.T) {
	logSvc := NewMockLogStreamService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create an API key for testing
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"*"},
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithLogStreamService(logSvc),
	)

	// Create test server
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/logs/task_test123/stream"

	// Connect to WebSocket with auth header
	header := http.Header{}
	header.Add("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer func() {
		_ = conn.Close()
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	// Send a log message
	logSvc.Publish("task_test123", LogMessage{
		TaskID:    "task_test123",
		Stream:    "stdout",
		Level:     "info",
		Content:   "Hello, World!",
		Sequence:  1,
		Timestamp: time.Now(),
	})

	// Read the message
	var msg LogMessage
	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, err)
	err = conn.ReadJSON(&msg)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeLog, msg.Type)
	assert.Equal(t, "task_test123", msg.TaskID)
	assert.Equal(t, "Hello, World!", msg.Content)
}

func TestEventStreamWebSocket(t *testing.T) {
	eventSvc := NewMockEventStreamService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create an API key for testing
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"*"},
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithEventStreamService(eventSvc),
	)

	// Create test server
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect to WebSocket
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/events"
	header := http.Header{}
	header.Add("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer func() {
		_ = conn.Close()
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	// Send an event
	eventSvc.Publish(EventMessage{
		EventType: "task.created",
		Resource:  "task",
		ID:        "task_123",
		Data:      map[string]any{"status": "pending"},
		Timestamp: time.Now(),
	})

	// Read the message
	var msg EventMessage
	err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	require.NoError(t, err)
	err = conn.ReadJSON(&msg)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeEvent, msg.Type)
	assert.Equal(t, "task.created", msg.EventType)
	assert.Equal(t, "task_123", msg.ID)
}

func TestEventStreamWithQueryParams(t *testing.T) {
	eventSvc := NewMockEventStreamService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"*"},
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithEventStreamService(eventSvc),
	)

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// Connect with query parameters for filtering
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/events?event_type=task.created&labels={\"env\":\"prod\"}"
	header := http.Header{}
	header.Add("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	defer func() {
		_ = conn.Close()
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
}

func TestMockLogStreamService(t *testing.T) {
	svc := NewMockLogStreamService()

	t.Run("subscribe and unsubscribe", func(t *testing.T) {
		ctx := context.Background()
		ch, unsubscribe, err := svc.Subscribe(ctx, "task_123")
		require.NoError(t, err)
		require.NotNil(t, ch)

		// Publish a message
		svc.Publish("task_123", LogMessage{Content: "test"})

		// Read the message
		select {
		case msg := <-ch:
			assert.Equal(t, "test", msg.Content)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}

		// Unsubscribe
		unsubscribe()

		// Channel should be closed
		_, ok := <-ch
		assert.False(t, ok)
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		ctx := context.Background()
		ch1, unsub1, err := svc.Subscribe(ctx, "task_multi")
		require.NoError(t, err)
		ch2, unsub2, err := svc.Subscribe(ctx, "task_multi")
		require.NoError(t, err)

		// Both should receive the message
		svc.Publish("task_multi", LogMessage{Content: "broadcast"})

		select {
		case msg := <-ch1:
			assert.Equal(t, "broadcast", msg.Content)
		case <-time.After(time.Second):
			t.Fatal("timeout on ch1")
		}

		select {
		case msg := <-ch2:
			assert.Equal(t, "broadcast", msg.Content)
		case <-time.After(time.Second):
			t.Fatal("timeout on ch2")
		}

		unsub1()
		unsub2()
	})

	t.Run("publish to non-existent task", func(_ *testing.T) {
		// Should not panic
		svc.Publish("non_existent", LogMessage{Content: "ignored"})
	})
}

func TestMockEventStreamService(t *testing.T) {
	svc := NewMockEventStreamService()

	t.Run("subscribe and unsubscribe", func(t *testing.T) {
		ctx := context.Background()
		ch, unsubscribe, err := svc.Subscribe(ctx, EventSubscribeOptions{})
		require.NoError(t, err)
		require.NotNil(t, ch)

		// Publish an event
		svc.Publish(EventMessage{EventType: "test"})

		// Read the event
		select {
		case msg := <-ch:
			assert.Equal(t, "test", msg.EventType)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		// Unsubscribe
		unsubscribe()

		// Channel should be closed
		_, ok := <-ch
		assert.False(t, ok)
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		ctx := context.Background()
		ch1, unsub1, err := svc.Subscribe(ctx, EventSubscribeOptions{})
		require.NoError(t, err)
		ch2, unsub2, err := svc.Subscribe(ctx, EventSubscribeOptions{})
		require.NoError(t, err)

		// Both should receive the event
		svc.Publish(EventMessage{EventType: "broadcast"})

		select {
		case msg := <-ch1:
			assert.Equal(t, "broadcast", msg.EventType)
		case <-time.After(time.Second):
			t.Fatal("timeout on ch1")
		}

		select {
		case msg := <-ch2:
			assert.Equal(t, "broadcast", msg.EventType)
		case <-time.After(time.Second):
			t.Fatal("timeout on ch2")
		}

		unsub1()
		unsub2()
	})

	t.Run("publish with no subscribers", func(_ *testing.T) {
		// Should not panic
		svc.Publish(EventMessage{EventType: "ignored"})
	})
}

func TestParseLabelsJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "valid json",
			input:    `{"env":"prod","team":"backend"}`,
			expected: map[string]string{"env": "prod", "team": "backend"},
		},
		{
			name:     "single label",
			input:    `{"key":"value"}`,
			expected: map[string]string{"key": "value"},
		},
		{
			name:     "invalid json",
			input:    `{invalid}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLabelsJSON(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWebSocketMessageTypes(t *testing.T) {
	assert.Equal(t, "log", MessageTypeLog)
	assert.Equal(t, "event", MessageTypeEvent)
	assert.Equal(t, "ping", MessageTypePing)
	assert.Equal(t, "pong", MessageTypePong)
	assert.Equal(t, "error", MessageTypeError)
}

func TestLogMessage(t *testing.T) {
	now := time.Now()
	msg := LogMessage{
		Type:      MessageTypeLog,
		TaskID:    "task_123",
		RunID:     "trun_123",
		Stream:    "stdout",
		Level:     "info",
		Content:   "Hello",
		Sequence:  42,
		Timestamp: now,
	}

	assert.Equal(t, "log", msg.Type)
	assert.Equal(t, "task_123", msg.TaskID)
	assert.Equal(t, int64(42), msg.Sequence)
}

func TestEventMessage(t *testing.T) {
	now := time.Now()
	msg := EventMessage{
		Type:      MessageTypeEvent,
		EventType: "session.created",
		Resource:  "session",
		ID:        "sess_123",
		Data:      map[string]any{"status": "active"},
		Timestamp: now,
	}

	assert.Equal(t, "event", msg.Type)
	assert.Equal(t, "session.created", msg.EventType)
	assert.Equal(t, "active", msg.Data["status"])
}

func TestEventSubscribeOptions(t *testing.T) {
	opts := EventSubscribeOptions{
		EventTypes: []string{"task.created", "task.completed"},
		Labels:     map[string]string{"env": "prod"},
	}

	assert.Len(t, opts.EventTypes, 2)
	assert.Equal(t, "prod", opts.Labels["env"])
}

func TestLogStreamAuthRequired(t *testing.T) {
	logSvc := NewMockLogStreamService()
	srv, _, _ := testServer(t, WithLogStreamService(logSvc))

	// Try without auth header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/task_test123/stream", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestEventStreamAuthRequired(t *testing.T) {
	eventSvc := NewMockEventStreamService()
	srv, _, _ := testServer(t, WithEventStreamService(eventSvc))

	// Try without auth header
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLogStreamScopeRequired(t *testing.T) {
	logSvc := NewMockLogStreamService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create API key with wrong scope
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "write-only",
		Scopes: []string{"tasks:write"}, // No read access
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithLogStreamService(logSvc),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs/task_test123/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEventStreamScopeRequired(t *testing.T) {
	eventSvc := NewMockEventStreamService()
	logger := zap.NewNop()
	keyStore := newMockAPIKeyStore()
	apiKeyService := auth.NewAPIKeyService(keyStore, func() string { return "key_test123" })

	// Create API key with wrong scope
	_, token, err := apiKeyService.Create(context.Background(), auth.CreateAPIKeyOptions{
		Name:   "sessions-only",
		Scopes: []string{"sessions:read"}, // No events access
	})
	require.NoError(t, err)

	srv := New(Config{Host: "localhost", Port: 8080}, logger,
		WithAPIKeyService(apiKeyService),
		WithEventStreamService(eventSvc),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestWebSocketBackpressure(t *testing.T) {
	svc := NewMockLogStreamService()
	ctx := context.Background()

	ch, unsub, err := svc.Subscribe(ctx, "task_bp")
	require.NoError(t, err)
	defer unsub()

	// Fill up the channel (buffer size is 100)
	for i := 0; i < 150; i++ {
		svc.Publish("task_bp", LogMessage{Content: "msg"})
	}

	// Should have 100 messages (buffer size)
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 100, count)
}

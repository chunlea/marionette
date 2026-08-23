package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/streaming"
)

// mockStreamManager implements StreamManager interface for testing.
type mockStreamManager struct {
	startStreamFn        func(ctx context.Context, opts streaming.StreamOptions) (*streaming.Stream, error)
	stopStreamFn         func(ctx context.Context, streamID string) error
	getStreamFn          func(ctx context.Context, streamID string) (*streaming.Stream, error)
	getStreamBySessionFn func(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error)
	listStreamsFn        func(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error)
	listSessionStreamsFn func(ctx context.Context, sessionID string) ([]*streaming.Stream, error)
}

func (m *mockStreamManager) StartStream(ctx context.Context, opts streaming.StreamOptions) (*streaming.Stream, error) {
	if m.startStreamFn != nil {
		return m.startStreamFn(ctx, opts)
	}
	return nil, nil
}

func (m *mockStreamManager) StopStream(ctx context.Context, streamID string) error {
	if m.stopStreamFn != nil {
		return m.stopStreamFn(ctx, streamID)
	}
	return nil
}

func (m *mockStreamManager) GetStream(ctx context.Context, streamID string) (*streaming.Stream, error) {
	if m.getStreamFn != nil {
		return m.getStreamFn(ctx, streamID)
	}
	return nil, nil
}

func (m *mockStreamManager) GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error) {
	if m.getStreamBySessionFn != nil {
		return m.getStreamBySessionFn(ctx, sessionID, streamType)
	}
	return nil, nil
}

func (m *mockStreamManager) ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
	if m.listStreamsFn != nil {
		return m.listStreamsFn(ctx, params)
	}
	return nil, 0, nil
}

func (m *mockStreamManager) ListSessionStreams(ctx context.Context, sessionID string) ([]*streaming.Stream, error) {
	if m.listSessionStreamsFn != nil {
		return m.listSessionStreamsFn(ctx, sessionID)
	}
	return nil, nil
}

func TestNewStreamsHandler(t *testing.T) {
	mgr := &mockStreamManager{}
	handler := NewStreamsHandler(mgr, nil, nil)

	assert.NotNil(t, handler)
}

func TestStreamsHandler_Routes(t *testing.T) {
	mgr := &mockStreamManager{}
	handler := NewStreamsHandler(mgr, nil, nil)

	routes := handler.Routes()
	assert.NotNil(t, routes)
}

func TestStreamsHandler_List(t *testing.T) {
	now := time.Now()
	testStream := &streaming.Stream{
		ID:           "stream_123",
		SessionID:    "sess_123",
		RunnerID:     "run_123",
		Type:         streaming.StreamTypeDesktop,
		State:        streaming.StreamStateActive,
		Resolution:   streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:    30,
		ProviderName: "selkies",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mgr := &mockStreamManager{
		listStreamsFn: func(_ context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
			return []*streaming.Stream{testStream}, 1, nil
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	items := result["items"].([]interface{})
	assert.Len(t, items, 1)
	// The streams listing used to answer {items,total}, alone among the admin
	// listings — and the TypeScript already expected total_count.
	assert.Equal(t, float64(1), result["total_count"])
	assert.Equal(t, false, result["has_more"])

	item := items[0].(map[string]interface{})
	assert.Equal(t, "stream_123", item["id"])
	assert.Equal(t, "sess_123", item["session_id"])
	assert.Equal(t, "desktop", item["type"])
	assert.Equal(t, "active", item["state"])
}

func TestStreamsHandler_List_WithFilters(t *testing.T) {
	mgr := &mockStreamManager{
		listStreamsFn: func(_ context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
			// Verify filters are passed correctly
			assert.Equal(t, "sess_filter", params.SessionID)
			assert.Equal(t, "run_filter", params.RunnerID)
			assert.NotNil(t, params.Type)
			assert.Equal(t, streaming.StreamTypeDesktop, *params.Type)
			assert.NotNil(t, params.State)
			assert.Equal(t, streaming.StreamStateActive, *params.State)
			assert.True(t, params.ActiveOnly)
			assert.Equal(t, 10, params.Limit)
			assert.Equal(t, 5, params.Offset)
			return []*streaming.Stream{}, 0, nil
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/?session_id=sess_filter&runner_id=run_filter&type=desktop&state=active&active_only=true&limit=10&offset=5", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStreamsHandler_List_Error(t *testing.T) {
	mgr := &mockStreamManager{
		listStreamsFn: func(_ context.Context, _ streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
			return nil, 0, errors.New("database error")
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestStreamsHandler_Create(t *testing.T) {
	now := time.Now()
	testStream := &streaming.Stream{
		ID:           "stream_new",
		SessionID:    "sess_123",
		Type:         streaming.StreamTypeDesktop,
		State:        streaming.StreamStateActive,
		Resolution:   streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:    30,
		ProviderName: "selkies",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mgr := &mockStreamManager{
		startStreamFn: func(_ context.Context, opts streaming.StreamOptions) (*streaming.Stream, error) {
			assert.Equal(t, "sess_123", opts.SessionID)
			assert.Equal(t, streaming.StreamTypeDesktop, opts.Type)
			assert.Equal(t, 1920, opts.Resolution.Width)
			assert.Equal(t, 1080, opts.Resolution.Height)
			assert.Equal(t, 30, opts.FrameRate)
			return testStream, nil
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	body := StreamRequest{
		SessionID: "sess_123",
		Type:      streaming.StreamTypeDesktop,
		Resolution: &ResolutionRequest{
			Width:  1920,
			Height: 1080,
		},
		FrameRate: 30,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var result StreamResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "stream_new", result.ID)
	assert.Equal(t, "sess_123", result.SessionID)
	assert.Equal(t, "desktop", result.Type)
}

func TestStreamsHandler_Create_InvalidBody(t *testing.T) {
	mgr := &mockStreamManager{}
	handler := NewStreamsHandler(mgr, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamsHandler_Create_MissingSessionID(t *testing.T) {
	mgr := &mockStreamManager{}
	handler := NewStreamsHandler(mgr, nil, nil)

	body := StreamRequest{
		Type: streaming.StreamTypeDesktop,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamsHandler_Create_InvalidType(t *testing.T) {
	mgr := &mockStreamManager{}
	handler := NewStreamsHandler(mgr, nil, nil)

	body := StreamRequest{
		SessionID: "sess_123",
		Type:      streaming.StreamType("invalid"),
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Create(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStreamsHandler_Create_StartStreamErrors(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "session required",
			err:            streaming.ErrSessionRequired,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid stream type",
			err:            streaming.ErrInvalidStreamType,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "provider not found",
			err:            streaming.ErrProviderNotFound,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "stream closed",
			err:            streaming.ErrStreamClosed,
			expectedStatus: http.StatusServiceUnavailable,
		},
		{
			name:           "internal error",
			err:            errors.New("internal error"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &mockStreamManager{
				startStreamFn: func(_ context.Context, _ streaming.StreamOptions) (*streaming.Stream, error) {
					return nil, tt.err
				},
			}

			handler := NewStreamsHandler(mgr, nil, nil)

			body := StreamRequest{
				SessionID: "sess_123",
				Type:      streaming.StreamTypeDesktop,
			}
			jsonBody, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Create(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
		})
	}
}

func TestStreamsHandler_Get(t *testing.T) {
	now := time.Now()
	testStream := &streaming.Stream{
		ID:           "stream_123",
		SessionID:    "sess_123",
		Type:         streaming.StreamTypeDesktop,
		State:        streaming.StreamStateActive,
		ProviderName: "selkies",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, streamID string) (*streaming.Stream, error) {
			assert.Equal(t, "stream_123", streamID)
			return testStream, nil
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	// Create a chi router context with URL param
	r := chi.NewRouter()
	r.Get("/{streamID}", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/stream_123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result StreamResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	assert.Equal(t, "stream_123", result.ID)
	assert.Equal(t, "desktop", result.Type)
}

func TestStreamsHandler_Get_NotFound(t *testing.T) {
	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, _ string) (*streaming.Stream, error) {
			return nil, streaming.ErrStreamNotFound
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	r := chi.NewRouter()
	r.Get("/{streamID}", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/stream_nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestStreamsHandler_Get_Error(t *testing.T) {
	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, _ string) (*streaming.Stream, error) {
			return nil, errors.New("internal error")
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	r := chi.NewRouter()
	r.Get("/{streamID}", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/stream_123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestStreamsHandler_Stop(t *testing.T) {
	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, streamID string) (*streaming.Stream, error) {
			return &streaming.Stream{
				ID:        streamID,
				SessionID: "sess_123",
				RunnerID:  "run_123",
				Type:      streaming.StreamTypeDesktop,
				State:     streaming.StreamStateActive,
			}, nil
		},
		stopStreamFn: func(_ context.Context, streamID string) error {
			assert.Equal(t, "stream_123", streamID)
			return nil
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	r := chi.NewRouter()
	r.Delete("/{streamID}", handler.Stop)

	req := httptest.NewRequest(http.MethodDelete, "/stream_123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestStreamsHandler_Stop_NotFound(t *testing.T) {
	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, _ string) (*streaming.Stream, error) {
			return nil, streaming.ErrStreamNotFound
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	r := chi.NewRouter()
	r.Delete("/{streamID}", handler.Stop)

	req := httptest.NewRequest(http.MethodDelete, "/stream_nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestStreamsHandler_Stop_Error(t *testing.T) {
	mgr := &mockStreamManager{
		getStreamFn: func(_ context.Context, _ string) (*streaming.Stream, error) {
			return nil, errors.New("internal error")
		},
	}

	handler := NewStreamsHandler(mgr, nil, nil)

	r := chi.NewRouter()
	r.Delete("/{streamID}", handler.Stop)

	req := httptest.NewRequest(http.MethodDelete, "/stream_123", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestStreamToResponse(t *testing.T) {
	now := time.Now()
	startedAt := now.Add(-time.Hour)
	stoppedAt := now

	stream := &streaming.Stream{
		ID:               "stream_123",
		SessionID:        "sess_123",
		RunnerID:         "run_123",
		Type:             streaming.StreamTypeDesktop,
		State:            streaming.StreamStateStopped,
		SignalingURL:     "wss://example.com/signal",
		Resolution:       streaming.Resolution{Width: 1920, Height: 1080},
		FrameRate:        30,
		BitRate:          5000000,
		VideoCodec:       "h264",
		AudioCodec:       "opus",
		AudioEnabled:     true,
		InputEnabled:     true,
		ProviderName:     "selkies",
		ProviderStreamID: "provider_123",
		Error:            "test error",
		Metadata:         map[string]string{"key": "value"},
		CreatedAt:        now,
		UpdatedAt:        now,
		StartedAt:        &startedAt,
		StoppedAt:        &stoppedAt,
	}

	resp := streamToResponse(stream)

	assert.Equal(t, "stream_123", resp.ID)
	assert.Equal(t, "sess_123", resp.SessionID)
	assert.Equal(t, "run_123", resp.RunnerID)
	assert.Equal(t, "desktop", resp.Type)
	assert.Equal(t, "stopped", resp.State)
	assert.Equal(t, "wss://example.com/signal", resp.SignalingURL)
	assert.NotNil(t, resp.Resolution)
	assert.Equal(t, 1920, resp.Resolution.Width)
	assert.Equal(t, 1080, resp.Resolution.Height)
	assert.Equal(t, 30, resp.FrameRate)
	assert.Equal(t, 5000000, resp.BitRate)
	assert.Equal(t, "h264", resp.VideoCodec)
	assert.Equal(t, "opus", resp.AudioCodec)
	assert.True(t, resp.AudioEnabled)
	assert.True(t, resp.InputEnabled)
	assert.Equal(t, "selkies", resp.ProviderName)
	assert.Equal(t, "provider_123", resp.ProviderStreamID)
	assert.Equal(t, "test error", resp.Error)
	assert.Equal(t, "value", resp.Metadata["key"])
	assert.NotEmpty(t, resp.CreatedAt)
	assert.NotEmpty(t, resp.UpdatedAt)
	assert.NotEmpty(t, resp.StartedAt)
	assert.NotEmpty(t, resp.StoppedAt)
}

func TestStreamToResponse_NoOptionalFields(t *testing.T) {
	now := time.Now()

	stream := &streaming.Stream{
		ID:           "stream_123",
		SessionID:    "sess_123",
		Type:         streaming.StreamTypeDesktop,
		State:        streaming.StreamStatePending,
		ProviderName: "selkies",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	resp := streamToResponse(stream)

	assert.Equal(t, "stream_123", resp.ID)
	assert.Nil(t, resp.Resolution)
	assert.Empty(t, resp.StartedAt)
	assert.Empty(t, resp.StoppedAt)
}

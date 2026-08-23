package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/streaming"
)

// StreamCommandSender sends stream commands to runners.
type StreamCommandSender interface {
	SendCommand(runnerID string, cmd *pb.ServerCommand) error
}

// StreamsHandler handles stream-related HTTP requests.
type StreamsHandler struct {
	streamManager StreamManager
	cmdSender     StreamCommandSender
	logger        *zap.Logger
}

// StreamManager interface for stream operations.
type StreamManager interface {
	StartStream(ctx context.Context, opts streaming.StreamOptions) (*streaming.Stream, error)
	StopStream(ctx context.Context, streamID string) error
	GetStream(ctx context.Context, streamID string) (*streaming.Stream, error)
	GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error)
	ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error)
	ListSessionStreams(ctx context.Context, sessionID string) ([]*streaming.Stream, error)
}

// NewStreamsHandler creates a new StreamsHandler.
func NewStreamsHandler(mgr StreamManager, cmdSender StreamCommandSender, logger *zap.Logger) *StreamsHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &StreamsHandler{
		streamManager: mgr,
		cmdSender:     cmdSender,
		logger:        logger.Named("streams_handler"),
	}
}

// Routes returns the handler's routes.
func (h *StreamsHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/{streamID}", h.Get)
	r.Delete("/{streamID}", h.Stop)

	return r
}

// StreamRequest represents the request body for creating a stream.
type StreamRequest struct {
	SessionID    string               `json:"session_id"`
	RunnerID     string               `json:"runner_id,omitempty"`
	Type         streaming.StreamType `json:"type"`
	Resolution   *ResolutionRequest   `json:"resolution,omitempty"`
	FrameRate    int                  `json:"frame_rate,omitempty"`
	BitRate      int                  `json:"bitrate,omitempty"`
	AudioEnabled bool                 `json:"audio_enabled,omitempty"`
	InputEnabled bool                 `json:"input_enabled,omitempty"`
	Metadata     map[string]string    `json:"metadata,omitempty"`
}

// ResolutionRequest represents resolution in a request.
type ResolutionRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// StreamResponse represents a stream in API responses.
type StreamResponse struct {
	ID               string             `json:"id"`
	SessionID        string             `json:"session_id"`
	RunnerID         string             `json:"runner_id,omitempty"`
	Type             string             `json:"type"`
	State            string             `json:"state"`
	SignalingURL     string             `json:"signaling_url,omitempty"`
	Resolution       *ResolutionRequest `json:"resolution,omitempty"`
	FrameRate        int                `json:"frame_rate,omitempty"`
	BitRate          int                `json:"bitrate,omitempty"`
	VideoCodec       string             `json:"video_codec,omitempty"`
	AudioCodec       string             `json:"audio_codec,omitempty"`
	AudioEnabled     bool               `json:"audio_enabled"`
	InputEnabled     bool               `json:"input_enabled"`
	ProviderName     string             `json:"provider_name"`
	ProviderStreamID string             `json:"provider_stream_id,omitempty"`
	Error            string             `json:"error,omitempty"`
	Metadata         map[string]string  `json:"metadata,omitempty"`
	CreatedAt        string             `json:"created_at"`
	UpdatedAt        string             `json:"updated_at"`
	StartedAt        string             `json:"started_at,omitempty"`
	StoppedAt        string             `json:"stopped_at,omitempty"`
}

// List lists streams with optional filters.
func (h *StreamsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters
	params := streaming.ListStreamsParams{
		SessionID: r.URL.Query().Get("session_id"),
		RunnerID:  r.URL.Query().Get("runner_id"),
		TenantID:  r.URL.Query().Get("tenant_id"),
	}

	// Parse type filter
	if typeStr := r.URL.Query().Get("type"); typeStr != "" {
		t := streaming.StreamType(typeStr)
		params.Type = &t
	}

	// Parse state filter
	if stateStr := r.URL.Query().Get("state"); stateStr != "" {
		s := streaming.StreamState(stateStr)
		params.State = &s
	}

	// Parse active_only filter
	if activeOnly := r.URL.Query().Get("active_only"); activeOnly == "true" {
		params.ActiveOnly = true
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}

	// Parse offset
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			params.Offset = offset
		}
	}

	streams, total, err := h.streamManager.ListStreams(ctx, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Convert to response format
	items := make([]*StreamResponse, len(streams))
	for i, stream := range streams {
		items[i] = streamToResponse(stream)
	}

	WriteJSON(w, http.StatusOK, &admintypes.ListResponse[StreamResponse]{
		Items:      items,
		TotalCount: int64(total),
		HasMore:    params.Offset+len(items) < total,
	})
}

// Create creates a new stream.
func (h *StreamsHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req StreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.SessionID == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "session_id is required")
		return
	}
	if !req.Type.IsValid() {
		WriteError(w, http.StatusBadRequest, "validation_error", "invalid stream type")
		return
	}

	// Build stream options
	opts := streaming.StreamOptions{
		SessionID:    req.SessionID,
		RunnerID:     req.RunnerID,
		Type:         req.Type,
		FrameRate:    req.FrameRate,
		BitRate:      req.BitRate,
		AudioEnabled: req.AudioEnabled,
		InputEnabled: req.InputEnabled,
		Metadata:     req.Metadata,
	}

	if req.Resolution != nil {
		opts.Resolution = streaming.Resolution{
			Width:  req.Resolution.Width,
			Height: req.Resolution.Height,
		}
	}

	stream, err := h.streamManager.StartStream(ctx, opts)
	if err != nil {
		switch err {
		case streaming.ErrSessionRequired:
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
		case streaming.ErrInvalidStreamType:
			WriteError(w, http.StatusBadRequest, "validation_error", err.Error())
		case streaming.ErrProviderNotFound:
			WriteError(w, http.StatusServiceUnavailable, "provider_unavailable", "no provider available for this stream type")
		case streaming.ErrStreamClosed:
			WriteError(w, http.StatusServiceUnavailable, "service_unavailable", "streaming service is shutting down")
		default:
			WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
		return
	}

	// Send StartDesktopStream command to the runner
	if h.cmdSender != nil && req.RunnerID != "" && req.Type == streaming.StreamTypeDesktop {
		cmd := &pb.ServerCommand{
			Payload: &pb.ServerCommand_StartDesktopStream{
				StartDesktopStream: &pb.StartDesktopStream{
					StreamId:  stream.ID,
					SessionId: stream.SessionID,
					Config: &pb.StreamConfig{
						Width:        int32(stream.Resolution.Width),
						Height:       int32(stream.Resolution.Height),
						FrameRate:    int32(stream.FrameRate),
						Bitrate:      int32(stream.BitRate),
						AudioEnabled: stream.AudioEnabled,
						InputEnabled: stream.InputEnabled,
					},
					// TODO: Add ICE servers configuration
				},
			},
		}

		if err := h.cmdSender.SendCommand(req.RunnerID, cmd); err != nil {
			h.logger.Error("failed to send StartDesktopStream command",
				zap.String("stream_id", stream.ID),
				zap.String("runner_id", req.RunnerID),
				zap.Error(err),
			)
			// Don't fail the request, the stream is created but the agent might not be ready
			// The agent can be notified later when it reconnects
		} else {
			h.logger.Info("sent StartDesktopStream command",
				zap.String("stream_id", stream.ID),
				zap.String("runner_id", req.RunnerID),
			)
		}
	}

	WriteJSON(w, http.StatusCreated, streamToResponse(stream))
}

// Get gets a stream by ID.
func (h *StreamsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streamID := chi.URLParam(r, "streamID")

	stream, err := h.streamManager.GetStream(ctx, streamID)
	if err != nil {
		if err == streaming.ErrStreamNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "stream not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, streamToResponse(stream))
}

// Stop stops a stream.
func (h *StreamsHandler) Stop(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	streamID := chi.URLParam(r, "streamID")

	// Get stream info before stopping so we know the runner ID
	stream, err := h.streamManager.GetStream(ctx, streamID)
	if err != nil {
		if err == streaming.ErrStreamNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "stream not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Send StopDesktopStream command to the runner
	if h.cmdSender != nil && stream.RunnerID != "" && stream.Type == streaming.StreamTypeDesktop {
		cmd := &pb.ServerCommand{
			Payload: &pb.ServerCommand_StopDesktopStream{
				StopDesktopStream: &pb.StopDesktopStream{
					StreamId: stream.ID,
					Reason:   "user_requested",
				},
			},
		}

		if err := h.cmdSender.SendCommand(stream.RunnerID, cmd); err != nil {
			h.logger.Error("failed to send StopDesktopStream command",
				zap.String("stream_id", stream.ID),
				zap.String("runner_id", stream.RunnerID),
				zap.Error(err),
			)
			// Don't fail the request, the stream will be stopped in DB
		} else {
			h.logger.Info("sent StopDesktopStream command",
				zap.String("stream_id", stream.ID),
				zap.String("runner_id", stream.RunnerID),
			)
		}
	}

	if err := h.streamManager.StopStream(ctx, streamID); err != nil {
		if err == streaming.ErrStreamNotFound {
			WriteError(w, http.StatusNotFound, "not_found", "stream not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// streamToResponse converts a Stream to StreamResponse.
func streamToResponse(s *streaming.Stream) *StreamResponse {
	resp := &StreamResponse{
		ID:               s.ID,
		SessionID:        s.SessionID,
		RunnerID:         s.RunnerID,
		Type:             string(s.Type),
		State:            string(s.State),
		SignalingURL:     s.SignalingURL,
		FrameRate:        s.FrameRate,
		BitRate:          s.BitRate,
		VideoCodec:       s.VideoCodec,
		AudioCodec:       s.AudioCodec,
		AudioEnabled:     s.AudioEnabled,
		InputEnabled:     s.InputEnabled,
		ProviderName:     s.ProviderName,
		ProviderStreamID: s.ProviderStreamID,
		Error:            s.Error,
		Metadata:         s.Metadata,
		CreatedAt:        s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:        s.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	if !s.Resolution.IsZero() {
		resp.Resolution = &ResolutionRequest{
			Width:  s.Resolution.Width,
			Height: s.Resolution.Height,
		}
	}

	if s.StartedAt != nil {
		resp.StartedAt = s.StartedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if s.StoppedAt != nil {
		resp.StoppedAt = s.StoppedAt.Format("2006-01-02T15:04:05Z07:00")
	}

	return resp
}

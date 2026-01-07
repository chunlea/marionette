package admin

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// StreamService interface for stream operations.
type StreamService interface {
	StartDesktopStream(ctx context.Context, input *core.StartStreamInput) (*core.StreamInfo, error)
	StopDesktopStream(ctx context.Context, streamID string) error
	GetStream(ctx context.Context, streamID string) (*core.StreamInfo, error)
	ListSessionStreams(ctx context.Context, sessionID string) ([]*core.StreamInfo, error)
}

// StartStreamRequest is the request body for starting a stream.
type StartStreamRequest struct {
	Config *StreamConfigRequest `json:"config,omitempty"`
}

// StreamConfigRequest is the stream configuration from the request.
type StreamConfigRequest struct {
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	FrameRate    int    `json:"frame_rate,omitempty"`
	Bitrate      int    `json:"bitrate,omitempty"`
	VideoCodec   string `json:"video_codec,omitempty"`
	AudioEnabled bool   `json:"audio_enabled,omitempty"`
	InputEnabled bool   `json:"input_enabled,omitempty"`
	Display      string `json:"display,omitempty"`
	HWAccel      string `json:"hw_accel,omitempty"`
}

// StreamResponse is the response for stream operations.
type StreamResponse struct {
	ID           string                `json:"id"`
	SessionID    string                `json:"session_id"`
	RunnerID     string                `json:"runner_id,omitempty"`
	Status       string                `json:"status"`
	SignalingURL string                `json:"signaling_url,omitempty"`
	Config       *StreamConfigResponse `json:"config,omitempty"`
	Provider     string                `json:"provider,omitempty"`
	StartedAt    *string               `json:"started_at,omitempty"`
	StoppedAt    *string               `json:"stopped_at,omitempty"`
}

// StreamConfigResponse is the stream configuration in the response.
type StreamConfigResponse struct {
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	FrameRate    int    `json:"frame_rate,omitempty"`
	Bitrate      int    `json:"bitrate,omitempty"`
	VideoCodec   string `json:"video_codec,omitempty"`
	AudioEnabled bool   `json:"audio_enabled,omitempty"`
	InputEnabled bool   `json:"input_enabled,omitempty"`
	Display      string `json:"display,omitempty"`
	HWAccel      string `json:"hw_accel,omitempty"`
}

// handleStartDesktopStream starts a desktop stream for a session.
// POST /admin/api/v1/sessions/{sessionID}/streams/desktop
func (s *Server) handleStartDesktopStream(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		WriteError(w, http.StatusServiceUnavailable, "STREAM_SERVICE_UNAVAILABLE", "stream service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "session_id is required")
		return
	}

	var req StartStreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	// Build input
	input := &core.StartStreamInput{
		SessionID: sessionID,
	}

	if req.Config != nil {
		input.Config = &core.StreamConfig{
			Width:        req.Config.Width,
			Height:       req.Config.Height,
			FrameRate:    req.Config.FrameRate,
			Bitrate:      req.Config.Bitrate,
			VideoCodec:   req.Config.VideoCodec,
			AudioEnabled: req.Config.AudioEnabled,
			InputEnabled: req.Config.InputEnabled,
			Display:      req.Config.Display,
			HWAccel:      req.Config.HWAccel,
		}
	}

	stream, err := s.streams.StartDesktopStream(r.Context(), input)
	if err != nil {
		s.logger.Error("failed to start desktop stream",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		WriteError(w, http.StatusInternalServerError, "START_STREAM_FAILED", err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, streamInfoToResponse(stream))
}

// handleStopDesktopStream stops a desktop stream.
// DELETE /admin/api/v1/streams/{streamID}
func (s *Server) handleStopDesktopStream(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		WriteError(w, http.StatusServiceUnavailable, "STREAM_SERVICE_UNAVAILABLE", "stream service not configured")
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_STREAM_ID", "stream_id is required")
		return
	}

	if err := s.streams.StopDesktopStream(r.Context(), streamID); err != nil {
		s.logger.Error("failed to stop desktop stream",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		WriteError(w, http.StatusInternalServerError, "STOP_STREAM_FAILED", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetStream returns stream information.
// GET /admin/api/v1/streams/{streamID}
func (s *Server) handleGetStream(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		WriteError(w, http.StatusServiceUnavailable, "STREAM_SERVICE_UNAVAILABLE", "stream service not configured")
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_STREAM_ID", "stream_id is required")
		return
	}

	stream, err := s.streams.GetStream(r.Context(), streamID)
	if err != nil {
		WriteError(w, http.StatusNotFound, "STREAM_NOT_FOUND", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, streamInfoToResponse(stream))
}

// handleListSessionStreams lists all streams for a session.
// GET /admin/api/v1/sessions/{sessionID}/streams
func (s *Server) handleListSessionStreams(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		WriteError(w, http.StatusServiceUnavailable, "STREAM_SERVICE_UNAVAILABLE", "stream service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "session_id is required")
		return
	}

	streams, err := s.streams.ListSessionStreams(r.Context(), sessionID)
	if err != nil {
		s.logger.Error("failed to list session streams",
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		WriteError(w, http.StatusInternalServerError, "LIST_STREAMS_FAILED", err.Error())
		return
	}

	response := make([]*StreamResponse, len(streams))
	for i, stream := range streams {
		response[i] = streamInfoToResponse(stream)
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items":       response,
		"total_count": len(response),
	})
}

// streamInfoToResponse converts StreamInfo to StreamResponse.
func streamInfoToResponse(info *core.StreamInfo) *StreamResponse {
	resp := &StreamResponse{
		ID:           info.ID,
		SessionID:    info.SessionID,
		RunnerID:     info.RunnerID,
		Status:       info.Status,
		SignalingURL: info.SignalingURL,
		Provider:     info.Provider,
	}

	if info.Config != nil {
		resp.Config = &StreamConfigResponse{
			Width:        info.Config.Width,
			Height:       info.Config.Height,
			FrameRate:    info.Config.FrameRate,
			Bitrate:      info.Config.Bitrate,
			VideoCodec:   info.Config.VideoCodec,
			AudioEnabled: info.Config.AudioEnabled,
			InputEnabled: info.Config.InputEnabled,
			Display:      info.Config.Display,
			HWAccel:      info.Config.HWAccel,
		}
	}

	if info.StartedAt != nil {
		t := info.StartedAt.Format(timeFormat)
		resp.StartedAt = &t
	}

	if info.StoppedAt != nil {
		t := info.StoppedAt.Format(timeFormat)
		resp.StoppedAt = &t
	}

	return resp
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

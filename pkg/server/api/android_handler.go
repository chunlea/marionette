package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleStartAndroidStream handles POST /sessions/{sessionID}/android/streams
func (s *Server) handleStartAndroidStream(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "session ID is required")
		return
	}

	var req StartAndroidStreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.DeviceSerial == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "device_serial is required")
		return
	}

	opts := StartAndroidStreamOptions{
		SessionID:    sessionID,
		DeviceSerial: req.DeviceSerial,
		MaxWidth:     req.MaxWidth,
		MaxHeight:    req.MaxHeight,
		MaxFPS:       req.MaxFPS,
		Bitrate:      req.Bitrate,
		AudioEnabled: req.AudioEnabled,
	}

	stream, err := s.androidStreams.StartStream(r.Context(), opts)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "stream_failed", err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, stream)
}

// handleListAndroidStreams handles GET /sessions/{sessionID}/android/streams
func (s *Server) handleListAndroidStreams(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "session ID is required")
		return
	}

	opts := ListAndroidStreamsOptions{
		SessionID:     sessionID,
		IncludeClosed: r.URL.Query().Get("include_closed") == "true",
	}

	if state := r.URL.Query()["state"]; len(state) > 0 {
		opts.State = state
	}

	result, err := s.androidStreams.ListStreams(r.Context(), opts)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// handleGetAndroidStream handles GET /sessions/{sessionID}/android/streams/{streamID}
func (s *Server) handleGetAndroidStream(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "stream ID is required")
		return
	}

	stream, err := s.androidStreams.GetStream(r.Context(), streamID)
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, stream)
}

// handleStopAndroidStream handles DELETE /sessions/{sessionID}/android/streams/{streamID}
func (s *Server) handleStopAndroidStream(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "stream ID is required")
		return
	}

	if err := s.androidStreams.StopStream(r.Context(), streamID); err != nil {
		WriteError(w, http.StatusInternalServerError, "stop_failed", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListAndroidDevices handles GET /sessions/{sessionID}/android/devices
func (s *Server) handleListAndroidDevices(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "session ID is required")
		return
	}

	devices, err := s.androidStreams.ListDevices(r.Context(), sessionID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"devices": devices,
	})
}

// handleSendAndroidInput handles POST /sessions/{sessionID}/android/streams/{streamID}/input
func (s *Server) handleSendAndroidInput(w http.ResponseWriter, r *http.Request) {
	if s.androidStreams == nil {
		WriteError(w, http.StatusNotImplemented, "service_unavailable", "Android streaming service not configured")
		return
	}

	streamID := chi.URLParam(r, "streamID")
	if streamID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "stream ID is required")
		return
	}

	var input AndroidInputEvent
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if input.Type == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "input type is required")
		return
	}

	if err := s.androidStreams.SendInput(r.Context(), streamID, input); err != nil {
		WriteError(w, http.StatusInternalServerError, "input_failed", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// StartAndroidStreamRequest is the request body for starting an Android stream.
type StartAndroidStreamRequest struct {
	DeviceSerial string `json:"device_serial"`
	MaxWidth     int    `json:"max_width,omitempty"`
	MaxHeight    int    `json:"max_height,omitempty"`
	MaxFPS       int    `json:"max_fps,omitempty"`
	Bitrate      int    `json:"bitrate,omitempty"`
	AudioEnabled bool   `json:"audio_enabled,omitempty"`
}

package api

import (
	"encoding/json"
	"net/http"

	"github.com/chunlea/marionette/pkg/server/api/apitypes"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes an error response with the given status code.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, apitypes.ErrorResponse{
		Code:    code,
		Message: message,
	})
}

// contains checks if a slice contains a value.
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// matchLabels checks if the resource labels match the filter labels.
// Filter labels must be a subset of resource labels.
func matchLabels(resourceLabels json.RawMessage, filterLabels map[string]string) bool {
	if len(filterLabels) == 0 {
		return true
	}

	var labels map[string]string
	if err := json.Unmarshal(resourceLabels, &labels); err != nil {
		return false
	}

	for k, v := range filterLabels {
		if labels[k] != v {
			return false
		}
	}
	return true
}

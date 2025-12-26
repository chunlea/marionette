package client

import (
	"errors"
	"fmt"
)

// Common errors returned by the client.
var (
	// ErrNotFound is returned when a resource is not found.
	ErrNotFound = errors.New("not found")

	// ErrUnauthorized is returned when authentication fails.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrForbidden is returned when access is denied.
	ErrForbidden = errors.New("forbidden")

	// ErrConflict is returned when there's a resource conflict.
	ErrConflict = errors.New("conflict")

	// ErrBadRequest is returned when the request is invalid.
	ErrBadRequest = errors.New("bad request")

	// ErrServerError is returned when the server returns an error.
	ErrServerError = errors.New("server error")
)

// APIError represents an error returned by the Marionette API.
type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Details    any    `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// Unwrap returns the underlying error type for errors.Is().
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case 400:
		return ErrBadRequest
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 409:
		return ErrConflict
	default:
		if e.StatusCode >= 500 {
			return ErrServerError
		}
		return nil
	}
}

// IsNotFound returns true if the error indicates a resource was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsUnauthorized returns true if the error indicates an authentication failure.
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

// IsForbidden returns true if the error indicates access was denied.
func IsForbidden(err error) bool {
	return errors.Is(err, ErrForbidden)
}

// IsConflict returns true if the error indicates a resource conflict.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsBadRequest returns true if the error indicates a bad request.
func IsBadRequest(err error) bool {
	return errors.Is(err, ErrBadRequest)
}

// IsServerError returns true if the error indicates a server error.
func IsServerError(err error) bool {
	return errors.Is(err, ErrServerError)
}

package api

import (
	"errors"
	"fmt"
)

// ErrNotImplemented is returned when a route is served but the service behind
// it was never configured.
//
// It is deliberately distinct from an empty result: three routes in this API
// answered 501 for the life of the project because their services were never
// wired, and the failure mode that hid it was endpoints that returned nothing
// and looked like they had simply found nothing.
var ErrNotImplemented = errors.New("service not configured")

// InvalidStateError is returned when an operation is not valid for the current state.
type InvalidStateError struct {
	Resource string // Resource type (e.g., "session", "task")
	ID       string // Resource ID
	Current  string // Current state
	Expected string // Expected state(s)
}

func (e *InvalidStateError) Error() string {
	return fmt.Sprintf("%s %s is in state %q, expected %s", e.Resource, e.ID, e.Current, e.Expected)
}

// IsInvalidState returns true if the error is an InvalidStateError.
func IsInvalidState(err error) bool {
	_, ok := err.(*InvalidStateError)
	return ok
}

// MaxRetriesExceededError is returned when a task has exceeded its retry limit.
type MaxRetriesExceededError struct {
	TaskID     string
	RetryCount int
	MaxRetries int
}

func (e *MaxRetriesExceededError) Error() string {
	return fmt.Sprintf("task %s has exceeded max retries (%d/%d)", e.TaskID, e.RetryCount, e.MaxRetries)
}

// IsMaxRetriesExceeded returns true if the error is a MaxRetriesExceededError.
func IsMaxRetriesExceeded(err error) bool {
	_, ok := err.(*MaxRetriesExceededError)
	return ok
}

// ValidationError is returned when request validation fails.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// IsValidation returns true if the error is a ValidationError.
func IsValidation(err error) bool {
	_, ok := err.(*ValidationError)
	return ok
}

// NotAuthorizedError is returned when the caller is not authorized for an operation.
type NotAuthorizedError struct {
	Operation string
	Resource  string
	ID        string
}

func (e *NotAuthorizedError) Error() string {
	if e.ID != "" {
		return fmt.Sprintf("not authorized to %s %s %s", e.Operation, e.Resource, e.ID)
	}
	return fmt.Sprintf("not authorized to %s %s", e.Operation, e.Resource)
}

// IsNotAuthorized returns true if the error is a NotAuthorizedError.
func IsNotAuthorized(err error) bool {
	_, ok := err.(*NotAuthorizedError)
	return ok
}

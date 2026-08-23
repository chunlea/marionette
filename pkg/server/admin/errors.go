package admin

import "errors"

// ValidationError reports that a request was rejected for bad input rather
// than for a server-side failure. Handlers map it to 400.
//
// This lived inside mock_apikey.go, so the production error type every admin
// handler depends on was defined in a file whose whole purpose was testing.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return e.Field + ": " + e.Message
	}
	return e.Message
}

// IsValidation reports whether err is a ValidationError.
func IsValidation(err error) bool {
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

// InvalidStateError reports that an operation is not valid for the resource's
// current state. Handlers map it to 409.
//
// Like ValidationError, this production type used to live in a mock file.
type InvalidStateError struct {
	Resource string
	ID       string
	Current  string
	Expected string
}

func (e *InvalidStateError) Error() string {
	return e.Resource + " " + e.ID + " is in state " + e.Current + ", expected " + e.Expected
}

// IsInvalidState reports whether err is an InvalidStateError.
func IsInvalidState(err error) bool {
	var stateErr *InvalidStateError
	return errors.As(err, &stateErr)
}

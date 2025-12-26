package client

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIError(t *testing.T) {
	t.Run("Error message with code", func(t *testing.T) {
		err := &APIError{
			StatusCode: 404,
			Code:       "not_found",
			Message:    "Session not found",
		}
		assert.Equal(t, "not_found: Session not found", err.Error())
	})

	t.Run("Error message without code", func(t *testing.T) {
		err := &APIError{
			StatusCode: 500,
			Message:    "Internal server error",
		}
		assert.Equal(t, "Internal server error", err.Error())
	})
}

func TestAPIError_Unwrap(t *testing.T) {
	t.Run("400 unwraps to ErrBadRequest", func(t *testing.T) {
		err := &APIError{StatusCode: 400}
		assert.ErrorIs(t, err, ErrBadRequest)
	})

	t.Run("401 unwraps to ErrUnauthorized", func(t *testing.T) {
		err := &APIError{StatusCode: 401}
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("403 unwraps to ErrForbidden", func(t *testing.T) {
		err := &APIError{StatusCode: 403}
		assert.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("404 unwraps to ErrNotFound", func(t *testing.T) {
		err := &APIError{StatusCode: 404}
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("409 unwraps to ErrConflict", func(t *testing.T) {
		err := &APIError{StatusCode: 409}
		assert.ErrorIs(t, err, ErrConflict)
	})

	t.Run("500 unwraps to ErrServerError", func(t *testing.T) {
		err := &APIError{StatusCode: 500}
		assert.ErrorIs(t, err, ErrServerError)
	})

	t.Run("502 unwraps to ErrServerError", func(t *testing.T) {
		err := &APIError{StatusCode: 502}
		assert.ErrorIs(t, err, ErrServerError)
	})

	t.Run("unknown status returns nil", func(t *testing.T) {
		err := &APIError{StatusCode: 418} // I'm a teapot
		assert.Nil(t, err.Unwrap())
	})
}

func TestIsNotFound(t *testing.T) {
	t.Run("404 error", func(t *testing.T) {
		err := &APIError{StatusCode: 404, Code: "not_found"}
		assert.True(t, IsNotFound(err))
	})

	t.Run("ErrNotFound", func(t *testing.T) {
		assert.True(t, IsNotFound(ErrNotFound))
	})

	t.Run("other status code", func(t *testing.T) {
		err := &APIError{StatusCode: 500, Code: "server_error"}
		assert.False(t, IsNotFound(err))
	})

	t.Run("non-API error", func(t *testing.T) {
		err := errors.New("some error")
		assert.False(t, IsNotFound(err))
	})

	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsNotFound(nil))
	})
}

func TestIsUnauthorized(t *testing.T) {
	t.Run("401 error", func(t *testing.T) {
		err := &APIError{StatusCode: 401}
		assert.True(t, IsUnauthorized(err))
	})

	t.Run("ErrUnauthorized", func(t *testing.T) {
		assert.True(t, IsUnauthorized(ErrUnauthorized))
	})

	t.Run("other error", func(t *testing.T) {
		err := &APIError{StatusCode: 404}
		assert.False(t, IsUnauthorized(err))
	})
}

func TestIsForbidden(t *testing.T) {
	t.Run("403 error", func(t *testing.T) {
		err := &APIError{StatusCode: 403}
		assert.True(t, IsForbidden(err))
	})

	t.Run("ErrForbidden", func(t *testing.T) {
		assert.True(t, IsForbidden(ErrForbidden))
	})

	t.Run("other error", func(t *testing.T) {
		err := &APIError{StatusCode: 404}
		assert.False(t, IsForbidden(err))
	})
}

func TestIsConflict(t *testing.T) {
	t.Run("409 error", func(t *testing.T) {
		err := &APIError{StatusCode: 409, Code: "conflict"}
		assert.True(t, IsConflict(err))
	})

	t.Run("ErrConflict", func(t *testing.T) {
		assert.True(t, IsConflict(ErrConflict))
	})

	t.Run("other error", func(t *testing.T) {
		err := &APIError{StatusCode: 404, Code: "not_found"}
		assert.False(t, IsConflict(err))
	})

	t.Run("non-API error", func(t *testing.T) {
		err := errors.New("some error")
		assert.False(t, IsConflict(err))
	})
}

func TestIsBadRequest(t *testing.T) {
	t.Run("400 error", func(t *testing.T) {
		err := &APIError{StatusCode: 400, Code: "bad_request"}
		assert.True(t, IsBadRequest(err))
	})

	t.Run("ErrBadRequest", func(t *testing.T) {
		assert.True(t, IsBadRequest(ErrBadRequest))
	})

	t.Run("other error", func(t *testing.T) {
		err := &APIError{StatusCode: 500, Code: "server_error"}
		assert.False(t, IsBadRequest(err))
	})

	t.Run("non-API error", func(t *testing.T) {
		err := errors.New("some error")
		assert.False(t, IsBadRequest(err))
	})
}

func TestIsServerError(t *testing.T) {
	t.Run("500 error", func(t *testing.T) {
		err := &APIError{StatusCode: 500, Code: "internal_error"}
		assert.True(t, IsServerError(err))
	})

	t.Run("502 error", func(t *testing.T) {
		err := &APIError{StatusCode: 502, Code: "bad_gateway"}
		assert.True(t, IsServerError(err))
	})

	t.Run("503 error", func(t *testing.T) {
		err := &APIError{StatusCode: 503, Code: "service_unavailable"}
		assert.True(t, IsServerError(err))
	})

	t.Run("ErrServerError", func(t *testing.T) {
		assert.True(t, IsServerError(ErrServerError))
	})

	t.Run("499 error (not server)", func(t *testing.T) {
		err := &APIError{StatusCode: 499, Code: "client_closed"}
		assert.False(t, IsServerError(err))
	})

	t.Run("non-API error", func(t *testing.T) {
		err := errors.New("some error")
		assert.False(t, IsServerError(err))
	})
}

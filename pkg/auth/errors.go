// Package auth provides authentication services for Marionette.
package auth

import "errors"

// Error definitions for the auth package.
var (
	// ErrInvalidToken indicates the token format is invalid.
	ErrInvalidToken = errors.New("invalid token")

	// ErrTokenRevoked indicates the token has been revoked.
	ErrTokenRevoked = errors.New("token revoked")

	// ErrTokenExpired indicates the token has expired.
	ErrTokenExpired = errors.New("token expired")

	// ErrTokenNotFound indicates the token does not exist.
	ErrTokenNotFound = errors.New("token not found")

	// ErrInvalidPrefix indicates the token has an invalid prefix.
	ErrInvalidPrefix = errors.New("invalid token prefix")

	// ErrInsufficientScope indicates the token lacks required permissions.
	ErrInsufficientScope = errors.New("insufficient scope")

	// ErrInvalidName indicates the name is invalid.
	ErrInvalidName = errors.New("invalid name")

	// ErrInvalidPoolName indicates the pool name is invalid.
	ErrInvalidPoolName = errors.New("invalid pool name")
)

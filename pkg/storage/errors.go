package storage

import "errors"

// Storage-level errors.
var (
	// ErrNotFound is returned when the requested object does not exist.
	ErrNotFound = errors.New("object not found")

	// ErrAlreadyExists is returned when attempting to create an object that already exists.
	ErrAlreadyExists = errors.New("object already exists")

	// ErrStorageUnavailable is returned when the storage backend is unavailable.
	ErrStorageUnavailable = errors.New("storage unavailable")

	// ErrInvalidKey is returned when the storage key is invalid.
	ErrInvalidKey = errors.New("invalid storage key")
)

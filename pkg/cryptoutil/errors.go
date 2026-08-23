package cryptoutil

import "errors"

// Error definitions for the cryptoutil package.
var (
	// ErrInvalidKEK indicates the Key Encryption Key is invalid.
	ErrInvalidKEK = errors.New("invalid key encryption key")

	// ErrDecryptionFailed indicates decryption failed (tampered data or wrong key).
	ErrDecryptionFailed = errors.New("decryption failed")

	// ErrDEKNotFound indicates no Data Encryption Key exists for the resource.
	//
	// A DEKStore implementation MUST return an error matching this via
	// errors.Is when a key is absent. The store layer has its own not-found
	// error, so the adapter is where the translation belongs; without it the
	// service cannot tell "no key yet" from "the database is broken" and the
	// first encryption for a resource fails instead of creating a key.
	ErrDEKNotFound = errors.New("data encryption key not found")

	// ErrDEKExists indicates a DEK for the resource was created concurrently.
	//
	// The loser of that race must adopt the winner's key: two DEKs for one
	// resource means data encrypted under the discarded one can never be read.
	ErrDEKExists = errors.New("data encryption key already exists")
)

package crypto

import "errors"

// Error definitions for the crypto package.
var (
	// ErrInvalidKEK indicates the Key Encryption Key is invalid.
	ErrInvalidKEK = errors.New("invalid key encryption key")

	// ErrDecryptionFailed indicates decryption failed (tampered data or wrong key).
	ErrDecryptionFailed = errors.New("decryption failed")

	// ErrDEKNotFound indicates no Data Encryption Key exists for the resource.
	ErrDEKNotFound = errors.New("data encryption key not found")
)

package cas

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunksMissingError_Error(t *testing.T) {
	// Test with storage missing
	err := &ChunksMissingError{
		ManifestID: "mfst_123",
		Missing:    []string{"hash1", "hash2"},
		InStorage:  true,
	}
	msg := err.Error()
	assert.Contains(t, msg, "mfst_123")
	assert.Contains(t, msg, "2 chunks")
	assert.Contains(t, msg, "object storage")

	// Test with database missing
	err2 := &ChunksMissingError{
		ManifestID: "mfst_456",
		Missing:    []string{"hash1"},
		InStorage:  false,
	}
	msg2 := err2.Error()
	assert.Contains(t, msg2, "database")
}

func TestPathTraversalError_Error(t *testing.T) {
	err := &PathTraversalError{Path: "../../../etc/passwd"}
	msg := err.Error()
	assert.Contains(t, msg, "path traversal")
	assert.Contains(t, msg, "../../../etc/passwd")
}

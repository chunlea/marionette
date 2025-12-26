// Package cas provides Content-Addressable Storage for workspace synchronization.
package cas

import (
	"io/fs"
	"time"
)

// ChunkInfo represents a chunk with its hash and data.
type ChunkInfo struct {
	// Hash is the SHA-256 hash of the uncompressed chunk data.
	Hash string

	// Size is the uncompressed size in bytes.
	Size int64

	// Data contains the chunk data (may be nil for metadata-only operations).
	Data []byte
}

// Manifest represents a workspace snapshot for CAS operations.
// This is the in-memory representation used during sync/restore.
type Manifest struct {
	// ID is the manifest identifier (mfst_xxx).
	ID string

	// WorkspaceID is the workspace this manifest belongs to.
	WorkspaceID string

	// ParentID is the parent manifest for incremental snapshots.
	ParentID *string

	// TenantID for tenant isolation.
	TenantID string

	// CreatedAt is when the manifest was created.
	CreatedAt time.Time

	// TotalSize is the total uncompressed size of all files.
	TotalSize int64

	// SingleChunk indicates if the workspace is stored as a single tar.zst chunk.
	// Used for workspaces smaller than SingleChunkThreshold.
	SingleChunk bool

	// ChunkHash is the hash of the single chunk (if SingleChunk is true).
	ChunkHash *string

	// ChunkCount is the total number of unique chunks (for CDC mode).
	ChunkCount int

	// Files contains the file entries (for CDC mode).
	Files []ManifestFile
}

// ManifestHeader is the first line of a JSONL manifest.
// It contains metadata without the file list.
type ManifestHeader struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ParentID    string    `json:"parent_id,omitempty"`
	TenantID    string    `json:"tenant_id"`
	CreatedAt   time.Time `json:"created_at"`
	TotalSize   int64     `json:"total_size"`
	ChunkCount  int       `json:"chunk_count"`
	SingleChunk bool      `json:"single_chunk,omitempty"`
	ChunkHash   string    `json:"chunk_hash,omitempty"`
}

// ManifestFile represents a file entry in a manifest.
// Each file is stored as a separate line in the JSONL manifest.
type ManifestFile struct {
	// Path is the relative path from workspace root.
	Path string `json:"path"`

	// Mode is the file permission bits.
	Mode fs.FileMode `json:"mode"`

	// ModTime is the file modification time.
	ModTime time.Time `json:"mod_time"`

	// Size is the uncompressed file size in bytes.
	Size int64 `json:"size"`

	// Chunks contains the ordered list of chunk hashes that make up this file.
	Chunks []string `json:"chunks"`
}

// CollectChunkHashes returns all unique chunk hashes in the manifest.
func (m *Manifest) CollectChunkHashes() []string {
	if m.SingleChunk && m.ChunkHash != nil {
		return []string{*m.ChunkHash}
	}

	seen := make(map[string]struct{})
	var hashes []string
	for _, f := range m.Files {
		for _, h := range f.Chunks {
			if _, ok := seen[h]; !ok {
				seen[h] = struct{}{}
				hashes = append(hashes, h)
			}
		}
	}
	return hashes
}

// ToHeader converts the manifest to a ManifestHeader for JSONL serialization.
func (m *Manifest) ToHeader() ManifestHeader {
	header := ManifestHeader{
		ID:          m.ID,
		WorkspaceID: m.WorkspaceID,
		TenantID:    m.TenantID,
		CreatedAt:   m.CreatedAt,
		TotalSize:   m.TotalSize,
		ChunkCount:  m.ChunkCount,
		SingleChunk: m.SingleChunk,
	}
	if m.ParentID != nil {
		header.ParentID = *m.ParentID
	}
	if m.ChunkHash != nil {
		header.ChunkHash = *m.ChunkHash
	}
	return header
}

// FromHeader creates a Manifest from a ManifestHeader.
func FromHeader(h ManifestHeader) *Manifest {
	m := &Manifest{
		ID:          h.ID,
		WorkspaceID: h.WorkspaceID,
		TenantID:    h.TenantID,
		CreatedAt:   h.CreatedAt,
		TotalSize:   h.TotalSize,
		ChunkCount:  h.ChunkCount,
		SingleChunk: h.SingleChunk,
	}
	if h.ParentID != "" {
		m.ParentID = &h.ParentID
	}
	if h.ChunkHash != "" {
		m.ChunkHash = &h.ChunkHash
	}
	return m
}

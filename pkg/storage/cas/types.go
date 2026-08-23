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

	// Ordered reports that Files are in directory-walk order. See
	// ManifestHeader.Ordered.
	Ordered bool

	// FileCount is the number of entries in the manifest. For a manifest
	// loaded in streaming mode Files is empty and this is the only record of
	// how many there were.
	FileCount int

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

	// Ordered reports that the file entries appear in directory-walk order.
	//
	// An ordered manifest can be compared against a fresh walk by streaming
	// both and merging, which is what lets an incremental sync of a
	// million-file workspace hold nothing but the two entries it is looking
	// at. Manifests written before this flag existed, and those assembled out
	// of order by the old incremental path, leave it false and are indexed in
	// memory instead.
	Ordered bool `json:"ordered,omitempty"`

	// FileCount is the number of entries that follow the header. Zero means
	// unknown, which is what manifests written before this field say.
	FileCount int `json:"file_count,omitempty"`
}

// Entry kinds recorded in a manifest.
//
// The empty string means a regular file so that manifests written before
// directories and symlinks were recorded still read correctly: every entry they
// contain was a regular file.
const (
	// EntryFile is a regular file. Also the zero value.
	EntryFile = ""

	// EntryDir is a directory. Recorded so an empty directory survives a
	// round trip - a workspace whose build output directory disappears on
	// restore is not the workspace that was saved.
	EntryDir = "d"

	// EntrySymlink is a symbolic link. The target is stored verbatim and is
	// never followed: following it would inline the target's bytes under the
	// link's name and silently turn one file into two.
	EntrySymlink = "l"
)

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

	// Type is the entry kind: EntryFile (default), EntryDir or EntrySymlink.
	Type string `json:"type,omitempty"`

	// Link is the symlink target, for EntrySymlink entries.
	Link string `json:"link,omitempty"`
}

// IsDir reports whether the entry is a directory.
func (f ManifestFile) IsDir() bool { return f.Type == EntryDir }

// IsSymlink reports whether the entry is a symbolic link.
func (f ManifestFile) IsSymlink() bool { return f.Type == EntrySymlink }

// IsRegular reports whether the entry is a regular file.
func (f ManifestFile) IsRegular() bool { return f.Type == EntryFile }

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
		Ordered:     m.Ordered,
		FileCount:   m.FileCount,
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
		Ordered:     h.Ordered,
		FileCount:   h.FileCount,
	}
	if h.ParentID != "" {
		m.ParentID = &h.ParentID
	}
	if h.ChunkHash != "" {
		m.ChunkHash = &h.ChunkHash
	}
	return m
}

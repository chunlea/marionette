package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// Common errors for workspace operations.
var (
	ErrWorkspaceNotFound = errors.New("workspace not found")
	ErrWorkspaceExists   = errors.New("workspace already exists")
	ErrInvalidPath       = errors.New("invalid workspace path")
)

// WorkspaceInfo contains information about a workspace.
type WorkspaceInfo struct {
	Path      string
	Exists    bool
	SizeBytes int64
	FileCount int
}

// WorkspaceManager handles workspace creation and management on the runner.
type WorkspaceManager struct {
	baseDir string
	logger  *zap.Logger

	// Track managed workspaces
	workspaces   map[string]bool
	workspacesMu sync.RWMutex
}

// NewWorkspaceManager creates a new workspace manager.
// baseDir is the base directory for workspaces (default: /workspace).
func NewWorkspaceManager(baseDir string, logger *zap.Logger) *WorkspaceManager {
	if baseDir == "" {
		baseDir = "/workspace"
	}

	return &WorkspaceManager{
		baseDir:    baseDir,
		logger:     logger.Named("workspace"),
		workspaces: make(map[string]bool),
	}
}

// BaseDir returns the base directory for workspaces.
func (m *WorkspaceManager) BaseDir() string {
	return m.baseDir
}

// Create creates a new workspace directory.
func (m *WorkspaceManager) Create(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	// Resolve path
	absPath := m.resolvePath(path)

	m.workspacesMu.Lock()
	defer m.workspacesMu.Unlock()

	// Check if already tracked
	if m.workspaces[absPath] {
		return ErrWorkspaceExists
	}

	// Create directory with appropriate permissions
	if err := os.MkdirAll(absPath, 0750); err != nil {
		return fmt.Errorf("creating workspace directory: %w", err)
	}

	m.workspaces[absPath] = true
	m.logger.Info("workspace created", zap.String("path", absPath))

	return nil
}

// EnsureExists ensures a workspace directory exists, creating it if necessary.
func (m *WorkspaceManager) EnsureExists(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	absPath := m.resolvePath(path)

	// Check if directory exists
	info, err := os.Stat(absPath)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("path exists but is not a directory: %s", absPath)
		}
		// Directory exists, track it
		m.workspacesMu.Lock()
		m.workspaces[absPath] = true
		m.workspacesMu.Unlock()
		return nil
	}

	if !os.IsNotExist(err) {
		return fmt.Errorf("checking workspace: %w", err)
	}

	// Create directory
	return m.Create(path)
}

// Delete removes a workspace directory.
func (m *WorkspaceManager) Delete(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	absPath := m.resolvePath(path)

	m.workspacesMu.Lock()
	defer m.workspacesMu.Unlock()

	// Check if tracked
	if !m.workspaces[absPath] {
		// Check if directory exists anyway
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return ErrWorkspaceNotFound
		}
	}

	// Remove directory and all contents
	if err := os.RemoveAll(absPath); err != nil {
		return fmt.Errorf("removing workspace: %w", err)
	}

	delete(m.workspaces, absPath)
	m.logger.Info("workspace deleted", zap.String("path", absPath))

	return nil
}

// Exists checks if a workspace directory exists.
func (m *WorkspaceManager) Exists(path string) bool {
	if path == "" {
		return false
	}

	absPath := m.resolvePath(path)

	info, err := os.Stat(absPath)
	if err != nil {
		return false
	}

	return info.IsDir()
}

// Info returns information about a workspace.
func (m *WorkspaceManager) Info(path string) (*WorkspaceInfo, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}

	absPath := m.resolvePath(path)

	info := &WorkspaceInfo{
		Path:   absPath,
		Exists: false,
	}

	// Check if exists
	dirInfo, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return nil, fmt.Errorf("stat workspace: %w", err)
	}

	if !dirInfo.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absPath)
	}

	info.Exists = true

	// Calculate size and file count
	var totalSize int64
	var fileCount int

	_ = filepath.WalkDir(absPath, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip entries with errors
			return nil //nolint:nilerr // intentionally skip errors during walk
		}

		if !d.IsDir() {
			fileCount++
			if fileInfo, infoErr := d.Info(); infoErr == nil {
				totalSize += fileInfo.Size()
			}
		}
		return nil
	})

	info.SizeBytes = totalSize
	info.FileCount = fileCount

	return info, nil
}

// ListFiles lists files in a workspace directory.
func (m *WorkspaceManager) ListFiles(path string, maxDepth int) ([]string, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}

	absPath := m.resolvePath(path)

	if !m.Exists(path) {
		return nil, ErrWorkspaceNotFound
	}

	var files []string

	_ = filepath.WalkDir(absPath, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip entries with errors
			return nil //nolint:nilerr // intentionally skip errors during walk
		}

		// Calculate depth (number of path components)
		relPath, _ := filepath.Rel(absPath, filePath)
		depth := 0
		if relPath != "." {
			// Count path separators + 1 = number of components
			// "root.txt" -> depth 1, "level1/file.txt" -> depth 2
			for _, c := range relPath {
				if c == filepath.Separator {
					depth++
				}
			}
			depth++ // Add 1 for the file itself
		}

		if maxDepth > 0 && depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			files = append(files, relPath)
		}

		return nil
	})

	return files, nil
}

// Clean removes all files from a workspace but keeps the directory.
func (m *WorkspaceManager) Clean(path string) error {
	if path == "" {
		return ErrInvalidPath
	}

	absPath := m.resolvePath(path)

	if !m.Exists(path) {
		return ErrWorkspaceNotFound
	}

	// Read directory entries
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Errorf("reading workspace: %w", err)
	}

	// Remove each entry
	for _, entry := range entries {
		entryPath := filepath.Join(absPath, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			m.logger.Warn("error removing entry",
				zap.String("path", entryPath),
				zap.Error(err),
			)
		}
	}

	m.logger.Info("workspace cleaned", zap.String("path", absPath))
	return nil
}

// resolvePath resolves a workspace path.
// If absolute, returns the cleaned path; if relative, joins with baseDir.
func (m *WorkspaceManager) resolvePath(path string) string {
	// If path is already absolute, use it directly
	// Security note: In production, this should be restricted to specific base directories
	// For testing flexibility, we allow any absolute path
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	// Relative path - join with base directory
	return filepath.Join(m.baseDir, path)
}

// TrackedWorkspaces returns a list of tracked workspace paths.
func (m *WorkspaceManager) TrackedWorkspaces() []string {
	m.workspacesMu.RLock()
	defer m.workspacesMu.RUnlock()

	paths := make([]string, 0, len(m.workspaces))
	for path := range m.workspaces {
		paths = append(paths, path)
	}
	return paths
}

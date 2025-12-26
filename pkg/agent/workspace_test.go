package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestWorkspaceManager_Create(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Create workspace
	wsPath := filepath.Join(baseDir, "test-workspace")
	err := mgr.Create(wsPath)
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(wsPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Try to create again - should fail
	err = mgr.Create(wsPath)
	assert.ErrorIs(t, err, ErrWorkspaceExists)
}

func TestWorkspaceManager_CreateRelativePath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Create with relative path
	err := mgr.Create("my-workspace")
	require.NoError(t, err)

	// Verify directory was created under base
	wsPath := filepath.Join(baseDir, "my-workspace")
	info, err := os.Stat(wsPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestWorkspaceManager_CreateEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	err := mgr.Create("")
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestWorkspaceManager_EnsureExists(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "ensure-test")

	// First call should create
	err := mgr.EnsureExists(wsPath)
	require.NoError(t, err)
	assert.True(t, mgr.Exists(wsPath))

	// Second call should succeed (already exists)
	err = mgr.EnsureExists(wsPath)
	require.NoError(t, err)
}

func TestWorkspaceManager_EnsureExists_NotDirectory(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Create a file instead of directory
	filePath := filepath.Join(baseDir, "not-a-dir")
	err := os.WriteFile(filePath, []byte("test"), 0600)
	require.NoError(t, err)

	// EnsureExists should fail
	err = mgr.EnsureExists(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestWorkspaceManager_Delete(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "delete-test")

	// Create workspace
	err := mgr.Create(wsPath)
	require.NoError(t, err)

	// Create some files inside
	err = os.WriteFile(filepath.Join(wsPath, "file.txt"), []byte("test"), 0600)
	require.NoError(t, err)

	// Delete
	err = mgr.Delete(wsPath)
	require.NoError(t, err)

	// Verify deleted
	assert.False(t, mgr.Exists(wsPath))
}

func TestWorkspaceManager_DeleteNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	err := mgr.Delete("/nonexistent/path")
	assert.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestWorkspaceManager_DeleteEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	err := mgr.Delete("")
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestWorkspaceManager_Exists(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Non-existent
	assert.False(t, mgr.Exists(filepath.Join(baseDir, "nope")))

	// Create and check
	wsPath := filepath.Join(baseDir, "exists-test")
	err := mgr.Create(wsPath)
	require.NoError(t, err)
	assert.True(t, mgr.Exists(wsPath))

	// Empty path
	assert.False(t, mgr.Exists(""))
}

func TestWorkspaceManager_Info(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "info-test")

	// Non-existent
	info, err := mgr.Info(wsPath)
	require.NoError(t, err)
	assert.False(t, info.Exists)
	assert.Equal(t, wsPath, info.Path)

	// Create workspace
	err = mgr.Create(wsPath)
	require.NoError(t, err)

	// Add some files
	err = os.WriteFile(filepath.Join(wsPath, "file1.txt"), []byte("hello"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "file2.txt"), []byte("world!"), 0600)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(wsPath, "subdir"), 0750)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "subdir", "file3.txt"), []byte("nested"), 0600)
	require.NoError(t, err)

	// Get info
	info, err = mgr.Info(wsPath)
	require.NoError(t, err)
	assert.True(t, info.Exists)
	assert.Equal(t, 3, info.FileCount) // 3 files
	assert.Equal(t, int64(5+6+6), info.SizeBytes)
}

func TestWorkspaceManager_InfoEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	_, err := mgr.Info("")
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestWorkspaceManager_ListFiles(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "list-test")
	err := mgr.Create(wsPath)
	require.NoError(t, err)

	// Add files
	err = os.WriteFile(filepath.Join(wsPath, "file1.txt"), []byte("1"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "file2.txt"), []byte("2"), 0600)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(wsPath, "subdir"), 0750)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "subdir", "file3.txt"), []byte("3"), 0600)
	require.NoError(t, err)

	// List all files
	files, err := mgr.ListFiles(wsPath, 0)
	require.NoError(t, err)
	assert.Len(t, files, 3)
}

func TestWorkspaceManager_ListFiles_MaxDepth(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "depth-test")
	err := mgr.Create(wsPath)
	require.NoError(t, err)

	// Add files at different depths
	err = os.WriteFile(filepath.Join(wsPath, "root.txt"), []byte("1"), 0600)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(wsPath, "level1"), 0750)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "level1", "file1.txt"), []byte("2"), 0600)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(wsPath, "level1", "level2"), 0750)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "level1", "level2", "file2.txt"), []byte("3"), 0600)
	require.NoError(t, err)

	// List with depth 1 - should only get root.txt
	files, err := mgr.ListFiles(wsPath, 1)
	require.NoError(t, err)
	assert.Len(t, files, 1)
	assert.Contains(t, files, "root.txt")
}

func TestWorkspaceManager_ListFilesNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	_, err := mgr.ListFiles("/nonexistent", 0)
	assert.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestWorkspaceManager_ListFilesEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	_, err := mgr.ListFiles("", 0)
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestWorkspaceManager_Clean(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	wsPath := filepath.Join(baseDir, "clean-test")
	err := mgr.Create(wsPath)
	require.NoError(t, err)

	// Add files
	err = os.WriteFile(filepath.Join(wsPath, "file1.txt"), []byte("1"), 0600)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(wsPath, "subdir"), 0750)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(wsPath, "subdir", "file2.txt"), []byte("2"), 0600)
	require.NoError(t, err)

	// Clean
	err = mgr.Clean(wsPath)
	require.NoError(t, err)

	// Verify workspace still exists but is empty
	assert.True(t, mgr.Exists(wsPath))

	info, err := mgr.Info(wsPath)
	require.NoError(t, err)
	assert.Equal(t, 0, info.FileCount)
}

func TestWorkspaceManager_CleanNotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	err := mgr.Clean("/nonexistent")
	assert.ErrorIs(t, err, ErrWorkspaceNotFound)
}

func TestWorkspaceManager_CleanEmptyPath(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewWorkspaceManager(t.TempDir(), logger)

	err := mgr.Clean("")
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestWorkspaceManager_TrackedWorkspaces(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Initially empty
	assert.Empty(t, mgr.TrackedWorkspaces())

	// Create workspaces
	ws1 := filepath.Join(baseDir, "ws1")
	ws2 := filepath.Join(baseDir, "ws2")

	err := mgr.Create(ws1)
	require.NoError(t, err)
	err = mgr.Create(ws2)
	require.NoError(t, err)

	// Check tracked
	tracked := mgr.TrackedWorkspaces()
	assert.Len(t, tracked, 2)
	assert.Contains(t, tracked, ws1)
	assert.Contains(t, tracked, ws2)

	// Delete one
	err = mgr.Delete(ws1)
	require.NoError(t, err)

	tracked = mgr.TrackedWorkspaces()
	assert.Len(t, tracked, 1)
	assert.Contains(t, tracked, ws2)
}

func TestWorkspaceManager_BaseDir(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Default base dir
	mgr := NewWorkspaceManager("", logger)
	assert.Equal(t, "/workspace", mgr.BaseDir())

	// Custom base dir
	customDir := t.TempDir()
	mgr = NewWorkspaceManager(customDir, logger)
	assert.Equal(t, customDir, mgr.BaseDir())
}

func TestWorkspaceManager_PathValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	// Paths outside base should work if under /tmp (for testing)
	tmpPath := t.TempDir()
	err := mgr.EnsureExists(tmpPath)
	require.NoError(t, err)
}

func TestWorkspaceManager_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	baseDir := t.TempDir()
	mgr := NewWorkspaceManager(baseDir, logger)

	done := make(chan bool, 10)

	// Create multiple workspaces concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			wsPath := filepath.Join(baseDir, "concurrent", "ws-"+string(rune('0'+id)))
			_ = mgr.Create(wsPath)
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 10; i++ {
		<-done
	}

	// Some workspaces should exist
	tracked := mgr.TrackedWorkspaces()
	assert.NotEmpty(t, tracked)
}

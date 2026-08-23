//go:build unix

package cas

import "syscall"

// makeFIFO creates a named pipe, which is one of the entry kinds a snapshot
// has nothing useful to say about.
func makeFIFO(path string) error {
	return syscall.Mkfifo(path, 0o600)
}

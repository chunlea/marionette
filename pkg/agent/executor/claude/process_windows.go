//go:build windows

package claude

import (
	"os"
	"os/exec"
)

// configureProcessGroup is a no-op on Windows, which has no process groups in
// the POSIX sense.
func configureProcessGroup(_ *exec.Cmd) {}

// killProcessTree kills the process. Child processes are not reaped; see
// configureProcessGroup.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

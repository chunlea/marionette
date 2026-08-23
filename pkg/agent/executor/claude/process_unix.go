//go:build !windows

package claude

import (
	"os"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the CLI in its own process group so that killing
// it kills everything it spawned.
//
// Claude Code runs tools as child processes. Signalling only the CLI leaves
// those children alive holding the pipes we are reading, so a timeout or a
// cancel would return while the work it was meant to stop kept running.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree kills the process and everything in its process group.
func killProcessTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	// A negative pid targets the whole group. Setpgid above makes the child a
	// group leader, so its pid is also its pgid.
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		// The group may already be gone; fall back to the process itself.
		return p.Kill()
	}
	return nil
}

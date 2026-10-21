//go:build !unix

package local

import (
	"os/exec"
	"syscall"
)

// newSysProcAttr returns the default process attributes on non-unix platforms.
func newSysProcAttr() *syscall.SysProcAttr {
	return nil
}

// killCommand kills only the direct child process.
//
// TODO(sandbox): on windows, terminate the whole process tree (job objects or CreateProcess group flags) so
// that grandchildren spawned by the shell do not survive and keep the output pipes open until WaitDelay
// expires.
func killCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	return cmd.Process.Kill()
}

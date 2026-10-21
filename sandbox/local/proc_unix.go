//go:build unix

package local

import (
	"os/exec"
	"syscall"
)

// newSysProcAttr puts the child into its own process group, so that the whole group can be signalled on
// cancellation. Shells commonly fork children that outlive them (ex: dash does not exec-optimize "sh -c 'sleep
// 30'"), and a survivor keeps the output pipes open until WaitDelay expires.
func newSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killCommand kills the command's entire process group. The group is assigned by the kernel before Start
// returns, so signalling the negative pid is race free; it falls back to killing the direct child when the
// group signal fails.
func killCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}

	return cmd.Process.Kill()
}

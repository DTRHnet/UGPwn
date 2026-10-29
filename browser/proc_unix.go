//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the worker in its own process group so cancelling
// the job tears down the browser children with it.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// terminateProcessGroup asks the whole group to stop. The worker installs
// SIGTERM/SIGINT handlers that emit a canceled event and close the browser, so a
// graceful signal is preferred; os/exec force-kills whatever remains once
// cmd.WaitDelay expires.
func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		// The group may already be gone; signal the process directly.
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

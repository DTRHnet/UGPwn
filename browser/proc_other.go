//go:build !unix

package main

import "os/exec"

// configureProcessGroup is a no-op where process groups are unavailable.
func configureProcessGroup(cmd *exec.Cmd) {}

// terminateProcessGroup asks the worker to stop. Without process groups the
// browser children cannot be signalled as a unit, so this is a hard kill.
func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

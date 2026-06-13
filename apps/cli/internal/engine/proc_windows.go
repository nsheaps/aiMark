//go:build windows

package engine

import "os/exec"

// setProcAttr is a no-op on Windows; llama-server has no child processes we
// depend on killing.
func setProcAttr(_ *exec.Cmd) {}

// killProcessGroup kills the server process.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

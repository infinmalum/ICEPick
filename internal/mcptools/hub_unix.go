//go:build unix

package mcptools

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the MCP server into its own process group so closing
// the session can reap the whole tree, not just the direct child.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup terminates the server's whole process group. Best effort:
// session transport cleanup remains responsible for the direct child.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

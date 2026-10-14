//go:build unix

package mcptools

import (
	"os/exec"
	"syscall"
)

func killExecutionTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
func cleanupExecutionTree(cmd *exec.Cmd) { _ = killExecutionTree(cmd) }

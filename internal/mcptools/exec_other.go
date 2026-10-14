//go:build !unix

package mcptools

import "os/exec"

func killExecutionTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
func cleanupExecutionTree(cmd *exec.Cmd) {}

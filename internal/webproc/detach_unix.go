//go:build !windows

package webproc

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session, so it survives the parent and is
// not in the parent's process group - a Ctrl-C in the terminal that runs the
// agent must not take the dashboard with it. That is the whole point of the
// split: the board outlives the MCP server.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

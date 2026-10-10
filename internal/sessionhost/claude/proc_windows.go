//go:build windows

package claude

import (
	"os/exec"
	"strconv"
)

// configureProcess is a no-op on Windows: the child is killed via taskkill,
// which understands process trees (design §4).
func configureProcess(cmd *exec.Cmd) {}

// killTree force-kills the process tree rooted at the child (the npm shim
// spawns node children that survive a plain process kill).
func killTree(cmd *exec.Cmd) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	return kill.Run()
}

//go:build windows

package webproc

import (
	"os/exec"
	"syscall"
)

// detach gives the child its own process group, which is as close as Windows
// comes to setsid: a Ctrl-C delivered to the parent's console group does not
// reach it. CREATE_NEW_PROCESS_GROUP is in the standard library's syscall
// package, so this needs no extra dependency.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

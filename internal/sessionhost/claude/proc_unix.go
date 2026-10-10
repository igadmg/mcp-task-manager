//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
	"time"
)

// configureProcess puts the child in its own process group so Stop can kill
// the whole tree without taking the host down with it (design §4).
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree signals the process group, escalating SIGTERM -> SIGKILL. The
// actual reaping happens in the reader's cmd.Wait, so killTree only probes
// for liveness with signal 0.
func killTree(cmd *exec.Cmd) error {
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if syscall.Kill(-pgid, 0) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}

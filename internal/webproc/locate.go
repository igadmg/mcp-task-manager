package webproc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// BinaryName is the dashboard's executable, the second artefact of this
// repository (cmd/mcp-task-manager-web).
const BinaryName = "mcp-task-manager-web"

// EnvBinary overrides where the dashboard binary is. It exists for a
// development tree and for an install that puts the two binaries in different
// places; without it the sibling lookup below covers the normal case.
const EnvBinary = "MCP_WEB_BINARY"

// Locate finds the dashboard binary: the env override, then next to this
// executable, then PATH. First hit wins.
//
// The sibling lookup is the normal case - `go install ./cmd/...` puts both
// binaries in the same GOBIN - and it is tried before PATH so a freshly built
// pair is not shadowed by an older installed one. PATH is the fallback for a
// packaged install where the running binary is a symlink somewhere else.
//
// Both the spawner and `serve web` go through this, so the two can never
// disagree about which dashboard they mean.
func Locate() (string, error) {
	var tried []string

	if override := os.Getenv(EnvBinary); override != "" {
		if usable(override) {
			return override, nil
		}
		tried = append(tried, fmt.Sprintf("%s=%s", EnvBinary, override))
	}

	if self, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(self), exeName())
		if usable(sibling) {
			return sibling, nil
		}
		tried = append(tried, sibling)
	}

	if path, err := exec.LookPath(exeName()); err == nil {
		return path, nil
	}
	tried = append(tried, "PATH")

	return "", fmt.Errorf("%s not found (looked in: %v); install it with "+
		"`go install github.com/gpayer/mcp-task-manager/cmd/...` or set %s",
		exeName(), tried, EnvBinary)
}

// usable reports whether path is an existing regular file. It deliberately
// does not check the executable bit: on Windows there is none, and a file
// that is there but not runnable fails loudly on exec, which is a better
// error than "not found".
func usable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return BinaryName + ".exe"
	}
	return BinaryName
}

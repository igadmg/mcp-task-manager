package claude

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
	"github.com/gpayer/mcp-task-manager/internal/sessionhost"
)

// Backend launches claude in stream-json mode (design §4). It is the only v1
// implementation of sessionhost.Backend.
type Backend struct {
	path string
	// command overrides exec.CommandContext; tests re-exec the test binary
	// as a fake claude via this hook.
	command func(ctx context.Context, path string, args []string) *exec.Cmd
	// sendPrompt delivers the first prompt; tests override it to exercise
	// the early-send-failure cleanup path (same pattern as command).
	sendPrompt func(p *proc, prompt string) error
}

// New returns a Backend. An empty path resolves claude via LookPath at
// Start time.
func New(path string) *Backend {
	return &Backend{path: path}
}

// Start implements sessionhost.Backend.
func (b *Backend) Start(ctx context.Context, spec sessionapi.Spec) (sessionhost.Process, error) {
	return b.start(ctx, spec, nil)
}

// StartRaw is like Start but also copies every raw stdout line into raw
// (the host's raw.jsonl); it implements the host's optional rawStarter hook.
func (b *Backend) StartRaw(ctx context.Context, spec sessionapi.Spec, raw io.WriteCloser) (sessionhost.Process, error) {
	return b.start(ctx, spec, raw)
}

// start resolves the binary, spawns the process, writes the first prompt,
// and starts the stdout reader goroutine.
func (b *Backend) start(ctx context.Context, spec sessionapi.Spec, raw io.WriteCloser) (sessionhost.Process, error) {
	resolved := b.path
	if resolved == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			if raw != nil {
				raw.Close()
			}
			return nil, &sessionapi.Error{Code: sessionapi.ErrCodeHostUnavailable,
				Message: "claude executable not found in PATH (use --claude-path)"}
		}
		resolved = p
	}

	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-prompt-tool", "stdio",
		"--session-id", newUUID(),
	}
	argv0, argv := buildArgv(resolved, args)

	var cmd *exec.Cmd
	if b.command != nil {
		cmd = b.command(ctx, argv0, argv)
	} else {
		cmd = exec.CommandContext(ctx, argv0, argv...)
	}
	cmd.Dir = spec.Workspace.Path
	if spec.TaskID != "" {
		// Preserve an env prepared by a command hook (fake claude tests).
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}
		cmd.Env = append(cmd.Env, "MCP_TASK_ID="+spec.TaskID)
	}
	configureProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		if raw != nil {
			raw.Close()
		}
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if raw != nil {
			raw.Close()
		}
		return nil, err
	}

	p := newProc(cmd, stdin, raw)
	cmd.Stderr = p.stderrTail
	if err := cmd.Start(); err != nil {
		if raw != nil {
			raw.Close()
		}
		return nil, &sessionapi.Error{Code: sessionapi.ErrCodeHostUnavailable,
			Message: "cannot start claude: " + err.Error()}
	}

	// The read loop starts before the first prompt: it owns reaping and the
	// raw log close, so a failed Send can still shut the half-started
	// process down cleanly through Stop (review finding 4).
	go p.readLoop(stdout)
	send := b.sendPrompt
	if send == nil {
		send = (*proc).Send
	}
	if err := send(p, spec.Prompt); err != nil {
		_ = p.Stop(context.Background())
		return nil, &sessionapi.Error{Code: sessionapi.ErrCodeHostUnavailable,
			Message: "cannot send prompt to claude: " + err.Error()}
	}
	return p, nil
}

// buildArgv wraps .cmd/.bat shims (npm on Windows) in cmd /c, which Go
// cannot exec directly (research §1).
func buildArgv(path string, args []string) (string, []string) {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".cmd") || strings.HasSuffix(lower, ".bat") {
		full := append([]string{"/c", path}, args...)
		return "cmd", full
	}
	return path, args
}

// newUUID returns a random RFC 4122 version 4 UUID. claude requires a UUID
// for --session-id; the host's own session id keeps its format (S3 note).
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}

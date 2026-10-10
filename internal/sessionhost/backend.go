package sessionhost

import (
	"context"
	"io"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// rawStarter is an optional extension of Backend: backends that want the
// per-session raw protocol log (raw.jsonl) receive it at start time. The
// Backend interface itself stays provider-neutral.
type rawStarter interface {
	StartRaw(ctx context.Context, spec sessionapi.Spec, raw io.WriteCloser) (Process, error)
}

// startProcess passes the raw log to backends implementing rawStarter and
// closes it otherwise; on failure the backend already released it.
func startProcess(ctx context.Context, b Backend, spec sessionapi.Spec, raw io.WriteCloser) (Process, error) {
	if rb, ok := b.(rawStarter); ok {
		return rb.StartRaw(ctx, spec, raw)
	}
	raw.Close()
	return b.Start(ctx, spec)
}

// Backend launches a provider process for a spec. The only v1 implementation
// lives in sessionhost/claude; the manager depends on this interface only.
type Backend interface {
	// Start begins the process. The prompt is already inside the spec.
	Start(ctx context.Context, spec sessionapi.Spec) (Process, error)
}

// Process is one running session's handle to its provider process.
// Events carries normalized events without seq/ts; the manager's journal
// assigns them.
type Process interface {
	// Events returns the stream of normalized events; the channel is closed
	// when the process ends.
	Events() <-chan sessionapi.Event
	// Send delivers a user follow-up message into the process.
	Send(text string) error
	// Answer responds to a question/permission request.
	Answer(a sessionapi.Answer) error
	// Stop asks the process to shut down gracefully, then kills it.
	Stop(ctx context.Context) error
	// Wait blocks until the process is gone and reports its exit error.
	Wait() error
}

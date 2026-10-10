package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const (
	// maxLineBytes guards the reader against pathological lines; anything
	// bigger is an error and stops the session (design §4).
	maxLineBytes    = 8 << 20
	stderrTailBytes = 16 << 10
	stopGrace       = 3 * time.Second
	eventsBuffer    = 64
)

// proc is one running claude process. A single reader goroutine owns stdout,
// the raw log and the events channel; a mutex serializes stdin writes.
type proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	raw    io.WriteCloser
	events chan sessionapi.Event

	// eventsMu serializes sends with the channel close: Answer runs on
	// caller goroutines while readLoop owns the close (review finding 2).
	eventsMu sync.Mutex
	closed   bool // set by closeEvents; emit refuses sends afterwards

	mu      sync.Mutex
	stdinOK bool

	pendingMu sync.Mutex
	pending   map[string]json.RawMessage // request_id -> original request input

	stderrTail *tailBuffer

	done     chan struct{}
	waitErr  error
	waitOnce sync.Once
}

func newProc(cmd *exec.Cmd, stdin io.WriteCloser, raw io.WriteCloser) *proc {
	return &proc{
		cmd:        cmd,
		stdin:      stdin,
		raw:        raw,
		events:     make(chan sessionapi.Event, eventsBuffer),
		stdinOK:    true,
		pending:    make(map[string]json.RawMessage),
		stderrTail: newTailBuffer(stderrTailBytes),
		done:       make(chan struct{}),
	}
}

// readLoop consumes stdout until EOF, then reaps the process. It is the only
// writer to raw and the closer of events and done; Answer emits on caller
// goroutines through emit, which cannot race the close.
func (p *proc) readLoop(stdout io.Reader) {
	defer p.closeEvents() // runs after done closes, so emitters observe done first
	defer close(p.done)
	if p.raw != nil {
		defer p.raw.Close()
	}
	reader := bufio.NewReaderSize(stdout, 64<<10)
	for {
		line, err := readLine(reader)
		if err == errLineTooLarge {
			p.emit(sessionapi.Event{Kind: sessionapi.EventError,
				Code: "line_too_large", Message: fmt.Sprintf("claude output line exceeds %d bytes", maxLineBytes)})
			_ = killTree(p.cmd)
			break
		}
		if len(line) > 0 {
			p.handleLine(line)
		}
		if err != nil {
			break
		}
	}
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		if err != nil && p.stderrTail.Len() > 0 {
			err = fmt.Errorf("%w (stderr: %s)", err, p.stderrTail.String())
		}
		p.waitErr = err
	})
}

// emit delivers an event without racing the channel close. Events produced
// after the process ended (e.g. a resolved answer racing exit) are dropped:
// once the channel is closed the select's send case panics when chosen, and
// the done case being ready does not protect it (review finding 1).
func (p *proc) emit(ev sessionapi.Event) {
	p.eventsMu.Lock()
	defer p.eventsMu.Unlock()
	if p.closed {
		return
	}
	select {
	case p.events <- ev:
	case <-p.done:
	}
}

// closeEvents shuts the events channel; it must be the only close call.
func (p *proc) closeEvents() {
	p.eventsMu.Lock()
	defer p.eventsMu.Unlock()
	p.closed = true
	close(p.events)
}

// errLineTooLarge marks a line that hit the size cap before its newline.
var errLineTooLarge = errors.New("claude output line exceeds the size limit")

// maxReadBytes is the content limit plus room for its single terminator.
const maxReadBytes = maxLineBytes + 1

// readLine reads one newline-terminated line with bounded memory: it gives
// up as soon as the line outgrows the limit instead of accumulating until
// the newline arrives (review finding 3).
func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		frag, err := reader.ReadSlice('\n')
		line = append(line, frag...)
		if len(line) > maxReadBytes || (len(line) == maxReadBytes && line[len(line)-1] != '\n') {
			return line, errLineTooLarge
		}
		if err == nil {
			return line, nil
		}
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

// handleLine writes the raw copy and converts the line into normalized
// events; an oversized or unparsable line stops the session with an error.
func (p *proc) handleLine(line []byte) {
	if p.raw != nil {
		if _, err := p.raw.Write(line); err != nil {
			fmt.Fprintf(os.Stderr, "claude backend: raw log write: %v\n", err)
		}
	}
	trimmed := bytes.TrimRight(line, "\r\n")
	if len(trimmed) > maxLineBytes {
		p.emit(sessionapi.Event{Kind: sessionapi.EventError,
			Code: "line_too_large", Message: fmt.Sprintf("claude output line exceeds %d bytes", maxLineBytes)})
		_ = killTree(p.cmd)
		return
	}
	evs, err := parseLine(trimmed)
	if err != nil {
		p.emit(sessionapi.Event{Kind: sessionapi.EventError,
			Code: "parse_error", Message: "cannot parse claude output: " + err.Error()})
		return
	}
	for _, ev := range evs {
		switch ev.Kind {
		case sessionapi.EventQuestion, sessionapi.EventPermission:
			p.pendingMu.Lock()
			p.pending[ev.RequestID] = ev.Input
			p.pendingMu.Unlock()
		}
		p.emit(ev)
	}
}

func (p *proc) Events() <-chan sessionapi.Event { return p.events }

// writeLine serializes one JSON message into stdin.
func (p *proc) writeLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stdinOK {
		return fmt.Errorf("claude process is not accepting input")
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

// Send delivers a user follow-up message (research §1 input format).
func (p *proc) Send(text string) error {
	return p.writeLine(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	})
}

// Answer replies to a control_request. The first answer wins: pending is
// removed under the lock so concurrent senders resolve the request at most
// once, and the manager's waiting_answer state is released by the
// request_resolved event emitted here (review finding 2). Any failure before
// the control_response reaches claude restores the pending entry so the
// answer stays retriable instead of stranding the UI in waiting_answer
// (review finding 3).
func (p *proc) Answer(a sessionapi.Answer) error {
	p.pendingMu.Lock()
	input, ok := p.pending[a.RequestID]
	if ok {
		delete(p.pending, a.RequestID)
	}
	p.pendingMu.Unlock()
	if !ok {
		return &sessionapi.Error{Code: sessionapi.ErrCodeNotFound,
			Message: "no pending request " + a.RequestID}
	}
	sent := false
	defer func() {
		if sent {
			return
		}
		// The control_response never reached claude (or could not even be
		// built), so the request is still pending there: put it back
		// (unless a concurrent answer already re-registered it).
		p.pendingMu.Lock()
		if _, taken := p.pending[a.RequestID]; !taken {
			p.pending[a.RequestID] = input
		}
		p.pendingMu.Unlock()
	}()
	inner := map[string]any{"behavior": a.Behavior}
	if a.Behavior == sessionapi.BehaviorDeny {
		if a.Message != "" {
			inner["message"] = a.Message
		}
	} else if a.Answers != nil {
		merged := map[string]any{}
		_ = json.Unmarshal(input, &merged)
		merged["answers"] = a.Answers
		inner["updatedInput"] = merged
	} else {
		var raw any
		if err := json.Unmarshal(input, &raw); err != nil {
			return fmt.Errorf("cannot reuse request input: %w", err)
		}
		inner["updatedInput"] = raw
	}
	if err := p.writeLine(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": a.RequestID,
			"response":   inner,
		},
	}); err != nil {
		return err
	}
	sent = true
	outcome := "answered"
	if a.Behavior == sessionapi.BehaviorDeny {
		outcome = "denied"
	}
	p.emit(sessionapi.Event{Kind: sessionapi.EventRequestResolved, RequestID: a.RequestID, Outcome: outcome})
	return nil
}

// Stop closes stdin, gives the process a grace period to exit, then kills
// the whole process tree (design §4).
func (p *proc) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.stdinOK {
		p.stdinOK = false
		_ = p.stdin.Close()
	}
	p.mu.Unlock()
	select {
	case <-p.done:
		return nil
	case <-time.After(stopGrace):
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := killTree(p.cmd); err != nil {
		return err
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Wait blocks until the process is reaped and reports its exit error, with
// the collected stderr tail attached (design §4).
func (p *proc) Wait() error {
	<-p.done
	return p.waitErr
}

// tailBuffer keeps the last n bytes written to it (the claude process's
// stderr, attached to errors on failure).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(n int) *tailBuffer { return &tailBuffer{max: n} }

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(b), nil
}

func (t *tailBuffer) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.buf)
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

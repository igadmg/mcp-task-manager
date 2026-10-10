package sessionhost

import (
	"context"
	"sync"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// fakeProcess is an in-memory Process for manager tests.
type fakeProcess struct {
	eventsCh  chan sessionapi.Event
	sent      chan string
	answers   chan sessionapi.Answer
	stopCalls chan struct{}
	waitErr   error

	stopOnce sync.Once
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{
		eventsCh:  make(chan sessionapi.Event, 128),
		sent:      make(chan string, 16),
		answers:   make(chan sessionapi.Answer, 16),
		stopCalls: make(chan struct{}, 4),
	}
}

func (p *fakeProcess) Events() <-chan sessionapi.Event { return p.eventsCh }

func (p *fakeProcess) Send(text string) error {
	p.sent <- text
	return nil
}

func (p *fakeProcess) Answer(a sessionapi.Answer) error {
	p.answers <- a
	return nil
}

func (p *fakeProcess) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() { close(p.stopCalls) })
	return nil
}

func (p *fakeProcess) Wait() error { return p.waitErr }

// fakeBackend hands out fakeProcess instances.
type fakeBackend struct {
	started  chan *fakeProcess
	specs    chan sessionapi.Spec
	startErr error
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		started: make(chan *fakeProcess, 16),
		specs:   make(chan sessionapi.Spec, 16),
	}
}

func (b *fakeBackend) Start(ctx context.Context, spec sessionapi.Spec) (Process, error) {
	if b.startErr != nil {
		return nil, b.startErr
	}
	p := newFakeProcess()
	b.specs <- spec
	b.started <- p
	return p, nil
}

func testSpec(ws, prompt string) sessionapi.Spec {
	return sessionapi.Spec{
		Version:   sessionapi.SpecVersion,
		Provider:  sessionapi.ProviderClaude,
		Workspace: sessionapi.Workspace{Kind: sessionapi.WorkspaceKindWorkingDir, Path: ws},
		Prompt:    prompt,
	}
}

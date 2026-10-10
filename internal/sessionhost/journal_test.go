package sessionhost

import (
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

func TestJournal_AppendAssignsSeqAndTs(t *testing.T) {
	j := openJournal(t)
	defer j.Close()

	ev, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 1 || ev.Ts.IsZero() {
		t.Fatalf("first event: %+v", ev)
	}
	ev2, _ := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "b"})
	if ev2.Seq != 2 {
		t.Fatalf("seq not increasing: %d", ev2.Seq)
	}
}

func TestJournal_ReadAfter(t *testing.T) {
	j := openJournal(t)
	defer j.Close()
	for i := 0; i < 3; i++ {
		if _, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := j.ReadAfter(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Seq != 2 || events[1].Seq != 3 {
		t.Fatalf("ReadAfter(1): %+v", events)
	}
	events, err = j.ReadAfter(3)
	if err != nil || len(events) != 0 {
		t.Fatalf("ReadAfter(3): %v %+v", err, events)
	}
}

func TestJournal_ReopenContinuesSeq(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText}); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}

	j2, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer j2.Close()
	ev, err := j2.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText})
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 3 {
		t.Fatalf("seq after reopen: %d, want 3", ev.Seq)
	}
}

func TestJournal_SubscribeReplayThenLive(t *testing.T) {
	j := openJournal(t)
	defer j.Close()
	for i := 0; i < 2; i++ {
		if _, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "old"}); err != nil {
			t.Fatal(err)
		}
	}

	sub, err := j.Subscribe(0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	for want := int64(1); want <= 2; want++ {
		ev := recvEvent(t, sub)
		if ev.Seq != want {
			t.Fatalf("replay seq %d, want %d", ev.Seq, want)
		}
	}

	if _, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: "new"}); err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, sub)
	if ev.Seq != 3 || ev.Text != "new" {
		t.Fatalf("live event: %+v", ev)
	}
}

func TestJournal_SubscribeAfter(t *testing.T) {
	j := openJournal(t)
	defer j.Close()
	for i := 0; i < 3; i++ {
		if _, err := j.Append(sessionapi.Event{Kind: sessionapi.EventAssistantText}); err != nil {
			t.Fatal(err)
		}
	}
	sub, err := j.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	ev := recvEvent(t, sub)
	if ev.Seq != 3 {
		t.Fatalf("replay from after=2: %+v", ev)
	}
}

func openJournal(t *testing.T) *Journal {
	t.Helper()
	j, err := OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func recvEvent(t *testing.T, sub *Subscription) sessionapi.Event {
	t.Helper()
	select {
	case ev, ok := <-sub.C:
		if !ok {
			t.Fatal("subscription closed")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
		return sessionapi.Event{}
	}
}

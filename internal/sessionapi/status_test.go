package sessionapi

import "testing"

func TestStatus_IsTerminal(t *testing.T) {
	terminal := map[Status]bool{
		StatusStarting:      false,
		StatusRunning:       false,
		StatusWaitingAnswer: false,
		StatusIdle:          false,
		StatusFinished:      true,
		StatusFailed:        true,
		StatusStopped:       true,
	}
	for s, want := range terminal {
		if s.IsTerminal() != want {
			t.Fatalf("IsTerminal(%q) = %v, want %v", s, s.IsTerminal(), want)
		}
	}
}

func TestStatus_StringValues(t *testing.T) {
	want := map[Status]string{
		StatusStarting:      "starting",
		StatusRunning:       "running",
		StatusWaitingAnswer: "waiting_answer",
		StatusIdle:          "idle",
		StatusFinished:      "finished",
		StatusFailed:        "failed",
		StatusStopped:       "stopped",
	}
	for s, v := range want {
		if string(s) != v {
			t.Fatalf("status %q != %q", s, v)
		}
	}
}

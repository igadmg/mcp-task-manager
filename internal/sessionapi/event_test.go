package sessionapi

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_RoundTrip_JSON(t *testing.T) {
	cost := 0.0123
	dur := int64(1500)
	events := []Event{
		{Seq: 1, Ts: time.Date(2026, 10, 10, 12, 0, 0, 123456789, time.UTC), Kind: EventStatus, Status: StatusRunning},
		{Seq: 2, Ts: time.Now().UTC().Truncate(time.Millisecond), Kind: EventUserMessage, Text: "hello\n\"quoted\""},
		{Seq: 3, Kind: EventAssistantText, Text: "привет <script>"},
		{Seq: 4, Kind: EventToolUse, ToolUseID: "tu1", Name: "Bash", Summary: "run ls", Input: json.RawMessage(`{"cmd":"ls"}`)},
		{Seq: 5, Kind: EventToolResult, ToolUseID: "tu1", IsError: true, Text: "boom"},
		{Seq: 6, Kind: EventQuestion, RequestID: "r1", ToolUseID: "tu2", Questions: []Question{{
			Question:    "pick one",
			Header:      "Choice",
			MultiSelect: true,
			Options:     []Option{{Label: "a", Description: "first"}, {Label: "b"}},
		}}},
		{Seq: 7, Kind: EventPermission, RequestID: "r2", ToolUseID: "tu3", ToolName: "Edit", Description: "edit file", Input: json.RawMessage(`{}`)},
		{Seq: 8, Kind: EventRequestResolved, RequestID: "r1", Outcome: "answered"},
		{Seq: 9, Kind: EventTurnResult, OK: true, CostUSD: &cost, DurationMs: &dur},
		{Seq: 10, Kind: EventError, Code: "start_failed", Message: "claude not found"},
		{Seq: 11, Kind: EventStatus, Status: StatusFailed, Reason: "host_restarted"},
	}
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal %s: %v", ev.Kind, err)
		}
		var back Event
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal %s: %v", ev.Kind, err)
		}
		if back.Kind != ev.Kind || back.Seq != ev.Seq {
			t.Fatalf("round trip %s: %+v != %+v", ev.Kind, back, ev)
		}
	}
}

func TestEventKind_StringValues(t *testing.T) {
	want := map[EventKind]string{
		EventStatus:          "status",
		EventUserMessage:     "user_message",
		EventAssistantText:   "assistant_text",
		EventToolUse:         "tool_use",
		EventToolResult:      "tool_result",
		EventQuestion:        "question",
		EventPermission:      "permission",
		EventRequestResolved: "request_resolved",
		EventTurnResult:      "turn_result",
		EventError:           "error",
	}
	for k, v := range want {
		if string(k) != v {
			t.Fatalf("kind %q != %q", k, v)
		}
	}
}

func TestAnswerAndMessage_JSON(t *testing.T) {
	a := Answer{RequestID: "r1", Behavior: BehaviorAllow, Answers: map[string]string{"q": "a, b"}, Message: "why not"}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	var back Answer
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.RequestID != "r1" || back.Behavior != BehaviorAllow || back.Answers["q"] != "a, b" {
		t.Fatalf("answer round trip: %+v", back)
	}
	m := Message{Text: "follow-up"}
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"text":"follow-up"}` {
		t.Fatalf("message JSON: %s", data)
	}
}

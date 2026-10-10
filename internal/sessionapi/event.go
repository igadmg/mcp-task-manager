package sessionapi

import (
	"encoding/json"
	"time"
)

// EventKind identifies the normalized event type (design §3 table).
type EventKind string

const (
	EventStatus          EventKind = "status"
	EventUserMessage     EventKind = "user_message"
	EventAssistantText   EventKind = "assistant_text"
	EventToolUse         EventKind = "tool_use"
	EventToolResult      EventKind = "tool_result"
	EventQuestion        EventKind = "question"
	EventPermission      EventKind = "permission"
	EventRequestResolved EventKind = "request_resolved"
	EventTurnResult      EventKind = "turn_result"
	EventError           EventKind = "error"
)

// Event is the only shape the web side ever sees. Seq and Ts are assigned
// by the host journal; backends emit events with them zero.
type Event struct {
	Seq  int64     `json:"seq"`
	Ts   time.Time `json:"ts"`
	Kind EventKind `json:"kind"`

	Status  Status `json:"status,omitempty"`  // status
	Reason  string `json:"reason,omitempty"`  // status (failure reason)
	Text    string `json:"text,omitempty"`    // user_message, assistant_text, tool_result
	Code    string `json:"code,omitempty"`    // error
	Message string `json:"message,omitempty"` // error

	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_use, tool_result, question, permission
	Name      string          `json:"name,omitempty"`        // tool_use
	Summary   string          `json:"summary,omitempty"`     // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use, permission
	IsError   bool            `json:"is_error,omitempty"`    // tool_result

	RequestID   string     `json:"request_id,omitempty"`  // question, permission, request_resolved
	ToolName    string     `json:"tool_name,omitempty"`   // permission
	Description string     `json:"description,omitempty"` // permission
	Questions   []Question `json:"questions,omitempty"`   // question
	Outcome     string     `json:"outcome,omitempty"`     // request_resolved

	OK         bool     `json:"ok,omitempty"`          // turn_result
	CostUSD    *float64 `json:"cost_usd,omitempty"`    // turn_result
	DurationMs *int64   `json:"duration_ms,omitempty"` // turn_result
}

// Question is one AskUserQuestion entry inside a question event.
type Question struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	Options     []Option `json:"options,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
}

// Option is one selectable answer inside a Question.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Answer is the user's reply to a question/permission request (design §6).
type Answer struct {
	RequestID string            `json:"request_id"`
	Behavior  string            `json:"behavior"` // allow | deny
	Answers   map[string]string `json:"answers,omitempty"`
	Message   string            `json:"message,omitempty"`
}

const (
	BehaviorAllow = "allow"
	BehaviorDeny  = "deny"
)

// Message is a user follow-up message sent into a live session.
type Message struct {
	Text string `json:"text"`
}

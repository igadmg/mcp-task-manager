package claude

import (
	"bytes"
	"encoding/json"
	"unicode/utf8"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// toolResultMaxBytes caps tool_result text stored in normalized events
// (design §3); the full line still lands in raw.jsonl.
const toolResultMaxBytes = 8 << 10

// streamEvent is the subset of Claude's stream-json wire format we consume.
type streamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Request   *ctlRequest     `json:"request"`
	Message   json.RawMessage `json:"message"`

	TotalCostUSD *float64 `json:"total_cost_usd"`
	DurationMs   *int64   `json:"duration_ms"`
}

type ctlRequest struct {
	Subtype     string          `json:"subtype"`
	ToolName    string          `json:"tool_name"`
	Input       json.RawMessage `json:"input"`
	ToolUseID   string          `json:"tool_use_id"`
	Description string          `json:"description"`
}

// streamMessage is an assistant/user message body.
type streamMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type questionInput struct {
	Questions []struct {
		Question    string `json:"question"`
		Header      string `json:"header"`
		MultiSelect bool   `json:"multiSelect"`
		Options     []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
}

// parseLine maps one stream-json line to zero or more normalized events
// (design §3). thinking blocks, rate_limit_event, system and our own
// control_response acknowledgements never reach the normalized stream.
func parseLine(line []byte) ([]sessionapi.Event, error) {
	var se streamEvent
	dec := json.NewDecoder(bytes.NewReader(line))
	if err := dec.Decode(&se); err != nil {
		return nil, err
	}
	switch se.Type {
	case "system", "rate_limit_event", "control_response":
		return nil, nil
	case "assistant":
		return parseAssistant(se.Message)
	case "user":
		return parseUser(se.Message)
	case "control_request":
		return parseControlRequest(se.RequestID, se.Request)
	case "result":
		return []sessionapi.Event{{
			Kind:       sessionapi.EventTurnResult,
			OK:         se.Subtype == "success",
			CostUSD:    se.TotalCostUSD,
			DurationMs: se.DurationMs,
		}}, nil
	}
	return nil, nil
}

func parseAssistant(raw json.RawMessage) ([]sessionapi.Event, error) {
	var msg streamMessage
	if raw == nil || json.Unmarshal(raw, &msg) != nil {
		return nil, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		// Plain-string content is not expected on assistant messages.
		return nil, nil
	}
	var evs []sessionapi.Event
	for _, b := range blocks {
		switch b.Type {
		case "text":
			evs = append(evs, sessionapi.Event{Kind: sessionapi.EventAssistantText, Text: b.Text})
		case "tool_use":
			evs = append(evs, sessionapi.Event{
				Kind:      sessionapi.EventToolUse,
				ToolUseID: b.ID,
				Name:      b.Name,
				Input:     b.Input,
			})
		}
		// thinking and anything else are dropped on purpose.
	}
	return evs, nil
}

func parseUser(raw json.RawMessage) ([]sessionapi.Event, error) {
	var msg streamMessage
	if raw == nil || json.Unmarshal(raw, &msg) != nil {
		return nil, nil
	}
	var blocks []contentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		// Echoed plain user text is ignored: the host journals user_message
		// itself when Send delivers the message.
		return nil, nil
	}
	var evs []sessionapi.Event
	for _, b := range blocks {
		if b.Type != "tool_result" {
			continue
		}
		evs = append(evs, sessionapi.Event{
			Kind:      sessionapi.EventToolResult,
			ToolUseID: b.ToolUseID,
			IsError:   b.IsError,
			Text:      truncate(toolResultText(b.Content), toolResultMaxBytes),
		})
	}
	return evs, nil
}

// toolResultText flattens the tool_result content, which may be a plain
// string or a list of typed blocks.
func toolResultText(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var out string
	for _, b := range blocks {
		out += b.Text
	}
	return out
}

func parseControlRequest(requestID string, req *ctlRequest) ([]sessionapi.Event, error) {
	if req == nil || req.Subtype != "can_use_tool" {
		return nil, nil
	}
	if req.ToolName == "AskUserQuestion" {
		var in questionInput
		if err := json.Unmarshal(req.Input, &in); err != nil {
			return nil, err
		}
		qs := make([]sessionapi.Question, 0, len(in.Questions))
		for _, q := range in.Questions {
			opts := make([]sessionapi.Option, 0, len(q.Options))
			for _, o := range q.Options {
				opts = append(opts, sessionapi.Option{Label: o.Label, Description: o.Description})
			}
			qs = append(qs, sessionapi.Question{
				Question:    q.Question,
				Header:      q.Header,
				Options:     opts,
				MultiSelect: q.MultiSelect,
			})
		}
		return []sessionapi.Event{{
			Kind:      sessionapi.EventQuestion,
			RequestID: requestID,
			ToolUseID: req.ToolUseID,
			Questions: qs,
			Input:     req.Input,
		}}, nil
	}
	return []sessionapi.Event{{
		Kind:        sessionapi.EventPermission,
		RequestID:   requestID,
		ToolUseID:   req.ToolUseID,
		ToolName:    req.ToolName,
		Description: req.Description,
		Input:       req.Input,
	}}, nil
}

// truncate cuts s to at most n bytes without breaking a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

package sessionapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	SpecVersion             = 1
	ProviderClaude          = "claude"
	WorkspaceKindWorkingDir = "working_dir"
	MaxPromptBytes          = 64 * 1024
)

var taskIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Workspace selects where the session's process runs. New kinds extend
// the kind switch without changing the spec shape (design §2).
type Workspace struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Spec is the provider-neutral session request (design §2).
type Spec struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	Workspace Workspace `json:"workspace"`
	Prompt    string    `json:"prompt"`
	TaskID    string    `json:"task_id,omitempty"`
}

// ParseSpec decodes a spec, rejecting unknown fields so typos fail loudly.
func ParseSpec(data []byte) (Spec, error) {
	var spec Spec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, &Error{Code: ErrCodeInvalidSpec, Message: "invalid spec: " + err.Error()}
	}
	if err := ensureEOF(dec); err != nil {
		return Spec{}, &Error{Code: ErrCodeInvalidSpec, Message: "invalid spec: trailing data"}
	}
	return spec, nil
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	return fmt.Errorf("trailing data")
}

// Validate enforces the v1 rules from design §2.
func (s Spec) Validate() error {
	if s.Version != SpecVersion {
		return &Error{Code: ErrCodeInvalidSpec, Message: fmt.Sprintf("unsupported spec version %d (supported: %d)", s.Version, SpecVersion)}
	}
	if s.Provider != ProviderClaude {
		return &Error{Code: ErrCodeUnsupportedProvider, Message: fmt.Sprintf("unsupported provider %q (supported: %q)", s.Provider, ProviderClaude)}
	}
	if s.Workspace.Kind != WorkspaceKindWorkingDir {
		return NewUnsupportedWorkspaceKind(s.Workspace.Kind)
	}
	if !isAbs(s.Workspace.Path) {
		return &Error{Code: ErrCodeInvalidSpec, Message: "workspace.path must be an absolute path"}
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return &Error{Code: ErrCodeInvalidSpec, Message: "prompt must not be empty"}
	}
	if len(s.Prompt) > MaxPromptBytes {
		return &Error{Code: ErrCodeInvalidSpec, Message: fmt.Sprintf("prompt exceeds %d bytes", MaxPromptBytes)}
	}
	if s.TaskID != "" && !taskIDRe.MatchString(s.TaskID) {
		return &Error{Code: ErrCodeInvalidSpec, Message: "task_id must be kebab-case ([a-z0-9] separated by dashes)"}
	}
	return nil
}

// isAbs treats Windows drive and UNC paths as absolute too.
func isAbs(p string) bool {
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z') {
		return true
	}
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\\`)
}

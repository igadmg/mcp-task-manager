package sessionapi

import (
	"encoding/json"
	"errors"
	"testing"
)

func asError(err error, target **Error) bool {
	return errors.As(err, target)
}

func TestError_CodesAndJSON(t *testing.T) {
	codes := []string{
		ErrCodeInvalidSpec,
		ErrCodeUnsupportedProvider,
		ErrCodeUnsupportedWorkspaceKind,
		ErrCodeWorkspaceNotFound,
		ErrCodeTooManySessions,
		ErrCodeNotFound,
		ErrCodeConflict,
		ErrCodeHostUnavailable,
		ErrCodeUnauthorized,
	}
	for _, c := range codes {
		if c == "" {
			t.Fatal("empty code constant")
		}
	}
	e := &Error{Code: ErrCodeNotFound, Message: "no such session"}
	if e.Error() != "not_found: no such session" {
		t.Fatalf("unexpected Error(): %q", e.Error())
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["code"] != "not_found" || m["message"] != "no such session" {
		t.Fatalf("unexpected JSON: %s", data)
	}
}

func TestNewUnsupportedWorkspaceKind_ListsSupported(t *testing.T) {
	e := NewUnsupportedWorkspaceKind("git_clone")
	if e.Code != ErrCodeUnsupportedWorkspaceKind {
		t.Fatalf("code: %s", e.Code)
	}
	if !contains(e.Message, "git_clone") || !contains(e.Message, WorkspaceKindWorkingDir) {
		t.Fatalf("message must mention kind and supported kinds: %q", e.Message)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && index(s, sub) >= 0)
}

func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

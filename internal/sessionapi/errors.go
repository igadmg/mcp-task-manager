package sessionapi

import "strconv"

// Error is the single structured error shape used across the session API.
// HTTP layers serialize it as {"error":{"code","message"}}.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

const (
	ErrCodeInvalidSpec              = "invalid_spec"
	ErrCodeUnsupportedProvider      = "unsupported_provider"
	ErrCodeUnsupportedWorkspaceKind = "unsupported_workspace_kind"
	ErrCodeWorkspaceNotFound        = "workspace_not_found"
	ErrCodeTooManySessions          = "too_many_sessions"
	ErrCodeNotFound                 = "not_found"
	ErrCodeConflict                 = "conflict"
	ErrCodeHostUnavailable          = "host_unavailable"
	ErrCodeUnauthorized             = "unauthorized"
)

func NewUnsupportedWorkspaceKind(kind string) *Error {
	return &Error{
		Code:    ErrCodeUnsupportedWorkspaceKind,
		Message: "unsupported workspace kind " + strconv.Quote(kind) + " (supported: " + WorkspaceKindWorkingDir + ")",
	}
}

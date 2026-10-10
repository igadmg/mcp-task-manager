package sessionapi

// Status is the lifecycle state of a session (design §3).
type Status string

const (
	StatusStarting      Status = "starting"
	StatusRunning       Status = "running"
	StatusWaitingAnswer Status = "waiting_answer"
	StatusIdle          Status = "idle"
	StatusFinished      Status = "finished"
	StatusFailed        Status = "failed"
	StatusStopped       Status = "stopped"
)

// IsTerminal reports whether the process behind the session is gone for good.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusFinished, StatusFailed, StatusStopped:
		return true
	}
	return false
}

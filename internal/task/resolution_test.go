package task

import (
	"testing"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
)

func newResolutionService() *Service {
	svc := NewService(newMockStorage(), nil, nil, newMockIndex(), []string{"feature", "bug"}, nil)
	svc.Initialize()
	return svc
}

// A plain completion keeps its old shape and gains the default resolution.
func TestCompleteTask_DefaultsToCompleted(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Ship it", "", PriorityHigh, "feature", "", "")
	svc.StartTask(created.ID)

	done, err := svc.CompleteTask(created.ID)
	if err != nil {
		t.Fatalf("CompleteTask() error = %v", err)
	}
	if done.Status != StatusDone {
		t.Errorf("status = %q, want done", done.Status)
	}
	if done.Resolution != ResolutionCompleted {
		t.Errorf("resolution = %q, want completed", done.Resolution)
	}
	if done.ClosedAt == nil {
		t.Error("ClosedAt not stamped on close")
	}
}

// Completion still refuses a task nobody started - only the non-completed
// resolutions relax that rule.
func TestCompleteTask_CompletedStillRequiresInProgress(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Never started", "", PriorityMedium, "feature", "", "")

	if _, err := svc.CompleteTask(created.ID); err == nil {
		t.Fatal("CompleteTask() on a todo task should fail")
	}
}

// The case this whole field exists for: a task that stopped applying, closed
// straight from todo, without lying about the work having been done.
func TestCompleteTask_ObsoleteFromTodo(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Research the old renderer", "", PriorityMedium, "feature", "", "")

	closed, err := svc.CompleteTask(created.ID,
		WithResolution(ResolutionObsolete),
		WithResolutionNote("renderer replaced in f7fe038"))
	if err != nil {
		t.Fatalf("CompleteTask(obsolete) error = %v", err)
	}
	if closed.Status != StatusDone {
		t.Errorf("status = %q, want done", closed.Status)
	}
	if closed.Resolution != ResolutionObsolete {
		t.Errorf("resolution = %q, want obsolete", closed.Resolution)
	}
	if closed.ResolutionNote != "renderer replaced in f7fe038" {
		t.Errorf("note = %q, want the note that was passed", closed.ResolutionNote)
	}
	if closed.EffectiveResolution().Delivered() {
		t.Error("an obsolete task must not read as delivered work")
	}
}

func TestCompleteTask_RejectsAlreadyClosed(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Done once", "", PriorityMedium, "feature", "", "")
	svc.StartTask(created.ID)
	svc.CompleteTask(created.ID)

	if _, err := svc.CompleteTask(created.ID, WithResolution(ResolutionObsolete)); err == nil {
		t.Fatal("closing an already closed task should fail")
	}
}

func TestCompleteTask_RejectsUnknownResolution(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Bad input", "", PriorityMedium, "feature", "", "")
	svc.StartTask(created.ID)

	if _, err := svc.CompleteTask(created.ID, WithResolution("stale")); err == nil {
		t.Fatal("an unknown resolution should be rejected")
	}
}

// Completing a parent with open subtasks still fails; closing one as obsolete
// takes the branch with it, because a branch that no longer applies does not
// apply subtask by subtask either.
func TestCompleteTask_SubtaskRules(t *testing.T) {
	t.Run("completed refuses open subtasks", func(t *testing.T) {
		svc := newResolutionService()

		parent, _ := svc.Create("Parent", "", PriorityHigh, "feature", "", "")
		svc.CreateSubtask("Child", "", PriorityHigh, "feature", parent.ID)
		svc.StartTask(parent.ID)

		if _, err := svc.CompleteTask(parent.ID); err == nil {
			t.Fatal("completing a parent with an open subtask should fail")
		}
	})

	t.Run("obsolete cascades to open subtasks", func(t *testing.T) {
		svc := newResolutionService()

		parent, _ := svc.Create("Parent", "", PriorityHigh, "feature", "", "")
		child, _ := svc.CreateSubtask("Child", "", PriorityHigh, "feature", parent.ID)

		if _, err := svc.CompleteTask(parent.ID, WithResolution(ResolutionObsolete)); err != nil {
			t.Fatalf("CompleteTask(obsolete) error = %v", err)
		}

		closedChild, err := svc.Get(child.ID)
		if err != nil {
			t.Fatalf("Get(child) error = %v", err)
		}
		if closedChild.Status != StatusDone {
			t.Errorf("child status = %q, want done", closedChild.Status)
		}
		if closedChild.Resolution != ResolutionObsolete {
			t.Errorf("child resolution = %q, want obsolete", closedChild.Resolution)
		}
		if closedChild.ResolutionNote == "" {
			t.Error("a cascaded close should record which parent pulled it along")
		}
	})
}

// Naming a resolution through update_task closes the task in one call.
func TestUpdate_ResolutionImpliesClosing(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Stale idea", "", PriorityLow, "feature", "", "")

	updated, err := svc.Update(created.ID, nil, nil, nil, nil, nil, WithResolution(ResolutionWontfix))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Status != StatusDone {
		t.Errorf("status = %q, want done", updated.Status)
	}
	if updated.Resolution != ResolutionWontfix {
		t.Errorf("resolution = %q, want wontfix", updated.Resolution)
	}
}

func TestUpdate_ResolutionConflictsWithReopening(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Conflicted", "", PriorityLow, "feature", "", "")

	inProgress := StatusInProgress
	if _, err := svc.Update(created.ID, nil, nil, &inProgress, nil, nil, WithResolution(ResolutionObsolete)); err == nil {
		t.Fatal("a resolution together with a move to in_progress should be rejected")
	}
}

// Reopening must not leave the old closure behind, or the task would carry a
// resolution describing a closure that no longer holds.
func TestUpdate_ReopenClearsResolution(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Back from the dead", "", PriorityMedium, "feature", "", "")
	svc.Update(created.ID, nil, nil, nil, nil, nil, WithResolution(ResolutionObsolete), WithResolutionNote("was obsolete"))

	todo := StatusTodo
	reopened, err := svc.Update(created.ID, nil, nil, &todo, nil, nil)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if reopened.Resolution != "" || reopened.ResolutionNote != "" || reopened.ClosedAt != nil {
		t.Errorf("reopened task still carries a closure: %q / %q / %v",
			reopened.Resolution, reopened.ResolutionNote, reopened.ClosedAt)
	}
	if reopened.EffectiveResolution() != "" {
		t.Errorf("EffectiveResolution() = %q on an open task, want empty", reopened.EffectiveResolution())
	}
}

func TestUpdate_ResolutionNoteNeedsAClosedTask(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Open", "", PriorityMedium, "feature", "", "")

	if _, err := svc.Update(created.ID, nil, nil, nil, nil, nil, WithResolutionNote("why?")); err == nil {
		t.Fatal("a resolution note on an open task should be rejected")
	}
}

// verified_at is about the task's text, not its progress: stamping it leaves
// status and resolution alone, and it survives on an open task.
func TestUpdate_VerifiedAt(t *testing.T) {
	svc := newResolutionService()

	created, _ := svc.Create("Still relevant", "", PriorityMedium, "feature", "", "")
	if created.VerifiedAt != nil {
		t.Fatal("a fresh task should not claim to have been verified")
	}

	verified, err := svc.Update(created.ID, nil, nil, nil, nil, nil, WithVerified(true))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if verified.VerifiedAt == nil {
		t.Fatal("VerifiedAt not stamped")
	}
	if verified.Status != StatusTodo {
		t.Errorf("status = %q, want todo: verifying is not progress", verified.Status)
	}

	cleared, err := svc.Update(created.ID, nil, nil, nil, nil, nil, WithVerified(false))
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if cleared.VerifiedAt != nil {
		t.Error("VerifiedAt not cleared")
	}
}

// A task closed before this field existed reads as completed, so old backlogs
// do not all become "unknown".
func TestEffectiveResolution_LegacyDoneTask(t *testing.T) {
	legacy := &Task{Status: StatusDone}
	if got := legacy.EffectiveResolution(); got != ResolutionCompleted {
		t.Errorf("EffectiveResolution() = %q, want completed", got)
	}
	if !legacy.EffectiveResolution().Delivered() {
		t.Error("a legacy done task should read as delivered")
	}
}

// A task closed without its work being done has nothing left to review, so it
// skips the grace period completed tasks serve out.
func TestAutoArchive_ClosedWithoutWorkArchivesImmediately(t *testing.T) {
	storage := newMockStorage()
	cfg := &config.Config{AutoArchive: config.AutoArchiveConfig{Enabled: true, AfterDays: 30}}
	svc := NewService(storage, newMockArchiveStorage(storage), nil, newMockIndex(), []string{"feature", "bug"}, cfg)
	svc.Initialize()

	fresh, _ := svc.Create("Just completed", "", PriorityMedium, "feature", "", "")
	svc.StartTask(fresh.ID)
	svc.CompleteTask(fresh.ID)

	obsolete, _ := svc.Create("Never applied", "", PriorityMedium, "feature", "", "")
	svc.CompleteTask(obsolete.ID, WithResolution(ResolutionObsolete))

	candidates := svc.GetAutoArchiveCandidates()
	if len(candidates) != 1 || candidates[0].ID != obsolete.ID {
		t.Fatalf("candidates = %v, want only the obsolete task %s", ids(candidates), obsolete.ID)
	}
	if time.Since(candidates[0].UpdatedAt) > time.Hour {
		t.Error("the obsolete task was picked up by the age rule, not the resolution rule")
	}
}

func ids(tasks []*Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

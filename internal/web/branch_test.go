package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/storage"
	"github.com/gpayer/mcp-task-manager/internal/task"
)

// dir is the tasks directory behind a test service.
func dir(t *testing.T, svc *task.Service) string {
	t.Helper()
	return svc.Config().TasksDir()
}

// setBranch writes branch fields into a task record the way the git flows
// persist them; the board only reads records, so no git is needed here.
func setBranch(t *testing.T, tasksDir, id, branch, final string) {
	t.Helper()
	st := storage.NewMarkdownStorage(tasksDir)
	rec, err := st.Load(id)
	if err != nil {
		t.Fatalf("Load(%s) error = %v", id, err)
	}
	rec.Branch, rec.FinalBranch = branch, final
	rec.BaseBranch = "main_patched"
	rec.StartCommit = "0123456789abcdef0123456789abcdef01234567"
	if final != "" {
		rec.SquashCommit = "fedcba9876543210fedcba9876543210fedcba98"
	}
	if err := st.Save(rec); err != nil {
		t.Fatalf("Save(%s) error = %v", id, err)
	}
}

func TestCardBranchPrefersFinal(t *testing.T) {
	for _, tc := range []struct {
		branch, final, want string
	}{
		{"", "", ""},
		{"dev/wip/x", "", "dev/wip/x"},
		{"dev/wip/x", "dev/x", "dev/x"},
	} {
		tk := &task.Task{ID: "1", Branch: tc.branch, FinalBranch: tc.final}
		card := newCardView(tk, &task.BoardSnapshot{}, fixedNow)
		if card.Branch != tc.want {
			t.Errorf("card branch for (%q, %q) = %q, want %q", tc.branch, tc.final, card.Branch, tc.want)
		}
	}
}

func TestDetailViewGitFields(t *testing.T) {
	start := "0123456789abcdef0123456789abcdef01234567"
	squash := "fedcba9876543210fedcba9876543210fedcba98"
	d := &task.TaskDetail{Task: &task.Task{ID: "1", Branch: "dev/wip/x", FinalBranch: "dev/x",
		BaseBranch: "main_patched", StartCommit: start, SquashCommit: squash}}

	v := newDetailView(d, nil, nil, fixedNow)
	if v.Branch != "dev/wip/x" || v.FinalBranch != "dev/x" || v.BaseBranch != "main_patched" || v.Card.Branch != "dev/x" {
		t.Errorf("branches = (%q, %q, %q, card %q)", v.Branch, v.FinalBranch, v.BaseBranch, v.Card.Branch)
	}
	if v.StartCommit != start || v.StartCommitShort != start[:12] || v.SquashCommit != squash || v.SquashCommitShort != squash[:12] {
		t.Errorf("commits = (%q, %q, %q, %q)", v.StartCommit, v.StartCommitShort, v.SquashCommit, v.SquashCommitShort)
	}
}

func TestBoardShowsBranchChip(t *testing.T) {
	h, svc, tasksDir := newTestHandler(t)
	seedBoard(t, svc)
	setBranch(t, tasksDir, "3", "dev/wip/3-fix-the-index", "")
	setBranch(t, tasksDir, "4", "dev/wip/4-old-chore", "dev/4-old-chore")

	board := get(t, h, "/board").Body.String()
	for _, want := range []string{
		`data-copy="dev/wip/3-fix-the-index"`,
		`aria-label="Copy branch name"`,
	} {
		if !strings.Contains(board, want) {
			t.Errorf("board lacks %s", want)
		}
	}
	// Done renders statistics, not cards: a delivered task's branch is
	// shown by its detail view only.
	if strings.Contains(board, "4-old-chore") {
		t.Error("the board names done task 4's branch")
	}
	if n := strings.Count(board, "copy-btn"); n != 1 {
		t.Errorf("board has %d copy buttons, want 1 (only branched open tasks)", n)
	}

	detail := get(t, h, "/tasks/4").Body.String()
	for _, want := range []string{
		">Git<",
		`data-copy="dev/4-old-chore"`,
		`data-copy="dev/wip/4-old-chore"`,
		`title="0123456789abcdef0123456789abcdef01234567">0123456789ab<`,
		`title="fedcba9876543210fedcba9876543210fedcba98">fedcba987654<`,
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail lacks %s", want)
		}
	}
	if plain := get(t, h, "/tasks/1").Body.String(); strings.Contains(plain, ">Git<") {
		t.Error("a task without branch data shows a Git block")
	}
}

// TestCopyButtonHasNoHtmxAttributes keeps the copy control client-side: it
// must never be wired to a request.
func TestCopyButtonHasNoHtmxAttributes(t *testing.T) {
	h, svc, tasksDir := newTestHandler(t)
	seedBoard(t, svc)
	setBranch(t, tasksDir, "3", "dev/wip/3-fix-the-index", "")
	setBranch(t, tasksDir, "4", "dev/wip/4-old-chore", "dev/4-old-chore")

	button := regexp.MustCompile(`<button[^>]*copy-btn[^>]*>`)
	for _, path := range []string{"/", "/board", "/tasks/4", "/tasks/4/panel"} {
		tags := button.FindAllString(get(t, h, path).Body.String(), -1)
		if len(tags) == 0 {
			t.Errorf("%s has no copy button", path)
		}
		for _, tag := range tags {
			if strings.Contains(tag, "hx-") {
				t.Errorf("%s: copy button carries an htmx attribute: %s", path, tag)
			}
		}
	}
}

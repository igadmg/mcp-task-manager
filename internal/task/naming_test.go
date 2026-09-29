package task

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestShortName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		id    string
		title string
		want  string
	}{
		{"kebab id used as is", "add-login", "Whatever", "add-login"},
		{"numeric id gets the title", "8", "Add login form!", "8-add-login-form"},
		{"id wip falls back", "wip", "", "task-" + sha1Prefix("wip")},
		{"id wip with a title", "wip", "Fix it", "wip-fix-it"},
		{"nothing sluggable", "", "!!!", "task-" + sha1Prefix("")},
		{"unicode title", "9", "Добавить login форму", "9-login"},
		{"uppercase id slugified", "Add-Login", "", "add-login"},
		{"digits-only id without a title", "42", "", "42"},
		{"long title cut at a hyphen", "3", "alpha beta gamma delta epsilon zeta eta theta", "3-alpha-beta-gamma-delta-epsilon-zeta-eta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shortName(&Task{ID: tc.id, Title: tc.title})
			if got != tc.want {
				t.Errorf("shortName(%q, %q) = %q, want %q", tc.id, tc.title, got, tc.want)
			}
			if strings.Contains(got, subSeparator) {
				t.Errorf("shortName(%q, %q) = %q contains the subtask separator", tc.id, tc.title, got)
			}
		})
	}
}

func sha1Prefix(id string) string {
	sum := sha1.Sum([]byte(id))
	return hex.EncodeToString(sum[:])[:8]
}

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Add login form!", "add-login-form"},
		{"  --a--b--  ", "a-b"},
		{"Ünïcode", "n-code"},
		{strings.Repeat("a", 45), strings.Repeat("a", 40)},
		{strings.Repeat("ab-", 20), "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab"},
	} {
		got := slugify(tc.in)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len(got) > maxSlug {
			t.Errorf("slugify(%q) = %q is longer than %d", tc.in, got, maxSlug)
		}
		if got != "" && !slugRe.MatchString(got) {
			t.Errorf("slugify(%q) = %q does not match slugRe", tc.in, got)
		}
	}
	// The sha1 fallback is stable and 8 hex characters.
	if got := shortName(&Task{ID: "wip"}); got != shortName(&Task{ID: "wip"}) || len(got) != len("task-")+8 {
		t.Errorf("fallback name %q is not a stable task-<8hex>", got)
	}
}

func TestWipAndFinalBranch(t *testing.T) {
	wip := wipBranch("dev", "add-login")
	if wip != "dev/wip/add-login" {
		t.Fatalf("wipBranch() = %q", wip)
	}
	final, err := finalBranch(wip)
	if err != nil || final != "dev/add-login" {
		t.Errorf("finalBranch(%q) = (%q, %v), want dev/add-login", wip, final, err)
	}
	if _, err := finalBranch("dev/add-login"); err == nil {
		t.Error("finalBranch() without /wip/: error = nil")
	}
}

func TestSubWipBranch(t *testing.T) {
	parent := wipBranch("dev", "add-login")
	sub := subWipBranch(parent, shortName(&Task{ID: "12", Title: "Form -- validation"}))
	if sub != "dev/wip/add-login--12-form-validation" {
		t.Errorf("subWipBranch() = %q", sub)
	}
	if got := strings.Count(sub, "/"); got != 2 {
		t.Errorf("subtask wip %q is at depth %d, want 3 (a sibling of the parent's)", sub, got+1)
	}
}

func TestCommitMessage(t *testing.T) {
	tk := &Task{ID: "8", Title: "  Add login form ", Description: "Adds the form.\n\nWith validation.\n"}
	if got, want := commitMessage(tk, ""), "Add login form\n\nAdds the form.\n\nWith validation.\n\nTask: 8\n"; got != want {
		t.Errorf("commitMessage() = %q, want %q", got, want)
	}
	tk.Description = "  \n"
	if got, want := commitMessage(tk, "   "), "Add login form\n\nTask: 8\n"; got != want {
		t.Errorf("commitMessage() without description = %q, want %q", got, want)
	}
	override := "feat: custom\n\n  indented body"
	if got := commitMessage(tk, override); got != override {
		t.Errorf("commitMessage() with override = %q, want it verbatim", got)
	}
}

func TestBookkeepingMessages(t *testing.T) {
	for got, want := range map[string]string{
		checkpointMsg("8", "switching to 9"): "wip(8): checkpoint before switching to 9\n",
		completionSnapshotMsg("8"):           "wip(8): snapshot before completion\n",
		closedAsMsg("8", ResolutionObsolete): "wip(8): closed as obsolete\n",
	} {
		if got != want {
			t.Errorf("message = %q, want %q", got, want)
		}
	}
}

func TestGitTxnRollbackOrderAndErrors(t *testing.T) {
	var order []string
	var txn gitTxn
	step := func(name string, err error) {
		txn.add(name, func() error {
			order = append(order, name)
			return err
		})
	}
	step("first", nil)
	step("second", errors.New("boom"))
	step("third", nil)

	err := txn.rollback()
	if want := []string{"third", "second", "first"}; !reflect.DeepEqual(order, want) {
		t.Errorf("undo order = %v, want %v (every undo runs, even after one fails)", order, want)
	}
	if err == nil || !strings.Contains(err.Error(), "undo second: boom") || !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("rollback() error = %v, want it to name the failed step", err)
	}

	order = nil
	if err := txn.rollback(); err != nil || len(order) != 0 {
		t.Errorf("second rollback() = %v ran %v, want a no-op", err, order)
	}
}

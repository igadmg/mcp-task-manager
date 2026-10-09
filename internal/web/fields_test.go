package web

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/task"
)

func TestCardFieldsSortedAndCapped(t *testing.T) {
	// Three fit on the card.
	three := task.Fields{"complexity": "high", "area": "web", "count": 3}
	shown, more, rest := cardFields(three)
	want := []FieldView{{"area", "web"}, {"complexity", "high"}, {"count", "3"}}
	if !reflect.DeepEqual(shown, want) {
		t.Errorf("shown = %#v, want %#v (sorted by key)", shown, want)
	}
	if more != 0 || rest != "" {
		t.Errorf("more, rest = %d, %q; want 0, \"\"", more, rest)
	}

	// A fourth and a fifth collapse into a +N chip, and the tooltip names
	// exactly the ones left off.
	five := task.Fields{"area": "web", "complexity": "high", "count": 3, "owner": "dev", "zone": "eu"}
	shown, more, rest = cardFields(five)
	if len(shown) != maxCardFields {
		t.Errorf("shown = %d chips, want %d", len(shown), maxCardFields)
	}
	if more != 2 {
		t.Errorf("more = %d, want 2", more)
	}
	if rest != "owner: dev, zone: eu" {
		t.Errorf("rest = %q, want the two omitted fields", rest)
	}

	// No fields, no chips.
	if shown, more, rest = cardFields(nil); shown != nil && len(shown) != 0 || more != 0 || rest != "" {
		t.Errorf("cardFields(nil) = %#v, %d, %q", shown, more, rest)
	}
}

func TestCardTemplateRendersFieldChips(t *testing.T) {
	card := CardView{
		ID: "7", Title: "With fields", Priority: "high", Type: "feature", Status: "todo",
		Fields:     []FieldView{{"area", "web"}, {"complexity", "high"}, {"count", "3"}},
		FieldsMore: 2, FieldsRest: "owner: dev, zone: eu",
	}

	var b strings.Builder
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_card.html", card); err != nil {
		t.Fatalf("execute _card.html: %v", err)
	}
	out := b.String()
	for _, want := range []string{"area: web", "complexity: high", "count: 3", "+2", `title="owner: dev, zone: eu"`} {
		if !strings.Contains(out, want) {
			t.Errorf("card is missing %q:\n%s", want, out)
		}
	}
	// The chips reuse the existing muted class, so no CSS rebuild is owed.
	if !strings.Contains(out, `class="chip chip-muted" title="area: web"`) {
		t.Errorf("field chip does not reuse chip-muted:\n%s", out)
	}
}

func TestCardTemplateEscapesFields(t *testing.T) {
	const payload = `<script>alert(1)</script>`
	card := CardView{
		ID: "7", Title: "x", Priority: "low", Type: "bug", Status: "todo",
		Fields:     []FieldView{{payload, payload}},
		FieldsMore: 1, FieldsRest: `" onmouseover="y`,
	}

	var b strings.Builder
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_card.html", card); err != nil {
		t.Fatalf("execute _card.html: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "<script>") || strings.Contains(out, `" onmouseover="`) {
		t.Errorf("field output is not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("escaped payload missing:\n%s", out)
	}
}

func TestDetailViewListsEveryField(t *testing.T) {
	d := &task.TaskDetail{Task: &task.Task{
		ID: "7", Title: "With fields", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "bug",
		Fields: task.Fields{"area": "web", "complexity": "high", "count": 3, "owner": "dev", "zone": "eu"},
	}}
	v := newDetailView(d, nil, nil, fixedNow)

	if len(v.Fields) != 5 {
		t.Fatalf("DetailView.Fields = %#v, want all five (the detail view is not capped)", v.Fields)
	}
	if v.Fields[0].Key != "area" || v.Fields[4].Key != "zone" {
		t.Errorf("DetailView.Fields is not sorted: %#v", v.Fields)
	}
	// The card inside the detail view still caps its chips.
	if v.Card.FieldsMore != 2 {
		t.Errorf("Card.FieldsMore = %d, want 2", v.Card.FieldsMore)
	}

	var b strings.Builder
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_detail.html", v); err != nil {
		t.Fatalf("execute _detail.html: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, ">Fields<") {
		t.Errorf("detail view has no Fields block:\n%s", out)
	}
	for _, want := range []string{"owner", "zone", "eu"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail Fields block is missing %q", want)
		}
	}
}

func TestDetailViewWithoutFieldsHasNoBlock(t *testing.T) {
	d := &task.TaskDetail{Task: &task.Task{
		ID: "8", Title: "Plain", Status: task.StatusTodo, Priority: task.PriorityLow, Type: "bug",
	}}
	v := newDetailView(d, nil, nil, fixedNow)

	var b strings.Builder
	if err := rootTpl.fragments.ExecuteTemplate(&b, "_detail.html", v); err != nil {
		t.Fatalf("execute _detail.html: %v", err)
	}
	if strings.Contains(b.String(), ">Fields<") {
		t.Error("a task with no fields still renders the Fields block")
	}
}

func TestBoardCardsCarryFields(t *testing.T) {
	seven := tk("7", "", task.StatusTodo, task.PriorityHigh, fixedNow)
	seven.Fields = task.Fields{"area": "web"}
	snap := boardOf(seven)

	card := newCardView(seven, snap, fixedNow)
	if len(card.Fields) != 1 || card.Fields[0].Value != "web" {
		t.Errorf("board card fields = %#v, want area: web", card.Fields)
	}
}

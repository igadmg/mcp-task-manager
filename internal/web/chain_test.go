package web

import (
	"net/url"
	"strings"
	"testing"
)

// chainEq compares two chains by value; Column is comparable, so only the
// slice needs the loop.
func chainEq(a, b Chain) bool {
	if a.Root != b.Root || len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
}

func TestParseChainValid(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Chain
		// canonical is Path()'s output; empty means "same as path".
		canonical string
	}{
		{
			name: "depth 0 is the bare task URL",
			path: "/tasks/42",
			want: Chain{Root: "42"},
		},
		{
			name: "depth 0 with a bare w",
			path: "/tasks/42/w",
			want: Chain{Root: "42"},
			// /w carries no column, so the canonical form drops it.
			canonical: "/tasks/42",
		},
		{
			name:      "depth 0 with a trailing slash",
			path:      "/tasks/42/w/",
			want:      Chain{Root: "42"},
			canonical: "/tasks/42",
		},
		{
			name: "one task column",
			path: "/tasks/42/w/t/43",
			want: Chain{Root: "42", Columns: []Column{{KindTask, "43"}}},
		},
		{
			name: "task then file",
			path: "/tasks/42/w/t/43/f/plan.md",
			want: Chain{Root: "42", Columns: []Column{{KindTask, "43"}, {KindFile, "plan.md"}}},
		},
		{
			name: "a description column carries its task id as the ref",
			path: "/tasks/42/w/d/42",
			want: Chain{Root: "42", Columns: []Column{{KindDesc, "42"}}},
		},
		{
			name: "a graph column's ref is the task it highlights",
			path: "/tasks/42/w/g/42",
			want: Chain{Root: "42", Columns: []Column{{KindGraph, "42"}}},
		},
		{
			name: "a chain may revisit a task: it is a history, not a set",
			path: "/tasks/42/w/t/43/t/42/t/43",
			want: Chain{Root: "42", Columns: []Column{
				{KindTask, "43"}, {KindTask, "42"}, {KindTask, "43"},
			}},
		},
		{
			name: "text ids",
			path: "/tasks/web-task-workspace/w/t/workspace-tiler/f/research.md",
			want: Chain{Root: "web-task-workspace", Columns: []Column{
				{KindTask, "workspace-tiler"}, {KindFile, "research.md"},
			}},
		},
		{
			name: "extensionless file name",
			path: "/tasks/42/w/f/design",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "design"}}},
		},
		{
			name: "space in a file name",
			path: "/tasks/42/w/f/my%20notes.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "my notes.md"}}},
		},
		{
			name: "hash in a file name",
			path: "/tasks/42/w/f/a%23b.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "a#b.md"}}},
		},
		{
			name: "question mark in a file name",
			path: "/tasks/42/w/f/a%3Fb.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "a?b.md"}}},
		},
		{
			name: "percent in a file name",
			path: "/tasks/42/w/f/100%25.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "100%.md"}}},
		},
		{
			name: "plus in a file name",
			path: "/tasks/42/w/f/a+b.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "a+b.md"}}},
		},
		{
			name: "unicode file name",
			path: "/tasks/42/w/f/%D0%B8%D1%81%D1%81%D0%BB%D0%B5%D0%B4.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "исслед.md"}}},
		},
		{
			name: "dots in a file name",
			path: "/tasks/42/w/f/a.b.c.md",
			want: Chain{Root: "42", Columns: []Column{{KindFile, "a.b.c.md"}}},
		},
		{
			name: "leading and trailing space is a legal name",
			path: "/tasks/42/w/f/%20x%20",
			want: Chain{Root: "42", Columns: []Column{{KindFile, " x "}}},
		},
		{
			// A task whose id is "w" makes the first "/w/" the wrong
			// one: the tail must be taken by segment index.
			name: "root id is w",
			path: "/tasks/w/w/f/x.md",
			want: Chain{Root: "w", Columns: []Column{{KindFile, "x.md"}}},
		},
		{
			name: "root id is tasks",
			path: "/tasks/tasks/w/t/7",
			want: Chain{Root: "tasks", Columns: []Column{{KindTask, "7"}}},
		},
		{
			name: "root id is f",
			path: "/tasks/f/w/f/f",
			want: Chain{Root: "f", Columns: []Column{{KindFile, "f"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseChain(tc.path)
			if err != nil {
				t.Fatalf("ParseChain(%q) error = %v", tc.path, err)
			}
			if !chainEq(got, tc.want) {
				t.Fatalf("ParseChain(%q) = %+v, want %+v", tc.path, got, tc.want)
			}

			want := tc.canonical
			if want == "" {
				want = tc.path
			}
			if p := got.Path(); p != want {
				t.Errorf("Path() = %q, want %q", p, want)
			}

			// parse -> render -> parse is the identity on the chain.
			again, err := ParseChain(got.Path())
			if err != nil {
				t.Fatalf("re-parsing %q error = %v", got.Path(), err)
			}
			if !chainEq(again, got) {
				t.Errorf("round trip changed the chain: %+v -> %+v", got, again)
			}
		})
	}
}

func TestParseChainRejects(t *testing.T) {
	long := "/tasks/42/w" + strings.Repeat("/t/7", maxChainDepth+1)

	tests := []struct{ name, path string }{
		{"not a task path", "/board"},
		{"panel is not a chain", "/tasks/42/panel"},
		{"files route is not a chain", "/tasks/42/files/x.md"},
		{"wrong marker", "/tasks/42/x/t/7"},
		{"odd tail", "/tasks/42/w/t"},
		{"odd tail deeper", "/tasks/42/w/t/7/f"},
		{"unknown kind", "/tasks/42/w/z/7"},
		{"empty kind", "/tasks/42/w//7"},
		{"empty ref", "/tasks/42/w/t/"},
		{"empty root", "/tasks//w/t/7"},
		// A decoded separator must never pass: this is the case that
		// makes PathValue unusable for the tail.
		{"encoded slash in a ref", "/tasks/42/w/f/a%2Fb.md"},
		{"encoded slash in the root", "/tasks/a%2Fb/w/f/x.md"},
		{"encoded dot-dot ref", "/tasks/42/w/f/%2E%2E"},
		{"lowercase encoded dot-dot ref", "/tasks/42/w/f/%2e%2e"},
		{"single dot ref", "/tasks/42/w/f/."},
		{"dots and spaces ref", "/tasks/42/w/f/.%20."},
		{"dots-only root", "/tasks/%2E%2E/w/f/x.md"},
		{"bad escape", "/tasks/42/w/f/a%zz.md"},
		{"over the depth cap", long},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ParseChain(tc.path); err == nil {
				t.Errorf("ParseChain(%q) = %+v, want an error", tc.path, got)
			}
		})
	}
}

func TestParseChainAtTheDepthCap(t *testing.T) {
	// The cap itself must still parse, and depth 10 is the required one.
	for _, depth := range []int{10, maxChainDepth} {
		path := "/tasks/42/w" + strings.Repeat("/t/7", depth)
		c, err := ParseChain(path)
		if err != nil {
			t.Fatalf("depth %d: ParseChain() error = %v", depth, err)
		}
		if c.Depth() != depth {
			t.Errorf("depth %d: Depth() = %d", depth, c.Depth())
		}
		if c.Path() != path {
			t.Errorf("depth %d: Path() = %q, want %q", depth, c.Path(), path)
		}
	}
}

func TestChainPathEscapesEverySegment(t *testing.T) {
	c := Chain{Root: "a b", Columns: []Column{{KindFile, "a#b?c%d.md"}}}
	want := "/tasks/a%20b/w/f/a%23b%3Fc%25d.md"
	if got := c.Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	// And it parses back to exactly what it was.
	back, err := ParseChain(want)
	if err != nil {
		t.Fatalf("ParseChain(%q) error = %v", want, err)
	}
	if !chainEq(back, c) {
		t.Errorf("round trip = %+v, want %+v", back, c)
	}
}

func TestChainEmptyRootIsTheBoard(t *testing.T) {
	if got := (Chain{}).Path(); got != "/" {
		t.Errorf("zero Chain Path() = %q, want /", got)
	}
}

func TestChainFragmentPairsWithThePage(t *testing.T) {
	c := Chain{Root: "42", Columns: []Column{{KindFile, "plan.md"}}}
	if got, want := c.Fragment(), "/strip/tasks/42/w/f/plan.md"; got != want {
		t.Errorf("Fragment() = %q, want %q", got, want)
	}
	if got, want := (Chain{}).Fragment(), "/strip/"; got != want {
		t.Errorf("board Fragment() = %q, want %q", got, want)
	}
	// Stripping the prefix is how the fragment handler gets the page path.
	if got := strings.TrimPrefix(c.Fragment(), stripPrefix); got != c.Path() {
		t.Errorf("Fragment() minus the prefix = %q, want %q", got, c.Path())
	}
}

func TestChainAppend(t *testing.T) {
	base := Chain{Root: "42"}
	one := base.Append(KindTask, "43")
	two := one.Append(KindFile, "plan.md")

	if base.Depth() != 0 {
		t.Error("Append mutated the chain it was called on")
	}
	if one.Depth() != 1 || two.Depth() != 2 {
		t.Fatalf("depths = %d, %d, want 1, 2", one.Depth(), two.Depth())
	}
	if got, want := two.Path(), "/tasks/42/w/t/43/f/plan.md"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	// Appending to a chain already handed out must not alias its columns.
	other := one.Append(KindFile, "other.md")
	if two.Columns[1].Ref != "plan.md" || other.Columns[1].Ref != "other.md" {
		t.Error("two Appends off one chain share their backing array")
	}
}

func TestChainTruncateTo(t *testing.T) {
	c := Chain{Root: "42", Columns: []Column{
		{KindTask, "43"}, {KindFile, "plan.md"}, {KindTask, "44"},
	}}

	wants := []string{
		"/tasks/42",
		"/tasks/42/w/t/43",
		"/tasks/42/w/t/43/f/plan.md",
		"/tasks/42/w/t/43/f/plan.md/t/44",
	}
	for n, want := range wants {
		got := c.TruncateTo(n)
		if got.Depth() != n {
			t.Errorf("TruncateTo(%d).Depth() = %d", n, got.Depth())
		}
		if got.Path() != want {
			t.Errorf("TruncateTo(%d).Path() = %q, want %q", n, got.Path(), want)
		}
	}
	// Out of range clamps rather than panicking.
	if c.TruncateTo(-1).Depth() != 0 {
		t.Error("TruncateTo(-1) is not the depth-0 chain")
	}
	if c.TruncateTo(99).Depth() != 3 {
		t.Error("TruncateTo past the end is not the whole chain")
	}
	// Truncating must not alias the original.
	cut := c.TruncateTo(2)
	cut.Columns[1] = Column{KindFile, "hijacked"}
	if c.Columns[1].Ref != "plan.md" {
		t.Error("TruncateTo shares its backing array with the original")
	}
}

func TestChainTaskAt(t *testing.T) {
	c := Chain{Root: "root", Columns: []Column{
		{KindFile, "a.md"}, // 0: belongs to the root
		{KindTask, "sub"},  // 1: belongs to the root
		{KindFile, "b.md"}, // 2: belongs to sub
		{KindFile, "c.md"}, // 3: belongs to sub
		{KindTask, "deep"}, // 4: belongs to sub
		{KindFile, "d.md"}, // 5: belongs to deep
	}}
	want := []string{"root", "root", "sub", "sub", "sub", "deep"}
	for i, w := range want {
		if got := c.TaskAt(i); got != w {
			t.Errorf("TaskAt(%d) = %q, want %q", i, got, w)
		}
	}
}

// TestEveryKindHasAWholeEntry guards the registry's contract: the chain parser
// accepts any registered tag, so a half-filled entry would be a 500 waiting
// for a URL.
func TestEveryKindHasAWholeEntry(t *testing.T) {
	for tag, k := range columnKinds {
		if k.Tag != tag {
			t.Errorf("kind %q is registered under the wrong tag (%q)", tag, k.Tag)
		}
		if k.Class == "" || k.Label == "" || k.Template == "" || k.Resolve == nil {
			t.Errorf("kind %q is incomplete: %+v", tag, k)
		}
		if url.PathEscape(string(tag)) != string(tag) {
			t.Errorf("kind tag %q needs escaping; tags must be plain path segments", tag)
		}
	}
	for _, want := range []ColumnKind{KindTask, KindFile, KindDesc, KindGraph} {
		if _, ok := columnKinds[want]; !ok {
			t.Errorf("kind %q is not registered", want)
		}
	}
}

package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRendersAsMarkdown(t *testing.T) {
	cases := map[string]bool{
		// The older artifacts in this backlog have no suffix at all.
		"design":   true,
		"research": true,
		"plan":     true,
		"plan.md":  true,
		"PLAN.MD":  true,
		// A description asks for a document by having no name.
		"":            true,
		"notes.en.md": true,
		// Only dot at index 0: a leading dot, not an extension.
		".notes": true,
		// Anything else is shown as it is.
		"notes.txt":      false,
		"mcp-tasks.yaml": false,
		"a.YAML":         false,
		"design.phase":   false,
		"x.":             false,
	}
	for name, want := range cases {
		if got := rendersAsMarkdown(name); got != want {
			t.Errorf("rendersAsMarkdown(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestNewBodyViewSetsExactlyOneField(t *testing.T) {
	doc := newBodyView("plan.md", "# title")
	if doc.Text != "" {
		t.Errorf("a document set Text as well: %q", doc.Text)
	}
	if !strings.Contains(string(doc.HTML), "<h1>title</h1>") {
		t.Errorf("a document did not render: %q", doc.HTML)
	}

	text := newBodyView("notes.txt", "# not a title")
	if text.HTML != "" {
		t.Errorf("plain text set HTML as well: %q", text.HTML)
	}
	if text.Text != "# not a title" {
		t.Errorf("plain text = %q, want the source unchanged", text.Text)
	}

	// An empty source is neither, so _body.html can tell "empty" from
	// "rendered to nothing".
	if empty := newBodyView("plan.md", ""); empty.Text != "" || empty.HTML != "" {
		t.Errorf("an empty document is not empty: %+v", empty)
	}
}

// TestOneTrustPoint is the other half of the narrowing in templates.go: this
// package may hold exactly one template.HTML, bodyView.HTML, and only
// body.go may write one. A second one - a field somewhere else, a safeHTML
// helper, a conversion in a handler - would mean some other string is trusted
// too, which is the thing html/template is here to prevent.
func TestOneTrustPoint(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HTML" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "template" {
				return true
			}
			if name != "body.go" {
				t.Errorf("%s:%d mentions template.HTML; body.go is the one trust point",
					name, fset.Position(sel.Pos()).Line)
			}
			return true
		})
	}
}

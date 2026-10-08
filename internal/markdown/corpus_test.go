package markdown

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedTags is the complete set of tags this package may emit. It is
// written out rather than pattern-matched on purpose: a renderer that starts
// emitting something new has to come here and say so.
var allowedTags = []string{
	"h1", "h2", "h3", "h4", "h5", "h6",
	"p", "pre", "code", "ul", "ol", "li", "blockquote", "hr",
	"table", "thead", "tbody", "tr", "th", "td",
	"strong", "em", "a", "img", "br",
}

// assertOnlyWhitelistedTags is the invariant that turns the escaping
// argument into something a machine checks: every '<' in the output must
// open or close a tag from allowedTags. If source text could ever reach the
// output unescaped, some input would break this.
func assertOnlyWhitelistedTags(t *testing.T, out string) {
	t.Helper()
	for i := 0; i < len(out); i++ {
		if out[i] != '<' {
			continue
		}
		rest := out[i+1:]
		rest = strings.TrimPrefix(rest, "/")
		if !opensAllowedTag(rest) {
			end := i + 40
			if end > len(out) {
				end = len(out)
			}
			t.Fatalf("output holds a '<' that does not open a whitelisted tag at %d: %q", i, out[i:end])
		}
	}
}

func opensAllowedTag(rest string) bool {
	for _, tag := range allowedTags {
		if !strings.HasPrefix(rest, tag) {
			continue
		}
		switch after := rest[len(tag):]; {
		case after == "":
			return false
		case after[0] == '>', after[0] == ' ':
			return true
		}
	}
	return false
}

// corpusFiles returns every task artifact in this repository: *.md plus the
// extensionless workflow files (design, research, plan) the older tasks use.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..", "tasks")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("no task corpus at %s: %v", root, err)
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".md") || !strings.Contains(name, ".") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(files) == 0 {
		t.Skipf("no artifacts under %s", root)
	}
	return files
}

// TestRenderCorpus is the acceptance criterion "rendering every *.md under
// tasks/ must not panic and must terminate", plus the tag invariant on real
// data. It is table-driven over the corpus, one subtest per artifact.
func TestRenderCorpus(t *testing.T) {
	files := corpusFiles(t)
	var in, out int
	for _, path := range files {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			got := Render(string(src))
			assertOnlyWhitelistedTags(t, got)
			if len(src) > 0 && strings.TrimSpace(string(src)) != "" && got == "" {
				t.Fatalf("%s rendered to nothing from %d bytes", path, len(src))
			}
			in += len(src)
			out += len(got)
		})
	}
	t.Logf("rendered %d artifacts, %d bytes in, %d bytes out", len(files), in, out)
}

// TestRenderCorpusIsStable renders two representative artifacts twice: one
// heavy in tables and code spans, one carrying the task-list checkboxes.
// This is the spot check the design calls for - it asserts invariants and
// shape, never exact bytes, because the corpus is live data.
func TestRenderCorpusIsStable(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "tasks", "web-task-workspace", "research.md"),
		filepath.Join("..", "..", "tasks", "web-ui-kanban-module", "task.md"),
	} {
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Skipf("artifact missing: %v", err)
			}
			first := Render(string(src))
			if first != Render(string(src)) {
				t.Fatal("two renders of the same artifact differed")
			}
			assertOnlyWhitelistedTags(t, first)
			t.Logf("%s: %d bytes in, %d out", path, len(src), len(first))
		})
	}
}

// TestCorpusOutOfSubsetConstructs records, rather than asserts, which
// constructs outside the rendered subset the corpus actually contains. The
// task description asks for exactly this note; run with -v to read it.
func TestCorpusOutOfSubsetConstructs(t *testing.T) {
	needles := map[string]string{
		"HTML entity":         "&middot;",
		"footnote reference":  "[^",
		"tilde fence":         "\n~~~",
		"reference link def":  "\n[1]:",
		"autolink":            "<https://",
		"underscore emphasis": " _",
	}
	counts := map[string]int{}
	for _, path := range corpusFiles(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		for name, needle := range needles {
			counts[name] += strings.Count(string(src), needle)
		}
	}
	for name, n := range counts {
		t.Logf("out-of-subset construct %q appears %d times and renders as literal text", name, n)
	}
}

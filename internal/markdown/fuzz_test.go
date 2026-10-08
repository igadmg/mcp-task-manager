package markdown

import (
	"os"
	"strings"
	"testing"
)

// FuzzRender asserts the two properties that have to hold for every possible
// input: Render returns (no panic, no hang), and every '<' in its output is
// a tag this package wrote. The seed corpus is this repository's own
// artifacts plus the pathological inputs listed in the task's research.
func FuzzRender(f *testing.F) {
	seeds := []string{
		"",
		"\n",
		"\r\n\r",
		"# h\n\npara **bold** `code` [l](a.md)\n",
		"```go\nx\n",
		"> - a\n>\n>   ```go\n>   x\n>   ```\n",
		"| a | b |\n| :-- | --: |\n",
		"| a |\n| --- |\n| `x|y` |\n",
		"- [ ] a\n- [x] b\n",
		strings.Repeat("> ", 100) + "x",
		strings.Repeat("*", 1000) + "x" + strings.Repeat("*", 1000),
		strings.Repeat("[", 200) + "x",
		strings.Repeat("`", 200),
		strings.Repeat("a", 1<<20),
		"[a](javascript:alert(1))",
		"![a](data:text/html,<script>alert(1)</script>)",
		"<script>alert(1)</script>",
		"a  \nb\\\nc",
		"---\nid: 7\n---\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	for _, path := range corpusSeedFiles() {
		if src, err := os.ReadFile(path); err == nil {
			f.Add(string(src))
		}
	}

	f.Fuzz(func(t *testing.T, src string) {
		out := Render(src)
		for i := 0; i < len(out); i++ {
			if out[i] != '<' {
				continue
			}
			rest := strings.TrimPrefix(out[i+1:], "/")
			if !opensAllowedTag(rest) {
				t.Fatalf("input %q produced a '<' outside a whitelisted tag: %q", src, out[i:])
			}
		}
	})
}

// corpusSeedFiles is the fuzz seed half of corpusFiles. It takes no
// *testing.T because f.Fuzz's setup runs before any subtest, and a missing
// corpus simply means fewer seeds.
func corpusSeedFiles() []string {
	entries, err := os.ReadDir("../../tasks")
	if err != nil {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		inner, err := os.ReadDir("../../tasks/" + entry.Name())
		if err != nil {
			continue
		}
		for _, file := range inner {
			name := file.Name()
			if file.IsDir() {
				continue
			}
			if strings.HasSuffix(name, ".md") || !strings.Contains(name, ".") {
				files = append(files, "../../tasks/"+entry.Name()+"/"+name)
			}
		}
	}
	return files
}

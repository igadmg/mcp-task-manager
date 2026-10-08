package markdown

import (
	"strings"
	"testing"
)

// TestRenderBlocks pins the exact output of every supported block construct.
// Exact comparison is affordable here because each case is a few lines; the
// whole-corpus test asserts invariants instead.
func TestRenderBlocks(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"empty", "", ""},
		{"only a newline", "\n", ""},
		{"only whitespace", "   \n\n \t \n", ""},
		{"one paragraph", "hello", "<p>hello</p>\n"},
		{"two paragraphs", "a\n\nb", "<p>a</p>\n<p>b</p>\n"},
		{"wrapped paragraph", "a\nb", "<p>a\nb</p>\n"},
		{"indented continuation is dedented", "a\n   b", "<p>a\nb</p>\n"},

		{"h1", "# t", "<h1>t</h1>\n"},
		{"h2", "## t", "<h2>t</h2>\n"},
		{"h3", "### t", "<h3>t</h3>\n"},
		{"h4", "#### t", "<h4>t</h4>\n"},
		{"h5", "##### t", "<h5>t</h5>\n"},
		{"h6", "###### t", "<h6>t</h6>\n"},
		{"seven hashes is a paragraph", "####### t", "<p>####### t</p>\n"},
		{"hash without a space is a paragraph", "#t", "<p>#t</p>\n"},
		{"closing hashes are decoration", "## t ##", "<h2>t</h2>\n"},
		{"empty heading", "##", "<h2></h2>\n"},
		{"heading renders inline content", "# a **b** `c`", "<h1>a <strong>b</strong> <code>c</code></h1>\n"},

		{"dashes break", "---", "<hr />\n"},
		{"stars break", "***", "<hr />\n"},
		{"underscores break", "___", "<hr />\n"},
		{"spaced break", "- - -", "<hr />\n"},
		{"two dashes are a paragraph", "--", "<p>--</p>\n"},
		// The setext decision: "---" after text is a break, never a heading.
		{"dashes after a paragraph", "t\n---", "<p>t</p>\n<hr />\n"},
		{"frontmatter renders as a break and a paragraph", "---\nid: 7\n---\n\nbody",
			"<hr />\n<p>id: 7</p>\n<hr />\n<p>body</p>\n"},

		{"fence with a language", "```go\nx\n```", "<pre><code class=\"language-go\">x\n</code></pre>\n"},
		{"fence without an info string", "```\nx\n```", "<pre><code>x\n</code></pre>\n"},
		{"mermaid is an ordinary fence", "```mermaid\ngraph LR\n```",
			"<pre><code class=\"language-mermaid\">graph LR\n</code></pre>\n"},
		{"info string's first word is the language", "```go amd64\nx\n```", "<pre><code class=\"language-go\">x\n</code></pre>\n"},
		{"unusable info string drops the class", "```not/a/class\nx\n```", "<pre><code>x\n</code></pre>\n"},
		{"empty fence", "```\n```", "<pre><code></code></pre>\n"},
		{"unterminated fence", "```\nx", "<pre><code>x\n</code></pre>\n"},
		{"tilde fence", "~~~\nx\n~~~", "<pre><code>x\n</code></pre>\n"},
		{"fence body is verbatim", "```\n# not a heading\n- not a list\n```",
			"<pre><code># not a heading\n- not a list\n</code></pre>\n"},
		{"longer fence holds a shorter one", "````\n```\n````", "<pre><code>```\n</code></pre>\n"},
		{"text after a fence", "```\nx\n```\nafter", "<pre><code>x\n</code></pre>\n<p>after</p>\n"},

		{"bullet list", "- a\n- b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
		{"star bullets", "* a\n* b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>\n"},
		{"plus bullets", "+ a", "<ul>\n<li>a</li>\n</ul>\n"},
		{"ordered list", "1. a\n2. b", "<ol>\n<li>a</li>\n<li>b</li>\n</ol>\n"},
		{"ordered list with parens", "1) a", "<ol>\n<li>a</li>\n</ol>\n"},
		{"loose list", "- a\n\n- b", "<ul>\n<li><p>a</p></li>\n<li><p>b</p></li>\n</ul>\n"},
		{"nested bullets", "- a\n  - b", "<ul>\n<li>a\n<ul>\n<li>b</li>\n</ul>\n</li>\n</ul>\n"},
		{"three levels", "- a\n  - b\n    - c",
			"<ul>\n<li>a\n<ul>\n<li>b\n<ul>\n<li>c</li>\n</ul>\n</li>\n</ul>\n</li>\n</ul>\n"},
		{"ordered inside bullet", "- a\n  1. b",
			"<ul>\n<li>a\n<ol>\n<li>b</li>\n</ol>\n</li>\n</ul>\n"},
		{"item with a wrapped line", "- a\n  b", "<ul>\n<li>a\nb</li>\n</ul>\n"},
		{"item with two paragraphs", "- a\n\n  b",
			"<ul>\n<li><p>a</p>\n<p>b</p>\n</li>\n</ul>\n"},
		{"fence inside an item", "- a\n\n  ```go\n  x\n  ```",
			"<ul>\n<li><p>a</p>\n<pre><code class=\"language-go\">x\n</code></pre>\n</li>\n</ul>\n"},
		{"list ends at a dedented paragraph", "- a\nb", "<ul>\n<li>a</li>\n</ul>\n<p>b</p>\n"},

		{"unchecked task item", "- [ ] a",
			"<ul>\n<li class=\"task-list-item\">&#9744; a</li>\n</ul>\n"},
		{"checked task item", "- [x] a",
			"<ul>\n<li class=\"task-list-item\">&#9745; a</li>\n</ul>\n"},
		{"capital X is checked", "- [X] a",
			"<ul>\n<li class=\"task-list-item\">&#9745; a</li>\n</ul>\n"},
		{"task items mixed with plain ones", "- [ ] a\n- b",
			"<ul>\n<li class=\"task-list-item\">&#9744; a</li>\n<li>b</li>\n</ul>\n"},
		{"a bracket that is not a task marker", "- [a] b", "<ul>\n<li>[a] b</li>\n</ul>\n"},

		{"blockquote", "> a", "<blockquote>\n<p>a</p>\n</blockquote>\n"},
		{"multi-line blockquote", "> a\n> b", "<blockquote>\n<p>a\nb</p>\n</blockquote>\n"},
		{"blockquote without a space", ">a", "<blockquote>\n<p>a</p>\n</blockquote>\n"},
		{"list inside a blockquote", "> - a",
			"<blockquote>\n<ul>\n<li>a</li>\n</ul>\n</blockquote>\n"},
		{"nested blockquote", "> > a",
			"<blockquote>\n<blockquote>\n<p>a</p>\n</blockquote>\n</blockquote>\n"},
		{"blockquote ends at a plain line", "> a\nb", "<blockquote>\n<p>a</p>\n</blockquote>\n<p>b</p>\n"},

		{"table", "| a | b |\n| --- | --- |\n| 1 | 2 |",
			"<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n" +
				"<tbody>\n<tr>\n<td>1</td>\n<td>2</td>\n</tr>\n</tbody>\n</table>\n"},
		{"table alignment", "| a | b | c |\n| :-- | :-: | --: |\n| 1 | 2 | 3 |",
			"<table>\n<thead>\n<tr>\n<th class=\"align-left\">a</th>\n<th class=\"align-center\">b</th>" +
				"\n<th class=\"align-right\">c</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n" +
				"<td class=\"align-left\">1</td>\n<td class=\"align-center\">2</td>\n" +
				"<td class=\"align-right\">3</td>\n</tr>\n</tbody>\n</table>\n"},
		{"header-only table", "| a |\n| --- |",
			"<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n"},
		{"short body row is padded", "| a | b |\n| --- | --- |\n| 1 |",
			"<table>\n<thead>\n<tr>\n<th>a</th>\n<th>b</th>\n</tr>\n</thead>\n" +
				"<tbody>\n<tr>\n<td>1</td>\n<td></td>\n</tr>\n</tbody>\n</table>\n"},
		{"long body row is truncated", "| a |\n| --- |\n| 1 | 2 |",
			"<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n" +
				"<tbody>\n<tr>\n<td>1</td>\n</tr>\n</tbody>\n</table>\n"},
		{"escaped pipe stays in the cell", "| a |\n| --- |\n| x \\| y |",
			"<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n" +
				"<tbody>\n<tr>\n<td>x | y</td>\n</tr>\n</tbody>\n</table>\n"},
		{"pipes without a delimiter row are a paragraph", "| a | b |\nplain",
			"<p>| a | b |\nplain</p>\n"},
		{"mismatched delimiter row is a paragraph", "| a | b |\n| --- |",
			"<p>| a | b |\n| --- |</p>\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Render(tc.src); got != tc.want {
				t.Fatalf("Render(%q)\n got: %q\nwant: %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestRenderInlineConstructs pins the inline level the same way.
func TestRenderInlineConstructs(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"inline code", "a `b` c", "<p>a <code>b</code> c</p>\n"},
		{"code span with a backtick", "a ``b ` c`` d", "<p>a <code>b ` c</code> d</p>\n"},
		{"code span strips one space", "` b `", "<p><code>b</code></p>\n"},
		{"code span of spaces is kept", "`  `", "<p><code>  </code></p>\n"},
		{"code span spanning a wrapped line", "a `b\nc` d", "<p>a <code>b c</code> d</p>\n"},
		{"unclosed backtick is literal", "a `b", "<p>a `b</p>\n"},

		{"strong", "a **b** c", "<p>a <strong>b</strong> c</p>\n"},
		{"emphasis", "a *b* c", "<p>a <em>b</em> c</p>\n"},
		{"strong across a wrapped line", "a **b\nc** d", "<p>a <strong>b\nc</strong> d</p>\n"},
		{"emphasis inside strong", "**a *b* c**", "<p><strong>a <em>b</em> c</strong></p>\n"},
		{"two emphases on one line", "*a* and *b*", "<p><em>a</em> and <em>b</em></p>\n"},
		{"unmatched star is literal", "a * b", "<p>a * b</p>\n"},
		{"unclosed strong is literal", "a **b", "<p>a **b</p>\n"},
		{"opener followed by a space is literal", "a * b *", "<p>a * b *</p>\n"},
		{"three stars are literal", "***a***", "<p>***a***</p>\n"},
		{"star inside a code span is not a delimiter", "`*a*`", "<p><code>*a*</code></p>\n"},
		// The underscore decision: snake_case prose is untouched.
		{"underscores are literal", "a parent_id and _b_", "<p>a parent_id and _b_</p>\n"},

		{"backslash escapes a star", `a \*b\* c`, "<p>a *b* c</p>\n"},
		{"backslash escapes a backtick", "a \\`b\\` c", "<p>a `b` c</p>\n"},
		{"backslash escapes a bracket", `a \[b\](c)`, "<p>a [b](c)</p>\n"},
		{"backslash before a letter is literal", `a \b c`, "<p>a \\b c</p>\n"},

		{"link", "[a](b.md)", "<p><a href=\"b.md\">a</a></p>\n"},
		{"link with a title", "[a](b.md \"t\")", "<p><a href=\"b.md\" title=\"t\">a</a></p>\n"},
		{"link with inline content", "[a **b**](c.md)", "<p><a href=\"c.md\">a <strong>b</strong></a></p>\n"},
		{"angle-bracket destination", "[a](<b.md>)", "<p><a href=\"b.md\">a</a></p>\n"},
		{"a space in a destination loses the link", "[a](<b c.md>)", "<p>a</p>\n"},
		{"image", "![a](b.png)", "<p><img src=\"b.png\" alt=\"a\" /></p>\n"},
		{"brackets without a destination are literal", "[a] b", "<p>[a] b</p>\n"},
		{"unparsable destination is literal", "[a](b c)", "<p>[a](b c)</p>\n"},

		{"hard break", "a  \nb", "<p>a<br />\nb</p>\n"},
		{"one trailing space is not a break", "a \nb", "<p>a\nb</p>\n"},
		{"trailing spaces at the end of input", "a  ", "<p>a</p>\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Render(tc.src); got != tc.want {
				t.Fatalf("Render(%q)\n got: %q\nwant: %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestTableCellWithCodeAndLink is called out by name in the acceptance
// criteria.
func TestTableCellWithCodeAndLink(t *testing.T) {
	src := "| field | doc |\n| --- | --- |\n| `parent_id` | [design](design.md) |"
	want := "<table>\n<thead>\n<tr>\n<th>field</th>\n<th>doc</th>\n</tr>\n</thead>\n" +
		"<tbody>\n<tr>\n<td><code>parent_id</code></td>\n" +
		"<td><a href=\"design.md\">design</a></td>\n</tr>\n</tbody>\n</table>\n"
	if got := Render(src); got != want {
		t.Fatalf("Render\n got: %q\nwant: %q", got, want)
	}
}

// TestLineEndingsAreEquivalent covers CRLF input and a missing trailing
// newline - 114 of this repo's 173 artifacts have no trailing newline.
func TestLineEndingsAreEquivalent(t *testing.T) {
	const body = "# t\n\n- a\n- b\n\n> q\n"
	base := Render(body)
	if base == "" {
		t.Fatal("Render returned nothing for the base document")
	}
	variants := map[string]string{
		"no trailing newline": strings.TrimSuffix(body, "\n"),
		"CRLF":                strings.ReplaceAll(body, "\n", "\r\n"),
		"CRLF without a trailing newline": strings.TrimSuffix(
			strings.ReplaceAll(body, "\n", "\r\n"), "\r\n"),
		"lone CR": strings.ReplaceAll(body, "\n", "\r"),
	}
	for name, src := range variants {
		t.Run(name, func(t *testing.T) {
			if got := Render(src); got != base {
				t.Fatalf("%s rendered differently\n got: %q\nwant: %q", name, got, base)
			}
		})
	}
}

// TestNestingTerminates checks the depth cap: deep input degrades to text
// instead of panicking or hanging.
func TestNestingTerminates(t *testing.T) {
	cases := map[string]string{
		"deep blockquotes":       strings.Repeat("> ", 200) + "x",
		"deep lists":             deepList(200),
		"deep emphasis":          strings.Repeat("*", 1000) + "x" + strings.Repeat("*", 1000),
		"deep brackets":          strings.Repeat("[", 500) + "x" + strings.Repeat("]", 500),
		"deep backticks":         strings.Repeat("`", 500) + "x",
		"long single line":       strings.Repeat("a*b`c[d]", 20000),
		"stars only":             strings.Repeat("*", 50000),
		"fence in a quoted list": "> - a\n>\n>   ```go\n>   x\n>   ```\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := Render(src)
			if len(src) > 0 && got == "" {
				t.Fatalf("Render returned nothing for %q", name)
			}
			assertOnlyWhitelistedTags(t, got)
		})
	}
}

func deepList(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(strings.Repeat("  ", i))
		b.WriteString("- x\n")
	}
	return b.String()
}

// TestRenderIsDeterministic guards against map iteration or any other
// source of per-call variation leaking into the output.
func TestRenderIsDeterministic(t *testing.T) {
	src := "# t\n\n| a | b |\n| :-- | --: |\n| `x` | [l](a.md) |\n\n- [ ] q\n- [x] r\n"
	first := Render(src)
	for i := 0; i < 20; i++ {
		if got := Render(src); got != first {
			t.Fatalf("render %d differed\n got: %q\nwant: %q", i, got, first)
		}
	}
}

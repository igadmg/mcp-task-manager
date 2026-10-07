package markdown

import (
	"strings"
	"testing"
)

// TestScriptIsNeverPassedThrough covers the central acceptance criterion in
// every position the task description names.
func TestScriptIsNeverPassedThrough(t *testing.T) {
	cases := map[string]string{
		"in prose":            "before <script>alert(1)</script> after",
		"in a heading":        "# <script>alert(1)</script>",
		"in a code block":     "```\n<script>alert(1)</script>\n```",
		"in a code span":      "a `<script>alert(1)</script>` b",
		"in a table cell":     "| a |\n| --- |\n| <script>alert(1)</script> |",
		"in link text":        "[<script>alert(1)</script>](a.md)",
		"in a link title":     "[a](b.md \"<script>alert(1)</script>\")",
		"in a link URL":       "[a](<script>alert(1)</script>)",
		"in an image alt":     "![<script>alert(1)</script>](a.png)",
		"in a blockquote":     "> <script>alert(1)</script>",
		"in a list item":      "- <script>alert(1)</script>",
		"in a fence info":     "```<script>\nx\n```",
		"as a bare tag":       "<img src=x onerror=alert(1)>",
		"in an attribute gap": "<a href=\"javascript:alert(1)\">x</a>",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := Render(src)
			if strings.Contains(got, "<script") || strings.Contains(got, "</script") {
				t.Fatalf("script tag survived: %s", got)
			}
			if strings.Contains(got, "onerror") && !strings.Contains(got, "&lt;img") {
				t.Fatalf("event handler reached markup: %s", got)
			}
			assertOnlyWhitelistedTags(t, got)
		})
	}
}

// TestEveryTextPositionIsEscaped checks all five characters in each position
// that carries source text.
func TestEveryTextPositionIsEscaped(t *testing.T) {
	const raw = `<&">'`
	const want = `&lt;&amp;&#34;&gt;&#39;`
	cases := map[string]string{
		"paragraph":    raw,
		"heading":      "# " + raw,
		"code block":   "```\n" + raw + "\n```",
		"code span":    "`" + raw + "`",
		"table cell":   "| h |\n| --- |\n| " + raw + " |",
		"list item":    "- " + raw,
		"blockquote":   "> " + raw,
		"link text":    "[" + raw + "](a.md)",
		"emphasis":     "**" + raw + "**",
		"image alt":    "![" + raw + "](a.png)",
		"link title":   "[a](b.md \"" + strings.ReplaceAll(raw, `"`, "") + "\")",
		"table header": "| " + raw + " |\n| --- |",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := Render(src)
			needle := want
			if name == "link title" {
				needle = `&lt;&amp;&gt;&#39;`
			}
			if !strings.Contains(got, needle) {
				t.Fatalf("Render(%q) = %q; want it to contain %q", src, got, needle)
			}
			assertOnlyWhitelistedTags(t, got)
		})
	}
}

// TestAttributeValuesCannotBreakOut puts a quote in every attribute the
// renderer emits.
func TestAttributeValuesCannotBreakOut(t *testing.T) {
	cases := map[string]string{
		"href":  `[a](b".md)`,
		"title": `[a](b.md "x\"y")`,
		"src":   `![a](b".png)`,
		"alt":   `![a"b](c.png)`,
		"class": "```go\"onload=alert(1)\nx\n```",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			got := Render(src)
			if strings.Contains(got, `"onload`) || strings.Contains(got, "onload=") {
				t.Fatalf("attribute break-out: %s", got)
			}
			assertOnlyWhitelistedTags(t, got)
		})
	}
}

// TestNoForbiddenConstructsInOutput states the "fragment, not a document"
// half of the scope: whatever the input, these never appear.
func TestNoForbiddenConstructsInOutput(t *testing.T) {
	sources := []string{
		"<html><body>x</body></html>",
		"<style>body{}</style>",
		"<iframe src=x></iframe>",
		"<div onclick=\"alert(1)\" style=\"color:red\">x</div>",
		"# <h1 id=\"x\">y</h1>",
		"```html\n<script>x</script>\n```",
		"- <input type=checkbox checked>",
		"[a](b.md)\n\n![c](d.png)\n\n> e\n\n| f |\n| --- |",
	}
	// Each needle carries the character that would make it live markup: a
	// real '<' for a tag, a real '"' for an attribute. Escaped text holds
	// "&lt;div" and "style=&#34;", which are prose and must survive.
	forbidden := []string{
		"<html", "<body", "<style", "<iframe", "<script", "<div", "<input",
		"style=\"", "onclick=\"", "onerror=\"", " id=\"",
	}
	for _, src := range sources {
		t.Run(src[:min(20, len(src))], func(t *testing.T) {
			got := Render(src)
			for _, bad := range forbidden {
				if strings.Contains(got, bad) {
					t.Errorf("output holds %q: %s", bad, got)
				}
			}
			assertOnlyWhitelistedTags(t, got)
		})
	}
}

// TestRenderHasNoUnsafeMode is a compile-time-ish assertion by inspection:
// Render takes a string and returns a string, so there is no option to flip.
func TestRenderHasNoUnsafeMode(t *testing.T) {
	var f func(string) string = Render
	if got := f("x"); got != "<p>x</p>\n" {
		t.Fatalf("Render(%q) = %q", "x", got)
	}
}

package markdown

import (
	"strings"
	"testing"
)

// TestSafeURL is the accept/reject table from
// tasks/workspace-markdown/research.md, one case per row.
func TestSafeURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string // "" means rejected
	}{
		{"http", "http://example.com/a", "http://example.com/a"},
		{"https with query and fragment", "https://example.com/a?b=1#c", "https://example.com/a?b=1#c"},
		{"scheme case is ignored", "HTTPS://EXAMPLE.COM", "HTTPS://EXAMPLE.COM"},
		{"mailto", "mailto:a@b.c", "mailto:a@b.c"},
		{"mailto without address", "mailto:", "mailto:"},
		{"relative file", "design.md", "design.md"},
		{"relative with dot", "./plan", "./plan"},
		{"parent relative", "../other/design.md", "../other/design.md"},
		{"in-page anchor", "#anchor", "#anchor"},
		{"colon after a slash", "tasks/x.md:12", "tasks/x.md:12"},
		{"colon in a query", "a?b=c:d", "a?b=c:d"},
		{"surrounding space is trimmed", "  https://example.com  ", "https://example.com"},

		{"javascript", "javascript:alert(1)", ""},
		{"javascript mixed case", "JaVaScRiPt:alert(1)", ""},
		{"javascript with a tab in the scheme", "java\tscript:alert(1)", ""},
		{"javascript with a newline in the scheme", "java\nscript:alert(1)", ""},
		{"javascript with an entity in the scheme", "java&#9;script:alert(1)", ""},
		{"leading control byte", "\x01javascript:alert(1)", ""},
		{"data", "data:text/html,<script>alert(1)</script>", ""},
		{"vbscript", "vbscript:msgbox", ""},
		{"file", "file:///etc/passwd", ""},
		{"protocol relative", "//evil.example/x", ""},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"lone colon", ":", ""},
		{"scheme starting with a digit", "1http://example.com", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := safeURL(tc.raw)
			if tc.want == "" {
				if ok {
					t.Fatalf("safeURL(%q) = %q, true; want rejected", tc.raw, got)
				}
				return
			}
			if !ok {
				t.Fatalf("safeURL(%q) rejected; want %q", tc.raw, tc.want)
			}
			if got != tc.want {
				t.Fatalf("safeURL(%q) = %q; want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestRejectedURLKeepsText covers the acceptance criterion directly: a
// rejected URL loses its tag and keeps its visible text.
func TestRejectedURLKeepsText(t *testing.T) {
	for _, scheme := range []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"java\tscript:alert(1)",
		"java&#9;script:alert(1)",
		"data:text/html,x",
		"vbscript:msgbox",
		"file:///etc/passwd",
		"//evil.example/x",
	} {
		t.Run(scheme, func(t *testing.T) {
			got := Render("a [click me](" + scheme + ") b")
			if strings.Contains(got, "<a") {
				t.Errorf("Render emitted an anchor for %q: %s", scheme, got)
			}
			if !strings.Contains(got, "click me") {
				t.Errorf("Render dropped the link text for %q: %s", scheme, got)
			}
			if strings.Contains(strings.ToLower(got), "script:") && !strings.Contains(got, "&amp;") {
				// The scheme may survive as escaped prose only when it was
				// never part of an attribute - assert it is not in one.
				if strings.Contains(got, "href") || strings.Contains(got, "src") {
					t.Errorf("rejected scheme reached an attribute: %s", got)
				}
			}
		})
	}
}

func TestRejectedImageURLKeepsAltText(t *testing.T) {
	got := Render("![the alt](javascript:alert(1))")
	if strings.Contains(got, "<img") {
		t.Fatalf("Render emitted an image for a rejected URL: %s", got)
	}
	if !strings.Contains(got, "the alt") {
		t.Fatalf("Render dropped the alt text: %s", got)
	}
}

func TestAcceptedURLsRender(t *testing.T) {
	cases := map[string]string{
		"[a](https://example.com)": `<a href="https://example.com">a</a>`,
		"[a](design.md)":           `<a href="design.md">a</a>`,
		"[a](#here)":               `<a href="#here">a</a>`,
		"[a](mailto:x@y.z)":        `<a href="mailto:x@y.z">a</a>`,
		`[a](b.md "the title")`:    `<a href="b.md" title="the title">a</a>`,
		"![a](img.png)":            `<img src="img.png" alt="a" />`,
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			if got := Render(src); !strings.Contains(got, want) {
				t.Fatalf("Render(%q) = %q; want it to contain %q", src, got, want)
			}
		})
	}
}

// TestSafeURLAmpersand pins the step-5 rule: an '&' in the leading segment
// rejects, an '&' in a query string does not.
func TestSafeURLAmpersand(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/a?b=1&c=2": true,
		"a?b=1&c=2":                     true,
		"/a&b":                          true,
		"a&b.md":                        false,
		"java&#9;script:alert(1)":       false,
	}
	for raw, want := range cases {
		if _, ok := safeURL(raw); ok != want {
			t.Errorf("safeURL(%q) accepted = %v; want %v", raw, ok, want)
		}
	}
}

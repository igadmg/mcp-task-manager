package markdown

import (
	"html"
	"strings"
)

const (
	// maxInlineDepth bounds emphasis and link-text nesting. Past it the
	// remaining text is written as literal text, so an adversarial
	// "*a*b*c*d..." nest degrades instead of growing the Go stack.
	maxInlineDepth = 16

	// inlineLookahead bounds the total bytes of forward scanning one
	// renderInline call may do looking for a closing delimiter. A normal
	// paragraph never comes close; a pathological line of a million
	// asterisks exhausts it and the surplus delimiters render literally.
	// This is what keeps the inline pass linear in the input.
	inlineLookahead = 1 << 20
)

// writeText is the only function in this package that copies source bytes
// into the output. Everything else writes tag literals and attribute names
// that are constants here, which is why every '<' in a rendered fragment is
// a tag this package wrote. Keep it that way: a new writer must route its
// text through writeText rather than through b.WriteString.
func writeText(b *strings.Builder, s string) {
	b.WriteString(html.EscapeString(s))
}

// inliner renders the inline level of one block. It owns the lookahead
// budget, which is shared by every nested call so that nesting cannot
// multiply the work.
type inliner struct {
	b      *strings.Builder
	budget int
}

// renderInline writes the inline rendering of s. Line breaks inside s are
// significant: a line ending in two or more spaces becomes a hard break.
func renderInline(b *strings.Builder, s string) {
	in := &inliner{b: b, budget: inlineLookahead}
	in.write(s, 0)
}

func (in *inliner) spend(n int) bool {
	if in.budget < n {
		return false
	}
	in.budget -= n
	return true
}

// write scans s once, left to right, advancing at least one byte per
// iteration - that is the whole termination argument. Bytes that are not
// part of a construct accumulate in a pending run flushed through
// writeText.
func (in *inliner) write(s string, depth int) {
	if depth > maxInlineDepth {
		writeText(in.b, s)
		return
	}
	start, i := 0, 0
	for i < len(s) {
		switch s[i] {
		case '\\':
			if i+1 < len(s) && isASCIIPunct(s[i+1]) {
				in.flush(s, start, i)
				writeText(in.b, s[i+1:i+2])
				i += 2
				start = i
				continue
			}
			i++
		case '\n':
			// Trailing spaces belong to the break, not to the text.
			k := i
			for k > start && s[k-1] == ' ' {
				k--
			}
			in.flush(s, start, k)
			if i-k >= 2 {
				in.b.WriteString("<br />")
			}
			in.b.WriteString("\n")
			i++
			start = i
		case '`':
			n := runLen(s, i, '`')
			if body, end, ok := in.codeSpan(s, i, n); ok {
				in.flush(s, start, i)
				in.b.WriteString("<code>")
				writeText(in.b, body)
				in.b.WriteString("</code>")
				i, start = end, end
				continue
			}
			i += n
		case '!':
			if i+1 < len(s) && s[i+1] == '[' {
				if alt, url, title, end, ok := in.linkAt(s, i+1); ok {
					in.flush(s, start, i)
					in.writeImage(alt, url, title)
					i, start = end, end
					continue
				}
			}
			i++
		case '[':
			if text, url, title, end, ok := in.linkAt(s, i); ok {
				in.flush(s, start, i)
				in.writeLink(text, url, title, depth)
				i, start = end, end
				continue
			}
			i++
		case '*':
			n := runLen(s, i, '*')
			// A run of three or more is literal text: there is no
			// bold-italic in this subset, and the corpus has no such run
			// outside code spans.
			if n <= 2 {
				if body, end, ok := in.emphasis(s, i, n); ok {
					in.flush(s, start, i)
					tag := "<em>"
					closing := "</em>"
					if n == 2 {
						tag, closing = "<strong>", "</strong>"
					}
					in.b.WriteString(tag)
					in.write(body, depth+1)
					in.b.WriteString(closing)
					i, start = end, end
					continue
				}
			}
			i += n
		default:
			i++
		}
	}
	in.flush(s, start, len(s))
}

func (in *inliner) flush(s string, from, to int) {
	if to > from {
		writeText(in.b, s[from:to])
	}
}

// codeSpan matches a run of n backticks at i against the next run of exactly
// n backticks and returns the body between them.
func (in *inliner) codeSpan(s string, i, n int) (string, int, bool) {
	for j := i + n; j < len(s); {
		if !in.spend(1) {
			return "", 0, false
		}
		if s[j] != '`' {
			j++
			continue
		}
		m := runLen(s, j, '`')
		if m == n {
			return stripCodeSpan(s[i+n : j]), j + m, true
		}
		j += m
	}
	return "", 0, false
}

// stripCodeSpan applies the CommonMark rule: line endings inside a code span
// become spaces, and if the result both begins and ends with a space without
// being all spaces, one space is stripped from each end.
func stripCodeSpan(body string) string {
	body = strings.ReplaceAll(body, "\n", " ")
	if len(body) >= 2 && body[0] == ' ' && body[len(body)-1] == ' ' && strings.TrimSpace(body) != "" {
		body = body[1 : len(body)-1]
	}
	return body
}

// linkAt parses "[text](url)" or "[text](url \"title\")" starting at the '['
// at i. A destination it cannot parse cleanly is not a link at all, so the
// brackets render as literal text.
func (in *inliner) linkAt(s string, i int) (text, url, title string, end int, ok bool) {
	depth := 0
	j := i
	for j < len(s) {
		if !in.spend(1) {
			return "", "", "", 0, false
		}
		switch s[j] {
		case '\\':
			j += 2
			continue
		case '`':
			n := runLen(s, j, '`')
			if _, e, found := in.codeSpan(s, j, n); found {
				j = e
				continue
			}
			j += n
			continue
		case '\n':
			return "", "", "", 0, false
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				text = s[i+1 : j]
				j++
				url, title, end, ok = in.destination(s, j)
				if !ok {
					return "", "", "", 0, false
				}
				return text, url, title, end, true
			}
		}
		j++
	}
	return "", "", "", 0, false
}

// destination parses the "(url)" or "(url \"title\")" that must follow a
// link's closing bracket at i.
func (in *inliner) destination(s string, i int) (url, title string, end int, ok bool) {
	if i >= len(s) || s[i] != '(' {
		return "", "", 0, false
	}
	depth := 0
	for j := i; j < len(s); j++ {
		if !in.spend(1) {
			return "", "", 0, false
		}
		switch s[j] {
		case '\\':
			j++
		case '\n':
			return "", "", 0, false
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				url, title, ok = splitDestination(s[i+1 : j])
				if !ok {
					return "", "", 0, false
				}
				return url, title, j + 1, true
			}
		}
	}
	return "", "", 0, false
}

// splitDestination separates a link destination from its optional quoted
// title. Anything else after the destination means this is not a link.
func splitDestination(inner string) (url, title string, ok bool) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return "", "", false
	}
	if inner[0] == '<' {
		closing := strings.IndexByte(inner, '>')
		if closing < 0 {
			return "", "", false
		}
		url, inner = inner[1:closing], strings.TrimSpace(inner[closing+1:])
	} else if cut := strings.IndexAny(inner, " \t"); cut >= 0 {
		url, inner = inner[:cut], strings.TrimSpace(inner[cut+1:])
	} else {
		return inner, "", true
	}
	if inner == "" {
		return url, "", true
	}
	if len(inner) >= 2 {
		switch inner[0] {
		case '"', '\'':
			if inner[len(inner)-1] == inner[0] {
				return url, inner[1 : len(inner)-1], true
			}
		}
	}
	return "", "", false
}

// emphasis matches a run of n asterisks at i against the next run of exactly
// n asterisks that is preceded by a non-space, and returns the content
// between them.
func (in *inliner) emphasis(s string, i, n int) (string, int, bool) {
	open := i + n
	if open >= len(s) || s[open] == ' ' || s[open] == '\n' {
		return "", 0, false
	}
	for j := open; j < len(s); {
		if !in.spend(1) {
			return "", 0, false
		}
		switch s[j] {
		case '\\':
			j += 2
			continue
		case '`':
			m := runLen(s, j, '`')
			if _, e, found := in.codeSpan(s, j, m); found {
				j = e
				continue
			}
			j += m
			continue
		case '*':
			m := runLen(s, j, '*')
			prev := s[j-1]
			if m == n && j > open && prev != ' ' && prev != '\n' {
				return s[open:j], j + m, true
			}
			j += m
			continue
		}
		j++
	}
	return "", 0, false
}

func (in *inliner) writeLink(text, url, title string, depth int) {
	href, ok := safeURL(url)
	if !ok {
		// The anchor is dropped, the visible text stays.
		in.write(text, depth+1)
		return
	}
	in.b.WriteString("<a")
	writeAttr(in.b, "href", href)
	if title != "" {
		writeAttr(in.b, "title", title)
	}
	in.b.WriteString(">")
	in.write(text, depth+1)
	in.b.WriteString("</a>")
}

func (in *inliner) writeImage(alt, url, title string) {
	src, ok := safeURL(url)
	if !ok {
		writeText(in.b, alt)
		return
	}
	in.b.WriteString("<img")
	writeAttr(in.b, "src", src)
	writeAttr(in.b, "alt", alt)
	if title != "" {
		writeAttr(in.b, "title", title)
	}
	in.b.WriteString(" />")
}

// writeAttr writes one attribute. name is always a constant in this
// package; value is source text and goes through writeText, which escapes
// both quote characters as well as '<', '>' and '&'.
func writeAttr(b *strings.Builder, name, value string) {
	b.WriteString(" ")
	b.WriteString(name)
	b.WriteString("=\"")
	writeText(b, value)
	b.WriteString("\"")
}

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func isASCIIPunct(c byte) bool {
	switch {
	case '!' <= c && c <= '/':
		return true
	case ':' <= c && c <= '@':
		return true
	case '[' <= c && c <= '`':
		return true
	case '{' <= c && c <= '~':
		return true
	}
	return false
}

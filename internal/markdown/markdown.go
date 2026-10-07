// Package markdown renders a small, fixed subset of markdown to an HTML
// fragment, using nothing outside the standard library.
//
// Render is the only exported symbol, and there is no unsafe mode to turn
// on: every byte that comes from the source is escaped on its way out, and
// the only function that copies source bytes into the output is writeText.
// Every '<' in a rendered fragment is therefore a tag this package wrote.
// Those tags are h1-h6, p, pre, code, ul, ol, li, blockquote, hr, table,
// thead, tbody, tr, th, td, strong, em, a, img and br, and the only
// attributes are class, href, src, alt and title. No style attribute, no
// event handler, no script, no document wrapper - the output is a fragment
// a caller embeds.
//
// The subset is the one this project's task artifacts actually use, counted
// over all 173 of them in tasks/workspace-markdown/research.md: ATX
// headings, paragraphs, bullet and ordered lists with nesting, GFM tables,
// GFM task-list items, fenced code blocks, blockquotes, thematic breaks,
// hard line breaks, inline code, '*'-delimited emphasis, links and images.
// Deliberately absent, each on the evidence of that count: underscore
// emphasis (one real occurrence against pervasive snake_case), setext
// headings (none, while "---" is a thematic break 266 times), reference
// links, footnotes, autolinks, HTML entity decoding and raw HTML
// passthrough. All of those render as literal text, so a reader sees the
// source rather than broken markup.
package markdown

import "strings"

// maxBlockDepth bounds container nesting. At the cap the remaining lines are
// written as one escaped paragraph, so no input can grow the Go stack
// without bound.
const maxBlockDepth = 16

// Render turns markdown into an HTML fragment. The result is plain text as
// far as Go is concerned: wrapping it in template.HTML is the caller's
// decision and should happen in exactly one, documented place.
func Render(src string) string {
	lines := splitLines(normalize(src))
	var b strings.Builder
	b.Grow(len(src) + len(src)/2 + 64)
	renderBlocks(&b, lines, 0)
	return b.String()
}

func normalize(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// splitLines drops the empty trailing element a final newline produces, so
// that a file with and without a trailing newline render identically. Most
// artifacts in this repo have none.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// renderBlocks is the block loop. Every branch consumes at least one line -
// the loop enforces it - so it terminates on any input, and every container
// recurses with a strictly smaller slice.
func renderBlocks(b *strings.Builder, lines []string, depth int) {
	if depth > maxBlockDepth {
		writeFallback(b, lines)
		return
	}
	for i := 0; i < len(lines); {
		next := i
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			next = i + 1
		case isFence(line):
			next = writeFence(b, lines, i)
		case headingLevel(line) > 0:
			writeHeading(b, line)
			next = i + 1
		case isThematicBreak(line):
			b.WriteString("<hr />\n")
			next = i + 1
		case isBlockquote(line):
			next = writeBlockquote(b, lines, i, depth)
		case isTableStart(lines, i):
			next = writeTable(b, lines, i)
		case listMarkerAt(line) != nil:
			next = writeList(b, lines, i, depth)
		default:
			next = writeParagraph(b, lines, i)
		}
		if next <= i {
			// Unreachable by construction; the belt is here so that a
			// future block writer cannot turn a bug into a hang.
			next = i + 1
		}
		i = next
	}
}

// writeFallback renders lines as one escaped paragraph. It is what the depth
// cap degrades to.
func writeFallback(b *strings.Builder, lines []string) {
	if len(lines) == 0 {
		return
	}
	b.WriteString("<p>")
	writeText(b, strings.Join(lines, "\n"))
	b.WriteString("</p>\n")
}

// startsBlock reports whether lines[i] opens a block other than a
// paragraph. It is what stops paragraph gathering.
func startsBlock(lines []string, i int) bool {
	line := lines[i]
	return isFence(line) ||
		headingLevel(line) > 0 ||
		isThematicBreak(line) ||
		isBlockquote(line) ||
		listMarkerAt(line) != nil ||
		isTableStart(lines, i)
}

// writeParagraph gathers a run of lines into one paragraph. The lines are
// joined with newlines and handed to the inline pass as a whole, so a code
// span or an emphasis run may span a wrapped line - which matters in this
// repo, where prose wraps at about 76 columns. Leading indentation is
// dropped per line; trailing spaces are kept, because two of them are a
// hard break.
func writeParagraph(b *strings.Builder, lines []string, i int) int {
	j := i
	var content []string
	for j < len(lines) {
		if strings.TrimSpace(lines[j]) == "" {
			break
		}
		if j > i && startsBlock(lines, j) {
			break
		}
		content = append(content, strings.TrimLeft(lines[j], " \t"))
		j++
	}
	if n := len(content); n > 0 {
		// No line follows the last one, so its trailing spaces cannot be a
		// hard break.
		content[n-1] = strings.TrimRight(content[n-1], " \t")
	}
	b.WriteString("<p>")
	renderInline(b, strings.Join(content, "\n"))
	b.WriteString("</p>\n")
	return j
}

func headingLevel(line string) int {
	line = strings.TrimLeft(line, " ")
	n := runLen(line, 0, '#')
	if n < 1 || n > 6 {
		return 0
	}
	if len(line) == n || line[n] == ' ' {
		return n
	}
	return 0
}

func writeHeading(b *strings.Builder, line string) {
	level := headingLevel(line)
	text := strings.TrimLeft(line, " ")[level:]
	text = strings.TrimSpace(text)
	// A closing run of '#' is decoration, not content.
	if trimmed := strings.TrimRight(text, "#"); trimmed != text {
		if trimmed == "" || strings.HasSuffix(trimmed, " ") {
			text = strings.TrimSpace(trimmed)
		}
	}
	tag := [...]string{"", "h1", "h2", "h3", "h4", "h5", "h6"}[level]
	b.WriteString("<")
	b.WriteString(tag)
	b.WriteString(">")
	renderInline(b, text)
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">\n")
}

// isThematicBreak reports whether the line is three or more of '-', '*' or
// '_' and nothing else but spaces. "---" is always this and never a setext
// heading: no artifact in this repo uses setext, while "---" closes YAML
// frontmatter and separates sections throughout.
func isThematicBreak(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	n := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case c:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// isFence reports whether the line opens or closes a fenced code block.
func isFence(line string) bool {
	_, _, _, ok := fenceAt(line)
	return ok
}

func fenceAt(line string) (c byte, n int, info string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return 0, 0, "", false
	}
	c = trimmed[0]
	if c != '`' && c != '~' {
		return 0, 0, "", false
	}
	n = runLen(trimmed, 0, c)
	if n < 3 {
		return 0, 0, "", false
	}
	info = strings.TrimSpace(trimmed[n:])
	// An info string may not contain the fence character itself.
	if strings.IndexByte(info, c) >= 0 {
		return 0, 0, "", false
	}
	return c, n, info, true
}

// writeFence renders a fenced code block. End of input closes the block
// implicitly, which is what makes an unterminated fence terminate.
func writeFence(b *strings.Builder, lines []string, i int) int {
	c, n, info, _ := fenceAt(lines[i])
	indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " \t"))
	j := i + 1
	for j < len(lines) {
		if c2, n2, info2, ok := fenceAt(lines[j]); ok && c2 == c && n2 >= n && info2 == "" {
			break
		}
		j++
	}
	b.WriteString("<pre><code")
	if lang := codeLanguage(info); lang != "" {
		writeAttr(b, "class", "language-"+lang)
	}
	b.WriteString(">")
	for _, line := range lines[i+1 : j] {
		writeText(b, trimIndent(line, indent))
		b.WriteString("\n")
	}
	b.WriteString("</code></pre>\n")
	if j < len(lines) {
		return j + 1
	}
	return j
}

// codeLanguage returns the first word of an info string when it is usable as
// a class name suffix, and "" otherwise - so an exotic info string drops the
// class and keeps the body.
func codeLanguage(info string) string {
	word := info
	if cut := strings.IndexAny(word, " \t"); cut >= 0 {
		word = word[:cut]
	}
	if word == "" {
		return ""
	}
	for i := 0; i < len(word); i++ {
		c := word[i]
		if isASCIILetter(c) || isASCIIDigit(c) || c == '_' || c == '+' || c == '-' || c == '.' {
			continue
		}
		return ""
	}
	return word
}

func isBlockquote(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, ">")
}

func writeBlockquote(b *strings.Builder, lines []string, i, depth int) int {
	j := i
	var inner []string
	for j < len(lines) && isBlockquote(lines[j]) {
		line := strings.TrimLeft(lines[j], " \t")
		line = line[1:]
		inner = append(inner, strings.TrimPrefix(line, " "))
		j++
	}
	b.WriteString("<blockquote>\n")
	renderBlocks(b, inner, depth+1)
	b.WriteString("</blockquote>\n")
	return j
}

// listMarker describes a list item's opening marker.
type listMarker struct {
	indent  int // leading spaces
	width   int // bytes from the line start to the item's content
	ordered bool
}

func listMarkerAt(line string) *listMarker {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	rest := line[indent:]
	if rest == "" {
		return nil
	}
	switch rest[0] {
	case '-', '*', '+':
		if len(rest) > 1 && rest[1] == ' ' {
			return &listMarker{indent: indent, width: indent + 2, ordered: false}
		}
		return nil
	}
	digits := 0
	for digits < len(rest) && isASCIIDigit(rest[digits]) {
		digits++
	}
	if digits == 0 || digits > 9 || digits+1 >= len(rest) {
		return nil
	}
	if rest[digits] != '.' && rest[digits] != ')' {
		return nil
	}
	if rest[digits+1] != ' ' {
		return nil
	}
	return &listMarker{indent: indent, width: indent + digits + 2, ordered: true}
}

// writeList gathers one list - its items, their continuation lines and its
// looseness - and renders each item's content through renderBlocks, so a
// nested list, a fence or a blockquote inside an item works by recursion.
func writeList(b *strings.Builder, lines []string, i, depth int) int {
	first := listMarkerAt(lines[i])
	var items [][]string
	var cur []string
	loose, pendingBlank := false, false
	j := i
	for j < len(lines) {
		line := lines[j]
		if strings.TrimSpace(line) == "" {
			pendingBlank = true
			j++
			continue
		}
		if m := listMarkerAt(line); m != nil && m.indent == first.indent && m.ordered == first.ordered {
			if cur != nil {
				items = append(items, cur)
				if pendingBlank {
					loose = true
				}
			}
			cur = []string{line[m.width:]}
			pendingBlank = false
			j++
			continue
		}
		if leadingSpaces(line) > first.indent {
			if pendingBlank {
				cur = append(cur, "")
				loose = true
			}
			cur = append(cur, trimIndent(line, first.width))
			pendingBlank = false
			j++
			continue
		}
		break
	}
	if cur != nil {
		items = append(items, cur)
	}

	tag := "ul"
	if first.ordered {
		tag = "ol"
	}
	b.WriteString("<")
	b.WriteString(tag)
	b.WriteString(">\n")
	for _, item := range items {
		writeListItem(b, item, depth, loose)
	}
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">\n")
	return j
}

// writeListItem renders one item. Its leading paragraph is emitted bare in a
// tight list and wrapped in <p> in a loose one; anything after it goes back
// through the block loop.
func writeListItem(b *strings.Builder, item []string, depth int, loose bool) {
	open, marker := "<li>", ""
	if checked, rest, ok := taskMarker(item[0]); ok {
		open = "<li class=\"task-list-item\">"
		// Numeric entities keep this file ASCII; the browser draws a box.
		marker = "&#9744; "
		if checked {
			marker = "&#9745; "
		}
		item = append([]string{rest}, item[1:]...)
	}
	b.WriteString(open)
	b.WriteString(marker)

	k := 0
	for k < len(item) && strings.TrimSpace(item[k]) != "" && !startsBlock(item, k) {
		k++
	}
	if k > 0 {
		content := make([]string, k)
		for n := 0; n < k; n++ {
			content[n] = strings.TrimLeft(item[n], " \t")
		}
		if loose {
			b.WriteString("<p>")
		}
		renderInline(b, strings.Join(content, "\n"))
		if loose {
			b.WriteString("</p>")
		}
	}
	if k < len(item) {
		b.WriteString("\n")
		renderBlocks(b, item[k:], depth+1)
	}
	b.WriteString("</li>\n")
}

// taskMarker recognises a GFM task-list item. Supported because 33 of them
// exist in this repo's artifacts, all in acceptance-criteria lists; rendered
// as a glyph and a class, never as a form element.
func taskMarker(line string) (checked bool, rest string, ok bool) {
	if len(line) < 3 || line[0] != '[' || line[2] != ']' {
		return false, "", false
	}
	switch line[1] {
	case ' ':
	case 'x', 'X':
		checked = true
	default:
		return false, "", false
	}
	rest = line[3:]
	if rest != "" && rest[0] != ' ' {
		return false, "", false
	}
	return checked, strings.TrimPrefix(rest, " "), true
}

func leadingSpaces(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// trimIndent removes up to n leading spaces.
func trimIndent(line string, n int) string {
	i := 0
	for i < n && i < len(line) && line[i] == ' ' {
		i++
	}
	return line[i:]
}

// isTableStart reports whether lines[i] is a GFM table header: it holds a
// pipe and the next line is a delimiter row with the same number of cells.
func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") {
		return false
	}
	aligns, ok := tableAligns(lines[i+1])
	return ok && len(aligns) == len(splitRow(lines[i]))
}

// tableAligns parses a delimiter row into one alignment per cell.
func tableAligns(line string) ([]string, bool) {
	if !strings.Contains(line, "|") {
		return nil, false
	}
	cells := splitRow(line)
	if len(cells) == 0 {
		return nil, false
	}
	aligns := make([]string, len(cells))
	for i, cell := range cells {
		cell = strings.TrimSpace(cell)
		if len(cell) < 3 {
			return nil, false
		}
		left, right := cell[0] == ':', cell[len(cell)-1] == ':'
		body := strings.Trim(cell, ":")
		if body == "" || strings.Trim(body, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			aligns[i] = "align-center"
		case right:
			aligns[i] = "align-right"
		case left:
			aligns[i] = "align-left"
		}
	}
	return aligns, true
}

// splitRow splits a table row on unescaped pipes, dropping one optional
// leading and trailing pipe. Like GFM, it splits before inline parsing, so a
// pipe inside a code span splits the cell unless it is backslash-escaped.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if i+1 < len(line) && line[i+1] == '|' {
				cur.WriteByte('|')
				i++
				continue
			}
			cur.WriteByte('\\')
		case '|':
			cells = append(cells, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	cells = append(cells, cur.String())
	return cells
}

func writeTable(b *strings.Builder, lines []string, i int) int {
	header := splitRow(lines[i])
	aligns, _ := tableAligns(lines[i+1])

	b.WriteString("<table>\n<thead>\n")
	writeRow(b, header, aligns, "th")
	b.WriteString("</thead>\n")

	j := i + 2
	body := j
	for j < len(lines) && strings.TrimSpace(lines[j]) != "" && strings.Contains(lines[j], "|") {
		j++
	}
	if j > body {
		b.WriteString("<tbody>\n")
		for _, line := range lines[body:j] {
			writeRow(b, fitRow(splitRow(line), len(header)), aligns, "td")
		}
		b.WriteString("</tbody>\n")
	}
	b.WriteString("</table>\n")
	return j
}

// fitRow pads a short row and drops the surplus cells of a long one, so a
// ragged table still renders as a rectangle.
func fitRow(cells []string, n int) []string {
	for len(cells) < n {
		cells = append(cells, "")
	}
	return cells[:n]
}

func writeRow(b *strings.Builder, cells, aligns []string, cell string) {
	b.WriteString("<tr>\n")
	for i, text := range cells {
		b.WriteString("<")
		b.WriteString(cell)
		if i < len(aligns) && aligns[i] != "" {
			writeAttr(b, "class", aligns[i])
		}
		b.WriteString(">")
		renderInline(b, strings.TrimSpace(text))
		b.WriteString("</")
		b.WriteString(cell)
		b.WriteString(">\n")
	}
	b.WriteString("</tr>\n")
}

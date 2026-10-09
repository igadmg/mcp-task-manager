package web

import (
	"html/template"
	"strings"

	"github.com/gpayer/mcp-task-manager/internal/markdown"
)

// bodyView is a wide column's body: a task artifact or a task's description,
// shown in the working area. Exactly one field is set - HTML for a rendered
// document, Text for anything else - and _body.html is the one fragment that
// prints either.
//
// HTML is the only template.HTML in this package. The rule used to be
// absolute (see templates.go); it is narrowed here, to one field written by
// one function, because the whole point of the workspace is to read a task's
// research, design and plan as documents rather than as source.
//
// What makes the narrowing safe is the renderer's own contract, not this
// package's trust: internal/markdown escapes every byte that comes from the
// source, so every '<' in its output is a tag it wrote itself. Its tag and
// attribute set is fixed (no script, no style, no event handler, no id), its
// URL policy drops every scheme but http, https and mailto, and it has no
// unsafe mode to turn on. TestRenderHasNoUnsafeMode and
// TestNoForbiddenConstructsInOutput hold that side; TestOneTrustPoint holds
// this one.
type bodyView struct {
	HTML template.HTML
	Text string
}

// newBodyView renders one body. name decides how: a document goes through the
// markdown renderer, everything else is shown as it is. It is the only
// template.HTML wrap in internal/web.
func newBodyView(name, src string) bodyView {
	if rendersAsMarkdown(name) {
		return bodyView{HTML: template.HTML(markdown.Render(src))}
	}
	return bodyView{Text: src}
}

// rendersAsMarkdown reports whether a name is a markdown document: no
// extension at all, or ".md".
//
// Extensionless counts because the older artifacts in this project's own
// backlog are named "research", "design" and "plan" with no suffix - 24 of
// them against 176 ".md" files, and nothing else but the server's own
// "*.phase" records. A dotfile (".notes") has its only dot at index 0, which
// is a leading dot and not an extension, so it reads as a document too; a
// double extension ("notes.en.md") ends in ".md" and needs no special case.
//
// An empty name is a document: that is how the description column asks for
// one, since a description has no file name at all.
func rendersAsMarkdown(name string) bool {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return true
	}
	return strings.EqualFold(name[i+1:], "md")
}

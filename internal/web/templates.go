package web

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// There is one page set: board.html plus the shared layout. Every state the
// dashboard serves - the bare board, a task's panel, a workspace chain - is
// that one page, so a URL never switches surface. Fragments are parsed on
// their own for the htmx swap targets.
//
// The template functions are rebound on immutable clones. nav prefixes a
// session route with its token and the external mount; root prefixes a global
// route with only that mount; asset prefixes the content-hashed global asset
// URL. These prototype entries exist for parsing and for unmounted sessions.
// Handlers own mounted root clones and cache mounted session clones (see
// sessiontpl.go and handlers.go).
//
// Every other field is a plain string and is rendered with {{ . }}, so
// html/template escapes all task text. There is deliberately no safeHTML
// helper: a FuncMap entry would be reachable from every template in every
// set, so any fragment could trust any string.
//
// There is exactly one template.HTML in this package, bodyView.HTML, written
// only by newBodyView (body.go) and printed only by _body.html: a task's
// research, design and plan are documents, and the workspace exists to read
// them as documents. What it wraps is internal/markdown's output, which
// escapes every source byte and emits a fixed tag set with no script, style,
// event handler or id. TestOneTrustPoint fails if a second one appears.
// The sets below are prototypes and are never executed: html/template
// refuses to Clone a set that has run, and every set here is cloned - once
// per session, and once for the root pages. Render through rootTpl or a
// session's own clones, never through these.
var (
	funcs       = template.FuncMap{"asset": assetURL, "nav": navFor(""), "root": navFor("")}
	fragments   = template.Must(template.New("fragments").Funcs(funcs).ParseFS(templateFS, "templates/_*.html"))
	boardPage   = mustPage("board.html")
	welcomePage = mustPage("welcome.html")
	gonePage    = mustPage("gone.html")

	// rootTpl serves the pages that live outside any session: the workspace
	// list and the unknown-token page. Their links are already
	// root-relative, so the same mechanism covers them with no special case.
	rootTpl = newSessionTemplates("")
)

func mustPage(name string) *template.Template {
	return template.Must(template.New(name).Funcs(funcs).ParseFS(templateFS,
		"templates/layout.html",
		"templates/"+name,
		"templates/_*.html",
	))
}

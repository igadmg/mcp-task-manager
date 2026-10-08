package web

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFS embed.FS

// The two page sets each pair the shared layout with one page template, so
// both can define "content" without colliding. Fragments are parsed on their
// own for the htmx swap targets.
//
// There are two funcMap entries. asset maps an embedded file name to its
// content-hashed URL and is the same for every session, because /static/ is
// one global URL space. nav prefixes a session route with the session's
// token; the entry here is bound to the root prefix, which is what the
// welcome and unknown-token pages need, and each session rebinds it on its
// own clones (see sessiontpl.go). Parsing needs the name to exist, which is
// the other reason it is in this map.
//
// Every other field is already a plain string and is rendered with {{ . }},
// so html/template escapes all task text. There is deliberately no safeHTML
// helper and no template.HTML anywhere in this package - task titles and
// descriptions are user-controlled.
// The sets below are prototypes and are never executed: html/template
// refuses to Clone a set that has run, and every set here is cloned - once
// per session, and once for the root pages. Render through rootTpl or a
// session's own clones, never through these.
var (
	funcs       = template.FuncMap{"asset": assetURL, "nav": navFor("")}
	fragments   = template.Must(template.New("fragments").Funcs(funcs).ParseFS(templateFS, "templates/_*.html"))
	boardPage   = mustPage("board.html")
	detailPage  = mustPage("detail.html")
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

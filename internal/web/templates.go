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
// Nothing here registers a funcMap: every field is already a plain string and
// is rendered with {{ . }}, so html/template escapes all task text. There is
// deliberately no safeHTML helper and no template.HTML anywhere in this
// package - task titles and descriptions are user-controlled.
var (
	fragments  = template.Must(template.ParseFS(templateFS, "templates/_*.html"))
	boardPage  = mustPage("board.html")
	detailPage = mustPage("detail.html")
)

func mustPage(name string) *template.Template {
	return template.Must(template.ParseFS(templateFS,
		"templates/layout.html",
		"templates/"+name,
		"templates/_*.html",
	))
}

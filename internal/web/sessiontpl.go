package web

import (
	"fmt"
	"html/template"
)

// sessionTemplates are one session's template sets, with nav bound to that
// session's URL prefix.
//
// The package's own sets are built at init, so a FuncMap entry there cannot
// close over a session. Each session therefore clones them once, at
// registration - never per request: the prefix is fixed for a session's
// whole life, and a clone on every htmx poll would be work for nothing.
//
// The alternative, a Base field on the view models, was rejected: _card.html
// is executed with a CardView, so the prefix would have to be copied onto
// four structs, and a forgotten one renders a link that looks right and
// 404s as an unknown token when clicked.
type sessionTemplates struct {
	board     *template.Template
	fragments *template.Template
	welcome   *template.Template
	gone      *template.Template
}

func newSessionTemplates(base string) sessionTemplates {
	return newMountedTemplates(base, "")
}

func newMountedTemplates(base, mount string) sessionTemplates {
	funcs := template.FuncMap{
		"nav":   navFor(mount + base),
		"root":  navFor(mount),
		"asset": func(name string) string { return mount + assetURL(name) },
	}
	return sessionTemplates{
		board:     mustClone(boardPage, funcs),
		fragments: mustClone(fragments, funcs),
		welcome:   mustClone(welcomePage, funcs),
		gone:      mustClone(gonePage, funcs),
	}
}

func mustClone(t *template.Template, funcs template.FuncMap) *template.Template {
	clone, err := t.Clone()
	if err != nil {
		panic("web: cannot clone " + t.Name() + ": " + err.Error())
	}
	return clone.Funcs(funcs)
}

// navFor returns the nav template func for a URL prefix. nav joins its parts
// onto that prefix, so no template ever writes a token by hand:
//
//	{{ nav "/" }}              -> /<token>/
//	{{ nav "/board" }}         -> /<token>/board
//	{{ nav "/tasks/" .ID }}    -> /<token>/tasks/42
//
// The result is a plain string; html/template escapes it in the attribute
// context it lands in, exactly as the raw paths it replaces were escaped.
func navFor(base string) func(...any) string {
	return func(parts ...any) string {
		return base + fmt.Sprint(parts...)
	}
}

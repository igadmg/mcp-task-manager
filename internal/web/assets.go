package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// staticFS lists the assets individually rather than static/*, so a stray
// source map or editor backup can never be shipped inside the binary.
//
//go:embed static/app.css static/htmx.min.js static/app.js
var staticFS embed.FS

// staticHandler serves the embedded CSS and JS. They are part of the binary,
// so they can be cached forever - a new build is a new binary.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("web: embedded static assets missing: " + err.Error())
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

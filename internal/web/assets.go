package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
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

// assetURL is the URL a page links an embedded asset by. The handler marks
// assets immutable for a year, so the URL carries a content hash: a changed
// asset is a new URL and an already-open browser never keeps a stale app.css.
func assetURL(name string) string {
	data, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		panic("web: embedded static asset missing: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return "/static/" + name + "?v=" + hex.EncodeToString(sum[:4])
}

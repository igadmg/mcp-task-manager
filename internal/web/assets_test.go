package web

import (
	"strings"
	"testing"
)

// TestAppCSSDefinesLaneClasses ties the Go lane list to the compiled CSS:
// a forgotten scripts/build-css.sh run, or a lane without its
// .lane-<phase> rule, fails here first. The strings are the minified forms
// Tailwind v4.3.3 emits; it rewrites the 14rem collapse rule's
// "width < 14rem" as "not (min-width:14rem)".
func TestAppCSSDefinesLaneClasses(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("read embedded app.css: %v", err)
	}
	css := string(data)
	want := []string{".lanes{", "container:lanes/inline-size", ".lane{", "lanes not (min-width:14rem)"}
	for _, l := range phaseLanes {
		want = append(want, ".lane-"+string(l.Phase)+"{")
	}
	for _, w := range want {
		if !strings.Contains(css, w) {
			t.Errorf("app.css lacks %q - rerun scripts/build-css.sh", w)
		}
	}
}

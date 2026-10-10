package web

import (
	"net/http"
	"strings"
	"testing"
)

// TestSessionsJSContract pins the browser-side safety model without a JS
// runtime: the live feed is a native EventSource that resumes with
// Last-Event-ID, closes on terminal statuses, and inserts every event string
// through textContent - never innerHTML.
func TestSessionsJSContract(t *testing.T) {
	data, err := staticFS.ReadFile("static/sessions.js")
	if err != nil {
		t.Fatalf("read embedded sessions.js: %v", err)
	}
	js := string(data)
	for _, want := range []string{
		"EventSource", "Last-Event-ID", "textContent", "X-Dashboard-CSRF",
		"application/json", "source.close()", "data-request-id", "multi_select",
		// radios share one group name per question; pending panels are
		// matched by exact getAttribute comparison (request ids are
		// untrusted); event input is re-stringified, never String(obj).
		`input.name = "q-"`, `getAttribute("data-request-id")`, "formatJSON",
		"session-conn", "Please answer every question",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("sessions.js lacks %s", want)
		}
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "document.write", "eval(", `[data-request-id="' +`} {
		if strings.Contains(js, bad) {
			t.Errorf("sessions.js uses %s: event text must never become HTML", bad)
		}
	}
}

// TestSessionsCSSDefinesClasses ties the hand-written sessions.css to the
// markup both the templates and sessions.js emit. sessions.css is deliberately
// NOT part of the compiled Tailwind bundle (design §8: no rebuild needed).
func TestSessionsCSSDefinesClasses(t *testing.T) {
	data, err := staticFS.ReadFile("static/sessions.css")
	if err != nil {
		t.Fatalf("read embedded sessions.css: %v", err)
	}
	css := string(data)
	// Grouped selectors list the class followed by a comma rather than the
	// opening brace, so accept both forms (like the app.css tests do).
	defines := func(class string) bool {
		return strings.Contains(css, "."+class+"{") || strings.Contains(css, "."+class+",")
	}
	for _, want := range []string{
		"sess-feed", "sess-line", "sess-msg-user", "sess-msg-assistant",
		"sess-msg-error", "sess-pre", "sess-pending", "sess-pending-title",
		"sess-question", "sess-option",
		"sess-status-waiting_answer", "sess-status-running", "sess-status-finished",
	} {
		if !defines(want) {
			t.Errorf("sessions.css lacks .%s", want)
		}
	}
}

// The session pages load their own script; the asset URL must be the
// content-hashed one served from /static/.
func TestSessionsAssetsAreServed(t *testing.T) {
	h, _, _ := twoSessions(t)
	for _, name := range []string{"sessions.js", "sessions.css"} {
		rec := getRaw(t, h, "/static/"+name)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /static/%s = %d, want 200", name, rec.Code)
		}
	}
}

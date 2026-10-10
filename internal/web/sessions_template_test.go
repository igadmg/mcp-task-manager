package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
	"github.com/gpayer/mcp-task-manager/internal/testsupport"
)

// The session page hands the CSRF token out in a <meta> and renders pending
// requests server-side so a reload restores them.
func TestSessionPageRendersPendingAndCSRF(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "waiting_answer")
	f.mu.Lock()
	f.pending["own-1"] = []sessionapi.Event{{
		Kind: sessionapi.EventQuestion, RequestID: "req-1",
		Questions: []sessionapi.Question{{Question: "Pick one?", Options: []sessionapi.Option{
			{Label: "alpha", Description: "the first"}, {Label: "beta"},
		}}},
	}}
	f.mu.Unlock()

	rec := getRaw(t, h, sess.Base()+"/sessions/own-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the session page = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<meta name="csrf-token" content="`+testCSRF+`">`) {
		t.Error("the session page does not carry the CSRF meta tag")
	}
	if !strings.Contains(body, `data-request-id="req-1"`) {
		t.Error("a pending request is not rendered server-side (reload would lose it)")
	}
	if !strings.Contains(body, `id="session-conn"`) {
		t.Error("the session page has no connection-status element")
	}
	for _, want := range []string{`data-events-url="` + sess.Base() + `/sessions/own-1/events"`,
		`data-messages-url="` + sess.Base() + `/sessions/own-1/messages"`,
		`data-answers-url="` + sess.Base() + `/sessions/own-1/answers"`,
		`data-stop-url="` + sess.Base() + `/sessions/own-1/stop"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the session page is missing %q", want)
		}
	}
}

// Event text is untrusted (model output, tool results): it must arrive
// HTML-escaped from the server render.
func TestSessionPageEscapesPendingText(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)

	const evil = `<script>alert("x")</script>`
	f.setMeta("own-1", root, "waiting_answer")
	f.mu.Lock()
	f.pending["own-1"] = []sessionapi.Event{{
		Kind: sessionapi.EventQuestion, RequestID: "req-9",
		Questions: []sessionapi.Question{{Question: evil, Options: []sessionapi.Option{{Label: evil}}}},
	}}
	f.mu.Unlock()

	rec := getRaw(t, h, sess.Base()+"/sessions/own-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the session page = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Error("unescaped script text reached the session page")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("the escaped text is not on the page either - where did the question go?")
	}
}

// The list page must offer the start form only through the JSON+CSRF path,
// and must not leak the host token anywhere (it never reaches the browser).
func TestSessionsPageHasStartFormAndNoTokenLeak(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)

	rec := getRaw(t, h, sess.Base()+"/sessions")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the sessions page = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="session-start-form"`) {
		t.Error("the sessions page has no start form")
	}
	if !strings.Contains(body, `data-start-url="`+sess.Base()+`/sessions"`) {
		t.Error("the start form is not bound to the session route")
	}
	if strings.Contains(body, f.token) {
		t.Error("the host bearer token leaked into the page")
	}
}

// The header link into the session list appears only when the surface is
// enabled - on every page of the session, board included.
func TestHeaderSessionsLinkFollowsTheOptIn(t *testing.T) {
	rs, _, _ := testsupport.NewBacklog(t)
	sessions := newTestSessions(t)
	sess := adoptBacklog(t, sessions, rs)

	off := NewHandler(Deps{Sessions: sessions, Logger: discardLogger()})
	if body := getRaw(t, off, sess.Base()+"/").Body.String(); strings.Contains(body, `href="`+sess.Base()+`/sessions"`) {
		t.Error("the board links sessions while the surface is disabled")
	}

	f := newFakeSessionHost(t)
	bed, bedSess := sessionsTestBed(t, f)
	if body := getRaw(t, bed, bedSess.Base()+"/").Body.String(); !strings.Contains(body, `href="`+bedSess.Base()+`/sessions"`) {
		t.Error("the board does not link sessions while the surface is enabled")
	}
}

// Single-select options of one question must share one radio group name
// (otherwise every option stays checked), while distinct questions and
// distinct requests stay distinct; multi-select stays checkbox. This is the
// server-rendered copy, i.e. the reload case; sessions.js builds the same
// names for the live-event case.
func TestSessionPageRadioGroupsShareOneNamePerQuestion(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "waiting_answer")
	f.mu.Lock()
	f.pending["own-1"] = []sessionapi.Event{{
		Kind: sessionapi.EventQuestion, RequestID: "req-1",
		Questions: []sessionapi.Question{
			{Question: "First?", Options: []sessionapi.Option{{Label: "a1"}, {Label: "a2"}}},
			{Question: "Second?", Options: []sessionapi.Option{{Label: "b1"}, {Label: "b2"}}},
			{Question: "Many?", MultiSelect: true, Options: []sessionapi.Option{{Label: "m1"}, {Label: "m2"}}},
		},
	}, {
		Kind: sessionapi.EventQuestion, RequestID: "req-2",
		Questions: []sessionapi.Question{
			{Question: "Other request?", Options: []sessionapi.Option{{Label: "x1"}, {Label: "x2"}}},
		},
	}}
	f.mu.Unlock()

	rec := getRaw(t, h, sess.Base()+"/sessions/own-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the session page = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	count := func(pattern string) int {
		return len(regexp.MustCompile(pattern).FindAllString(body, -1))
	}
	// Both options of each single-select question share the group name.
	for _, name := range []string{"q-req-1-0", "q-req-1-1", "q-req-2-0"} {
		if n := count(`name="` + name + `"`); n != 2 {
			t.Errorf("radio group %q appears %d times, want 2 (one per option)", name, n)
		}
	}
	// The multi-select question keeps checkboxes under its own group.
	if n := count(`type="checkbox" name="q-req-1-2"`); n != 2 {
		t.Errorf("checkbox group %q appears %d times, want 2", "q-req-1-2", n)
	}
	// No question reuses another's name, and no per-option name survives.
	if strings.Contains(body, `name="opt-`) {
		t.Error("a per-option radio name survived; options are not grouped per question")
	}
}

// A permission's raw JSON input must reach the page as indented JSON text,
// HTML-escaped - never as a Go byte-array dump ([123 34 ...) and never raw.
func TestSessionPagePermissionInputIsFormattedAndEscaped(t *testing.T) {
	f := newFakeSessionHost(t)
	h, sess := sessionsTestBed(t, f)
	root := workspaceRootOf(t, sess)
	f.setMeta("own-1", root, "waiting_answer")
	f.mu.Lock()
	f.pending["own-1"] = []sessionapi.Event{{
		Kind: sessionapi.EventPermission, RequestID: "req-p",
		ToolName: "Bash",
		Input:    json.RawMessage(`{"cmd":"rm -rf <script>alert(1)</script>","args":["-a"]}`),
	}}
	f.mu.Unlock()

	rec := getRaw(t, h, sess.Base()+"/sessions/own-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the session page = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	if strings.Contains(body, "[123 34") {
		t.Error("the raw JSON input rendered as a Go byte array")
	}
	// html/template escapes quotes as &#34; and < as u003c; either way the
	// markup cannot execute.
	for _, want := range []string{`&#34;cmd&#34;: &#34;rm -rf`, `\u003cscript\u003e`, `&#34;args&#34;: [`} {
		if !strings.Contains(body, want) {
			t.Errorf("the formatted, escaped input lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("unescaped script markup reached the session page")
	}
}

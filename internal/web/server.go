package web

import (
	"log"
	"net/http"
	"os"
	"time"
)

// DefaultPollSeconds is how often one board refreshes itself. Each poll takes
// that session's service lock and can trigger an index rebuild when its tasks
// directory changed, so this is deliberately not one second - and the cost
// multiplies by the number of open boards, because every one of them polls.
const DefaultPollSeconds = 5

// Deps is everything the web module needs from the rest of the process.
type Deps struct {
	// BasePath is the trusted external mount path. The proxy strips it before
	// forwarding requests; internal routes remain rooted at /. Set a canonical
	// value from NormalizeBasePath at the composition boundary, never from headers.
	BasePath string
	// Sessions is the workspace registry. Handlers only ever Lookup() in
	// it: a session's project was built by whoever registered it - the MCP
	// resolver, or the one POST on the registry - never by a request.
	//
	// That is the structural form of the read-only rule. Resolution runs
	// Service.Initialize(), which migrates the legacy layout and may
	// auto-archive, and even the POST cannot reach it: Sessions.Pick opens
	// a workspace through project.BuildReadOnly, which does neither.
	Sessions *Sessions
	// Logger receives request and render errors. It must never write to
	// stdout: in MCP stdio mode stdout is the JSON-RPC channel.
	Logger *log.Logger
	// Now is injectable so tests get deterministic "x ago" strings.
	Now func() time.Time
	// PollSeconds overrides DefaultPollSeconds.
	PollSeconds int
	// PrimaryTasksDir is the backlog this process was started for, as an
	// absolute path. It is reported on /healthz in the X-Task-Dashboard
	// header, which is how a process that wants to start a dashboard can
	// tell a running one of ours from anything else holding the port - a
	// bare "ok" cannot, and a PID file answers a different question (see
	// internal/webproc).
	//
	// A dashboard can serve many workspaces, but it was started for
	// exactly one, and that is the one a spawner asks about. Empty when
	// nothing was resolved, which is honest rather than misleading: the
	// header then claims no backlog.
	PrimaryTasksDir string
}

// HealthHeader names the backlog a dashboard was started for. It is the one
// thing that makes GET /healthz identifying rather than merely affirmative.
const HealthHeader = "X-Task-Dashboard"

func (d Deps) withDefaults() Deps {
	if d.Sessions == nil {
		d.Sessions = NewSessions(SessionsConfig{Logger: d.Logger})
	}
	if d.Logger == nil {
		d.Logger = log.New(os.Stderr, "web: ", log.LstdFlags)
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.PollSeconds <= 0 {
		d.PollSeconds = DefaultPollSeconds
	}
	return d
}

// NewHandler builds the dashboard's route table.
//
// Every URL that shows task data starts with a session token, and the whole
// read-only guarantee is one sentence: task data is reachable by GET only,
// and the single POST in this server is the session registry, which touches
// no task data at all. Only GET patterns are registered in the session mux,
// so ServeMux answers every other method there with 405 by itself and no
// handler below can reach a mutating Service method.
//
// /static/ and /healthz stay outside the token space: the embedded assets
// are identical for every session, so they get one URL space and one browser
// cache. Both names are reserved against the token generator
// (reservedSegments).
//
// # Why the session routes are a nested mux
//
// This is a correctness constraint, not a style choice. ServeMux panics at
// registration when two patterns overlap and neither is more specific, and
// a wildcard first segment overlaps the static routes in exactly that way:
// "GET /static/{file}" and "GET /{token}/board" both match /static/board,
// and neither dominates - the first segment favours one, the second the
// other. Flat token patterns are therefore a startup panic, whatever shape
// the static pattern takes.
//
// A subtree pattern has no such problem: "/{token}/" and "/static/{file...}"
// differ in one segment where one is a literal and the other a wildcard, and
// every path the static pattern matches is also matched by the subtree, so
// ServeMux ranks it more specific and routes it first. The session patterns
// then live in their own mux, where the only thing they can collide with is
// each other.
//
// Registering the subtree without a method is deliberate: a POST to
// /<token>/board has to reach the session mux to be answered with 405. A
// "GET /{token}/" pattern would answer 404 instead, and the structural
// read-only claim would quietly weaken to a convention.
func NewHandler(d Deps) http.Handler {
	h := &handler{Deps: d.withDefaults(), templates: make(map[string]sessionTemplates)}
	h.rootTpl = newMountedTemplates("", h.BasePath)

	sessions := http.NewServeMux()
	sessions.HandleFunc("GET /{token}/{$}", h.board)
	sessions.HandleFunc("GET /{token}/board", h.boardFragment)
	sessions.HandleFunc("GET /{token}/tasks/{id}", h.detail)
	sessions.HandleFunc("GET /{token}/tasks/{id}/panel", h.detailPanel)
	sessions.HandleFunc("GET /{token}/tasks/{id}/files/{name}", h.taskFile)
	// The literal "description" segment overlaps neither /panel, /files/{name}
	// nor /w/{rest...}. GET only, so a POST is answered 405 by the mux.
	sessions.HandleFunc("GET /{token}/tasks/{id}/description/{view}", h.descriptionFragment)
	// One pattern per family: registering "GET /{token}/tasks/{id}/w/"
	// beside the wildcard panics, because it matches the same requests.
	sessions.HandleFunc("GET /{token}/tasks/{id}/w/{rest...}", h.workspace)
	// The one workspace state a chain cannot name: the backlog graph with
	// no task highlighted, opened from the board. A chain always starts
	// /tasks/{root} (chain.go), and from the board there is no task to
	// root at. Its fragment needs no pattern of its own - the strip
	// wildcard below carries it, as it already carries "/" for the bare
	// board.
	sessions.HandleFunc("GET /{token}/graph", h.graph)
	sessions.HandleFunc("GET /{token}/strip/{rest...}", h.workspaceFragment)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.welcome)
	mux.HandleFunc("POST /sessions", h.createSession)
	mux.Handle("GET /static/{file...}", http.StripPrefix("/static/", staticHandler()))
	mux.HandleFunc("GET /healthz", h.health)
	mux.Handle("/{token}/", sessions)
	if h.BasePath == "" {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(&mountRedirectWriter{ResponseWriter: w, base: h.BasePath}, r)
	})
}

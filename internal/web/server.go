package web

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/project"
)

// DefaultPollSeconds is how often the board refreshes itself. Each poll takes
// the service lock and can trigger an index rebuild when the tasks directory
// changed, so this is deliberately not one second.
const DefaultPollSeconds = 5

// Deps is everything the web module needs from the rest of the process.
type Deps struct {
	// Project reads the current project WITHOUT resolving it. Resolution
	// runs Service.Initialize(), which migrates the layout and may
	// auto-archive; a plain GET must never be able to move files on disk.
	// Wire this to Resolver.Current, never to Resolver.Get.
	Project func() (*project.Resolved, bool)
	// Logger receives request and render errors. It must never write to
	// stdout: in MCP stdio mode stdout is the JSON-RPC channel.
	Logger *log.Logger
	// Now is injectable so tests get deterministic "x ago" strings.
	Now func() time.Time
	// PollSeconds overrides DefaultPollSeconds.
	PollSeconds int
}

func (d Deps) withDefaults() Deps {
	if d.Project == nil {
		d.Project = func() (*project.Resolved, bool) { return nil, false }
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
// Read-only is structural, not a convention: only GET patterns are ever
// registered, so ServeMux answers every other method with 405 by itself, and
// no handler below can reach a mutating Service method.
func NewHandler(d Deps) http.Handler {
	h := &handler{Deps: d.withDefaults()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.board)
	mux.HandleFunc("GET /board", h.boardFragment)
	mux.HandleFunc("GET /tasks/{id}", h.detail)
	mux.HandleFunc("GET /tasks/{id}/panel", h.detailPanel)
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler()))
	mux.HandleFunc("GET /healthz", h.health)
	return mux
}

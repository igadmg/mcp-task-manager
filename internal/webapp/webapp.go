// Package webapp is the dashboard's composition root: it resolves the project
// read-only, builds the session registry and serves HTTP, in a process of its
// own. It is the second of this repository's two applications - internal/app
// is the first - and the only one that imports internal/web.
//
// Everything here follows from one rule: this process never writes to the
// backlog. The MCP process owns writes; this one reads whatever is on disk.
package webapp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/config"
	"github.com/gpayer/mcp-task-manager/internal/project"
	"github.com/gpayer/mcp-task-manager/internal/web"
	"github.com/gpayer/mcp-task-manager/internal/webproc"
)

// shutdownGrace is how long the listener gets to drain once the process is
// stopping.
const shutdownGrace = 5 * time.Second

// Options are what the binary was started with.
type Options struct {
	// Addr overrides the configured listen address.
	Addr string
	// Stderr is the log sink. Never stdout: this process may be started by
	// an MCP server whose stdout is a JSON-RPC channel, and inheriting a
	// pipe that then closes is how a detached child dies.
	Stderr io.Writer
}

func (o Options) logger() *log.Logger {
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	return log.New(o.Stderr, "web: ", log.LstdFlags)
}

// Run serves the dashboard in the foreground until ctx is done.
//
// It resolves eagerly, unlike the MCP side: an unresolvable project or an
// occupied port is a startup error the operator sees immediately rather than
// a surprise on the first request. And it resolves through
// project.BuildReadOnly, which is this task's whole write-ownership decision
// in one line - no legacy-layout migration, no auto-archive, no git handle.
// A backlog still in the flat layout is rendered as the read-only service
// sees it; the MCP process migrates it when it next starts.
func Run(ctx context.Context, opts Options) error {
	basePath, err := web.NormalizeBasePath(os.Getenv("MCP_WEB_BASE_PATH"))
	if err != nil {
		return err
	}
	logger := opts.logger()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	resolved, err := project.BuildReadOnly(cfg)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	addr := opts.Addr
	if addr == "" {
		addr = cfg.Web.Addr
	}
	tasksDir := cfg.TasksDir()

	sessions := NewSessions(logger)
	controller := web.NewController(web.Deps{
		Sessions:        sessions,
		Logger:          logger,
		PrimaryTasksDir: tasksDir,
		BasePath:        basePath,
	}, addr)

	// Publish the project before the listener accepts anything, so the very
	// first request already finds its board.
	sess, err := sessions.Adopt(resolved)
	if err != nil {
		return fmt.Errorf("publish the project on the dashboard: %w", err)
	}

	url, _, err := controller.Start(addr)
	if err != nil {
		return fmt.Errorf("start dashboard: %w", err)
	}
	logger.Printf("%s", resolved.Resolution().Explain())
	logger.Printf("dashboard: %s%s%s/", url, basePath, sess.Base())
	logger.Printf("workspaces: %s%s/", url, basePath)

	// Record the instance so the MCP side can find it - this process owns
	// the marker's whole lifetime, and nothing else ever deletes it.
	user := resolved.Service.UserName()
	marker := webproc.Marker{
		Pid:  os.Getpid(),
		Addr: listenAddr(url),
		Base: sess.Base(),
	}
	if err := webproc.WriteMarker(tasksDir, user, marker); err != nil {
		// Not fatal: the dashboard serves fine, the MCP side just cannot
		// find it without probing the configured address.
		logger.Printf("could not record this instance: %v", err)
	} else {
		defer func() {
			if err := webproc.RemoveMarker(tasksDir, user); err != nil {
				logger.Printf("could not clear this instance's marker: %v", err)
			}
		}()
	}

	<-ctx.Done()
	shutdown(controller)
	return nil
}

// listenAddr strips the scheme off the controller's URL, which is what the
// marker and the probe speak in.
func listenAddr(url string) string {
	const prefix = "http://"
	if len(url) > len(prefix) && url[:len(prefix)] == prefix {
		return url[len(prefix):]
	}
	return url
}

func shutdown(c *web.Controller) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = c.Shutdown(ctx)
}

// NewSessions builds the dashboard's workspace registry.
//
// The workspace list is the web server's own config file, not a project's: it
// is cross-project, so it must not depend on which project this process was
// started from. A failure to read it is logged and not fatal - the list is an
// extra, and a typo in a personal config file must not stop the dashboard
// serving the backlog it was started for.
func NewSessions(logger *log.Logger) *web.Sessions {
	wf, err := config.LoadWebFile()
	if err != nil {
		logger.Printf("could not read the workspace list: %v", err)
		wf = &config.WebFile{}
	}
	return web.NewSessions(web.SessionsConfig{
		Workspaces: wf.Workspaces,
		ConfigPath: wf.Path,
		Problems:   wf.Problems,
		Logger:     logger,
		Open:       openWorkspace,
	})
}

// openWorkspace is the one place a dashboard-picked workspace becomes a
// project. It goes through BuildReadOnly for the same reason this whole
// process does: opening a backlog nobody asked to write must not migrate its
// layout or auto-archive it.
func openWorkspace(ws config.Workspace) (*project.Resolved, error) {
	cfg, err := config.LoadForRoot(ws.Path, ws.TasksDir)
	if err != nil {
		return nil, err
	}
	return project.BuildReadOnly(cfg)
}

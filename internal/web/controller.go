package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"
)

// Controller owns the dashboard's listener and HTTP server.
//
// It is what makes "start the dashboard" safe to call more than once: the MCP
// tool, the config flag and the CLI entry point can all ask for it, and only
// the first one binds a port.
type Controller struct {
	deps        Deps
	defaultAddr string

	mu   sync.Mutex
	ln   net.Listener
	srv  *http.Server
	url  string
	done chan struct{}
}

// NewController prepares the dashboard without binding anything.
func NewController(d Deps, defaultAddr string) *Controller {
	d = d.withDefaults()
	if defaultAddr == "" {
		defaultAddr = "127.0.0.1:0"
	}
	return &Controller{deps: d, defaultAddr: defaultAddr}
}

// Start binds the listener and serves in the background. An empty addr uses
// the configured default. Calling it again while the server runs reports the
// running URL with already=true; it never rebinds and never errors for that.
func (c *Controller) Start(addr string) (url string, already bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ln != nil {
		return c.url, true, nil
	}
	if addr == "" {
		addr = c.defaultAddr
	}

	// Bind synchronously: a port conflict must surface as a startup error,
	// and :0 has to yield a real URL before this returns.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", false, err
	}

	c.ln = ln
	c.url = "http://" + ln.Addr().String()
	c.done = make(chan struct{})
	c.srv = &http.Server{
		Handler:           NewHandler(c.deps),
		ErrorLog:          c.deps.Logger,
		ReadHeaderTimeout: 10 * time.Second,
	}

	srv, done := c.srv, c.done
	go func() {
		defer close(done)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.deps.Logger.Printf("server stopped: %v", err)
		}
	}()

	c.deps.Logger.Printf("task dashboard on %s (read-only)", c.url)
	return c.url, false, nil
}

// URL reports where the dashboard is listening, if it is.
func (c *Controller) URL() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.url, c.ln != nil
}

// SessionPath reports the URL path of the session serving tasksDir, so a
// caller holding the listener's base URL can report a usable link. It works
// whether or not the listener is up: the registry is independent of it.
func (c *Controller) SessionPath(tasksDir string) (string, bool) {
	return c.deps.Sessions.PathFor(tasksDir)
}

// Shutdown stops the listener and waits for in-flight requests. Idempotent:
// shutting down a controller that never started, or already stopped, is fine.
func (c *Controller) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	srv, done := c.srv, c.done
	c.srv, c.ln, c.url, c.done = nil, nil, "", nil
	c.mu.Unlock()

	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	select {
	case <-done:
	case <-ctx.Done():
	}
	return err
}

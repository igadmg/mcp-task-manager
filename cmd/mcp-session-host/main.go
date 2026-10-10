// Command mcp-session-host runs the interactive session host: it owns the
// claude processes, the event journals, and the loopback+bearer HTTP API the
// web dashboard tunnels to (design §1, §6).
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionhost"
	"github.com/gpayer/mcp-task-manager/internal/sessionhost/claude"
)

func main() {
	fs := flag.NewFlagSet("mcp-session-host", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7778", "listen address (loopback only)")
	stateDir := fs.String("state-dir", defaultStateDir(), "session state directory")
	claudePath := fs.String("claude-path", "", "path to the claude executable (default: look up in PATH)")
	maxSessions := fs.Int("max-sessions", 4, "maximum number of live sessions")
	_ = fs.Parse(os.Args[1:])

	logger := log.New(os.Stderr, "session-host: ", log.LstdFlags)

	normAddr, err := sessionhost.NormalizeListenAddr(*addr)
	if err != nil {
		logger.Fatalf("%v", err)
	}
	backend := claude.New(*claudePath)
	mgr, err := sessionhost.NewManager(*stateDir, backend, *maxSessions)
	if err != nil {
		logger.Fatalf("open state dir: %v", err)
	}
	defer mgr.Close()
	token, err := sessionhost.EnsureToken(*stateDir)
	if err != nil {
		logger.Fatalf("ensure token: %v", err)
	}

	srv := &http.Server{Addr: normAddr, Handler: sessionhost.NewServer(mgr, token)}
	errCh := make(chan error, 1)
	go func() {
		logger.Printf("listening on %s (state dir %s)", normAddr, *stateDir)
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		logger.Fatalf("serve: %v", err)
	}

	// Graceful shutdown: stop live sessions (their claude processes die with
	// us anyway), then close the listener.
	logger.Printf("shutting down")
	stopSessions(mgr, logger)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// stopSessions terminates every live session, waiting briefly for the stops
// to land before the process exits.
func stopSessions(mgr *sessionhost.Manager, logger *log.Logger) {
	metas, err := mgr.List()
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, meta := range metas {
		if meta.Status.IsTerminal() {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := mgr.Stop(id); err != nil {
				logger.Printf("stop session %s: %v", id, err)
			}
		}(meta.ID)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		logger.Printf("timed out waiting for sessions to stop")
	}
}

func defaultStateDir() string {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "mcp-task-manager", "sessions")
	}
	return filepath.Join(cfg, "mcp-task-manager", "sessions")
}

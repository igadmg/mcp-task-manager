package project

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DefaultRootsTimeout bounds the roots/list round trip. Without it a client
// that never answers would hang the first tool call indefinitely.
const DefaultRootsTimeout = 5 * time.Second

// ServerRoots returns a RootsFunc backed by the protocol's roots/list request.
// Clients that did not declare the roots capability make this fail, which the
// resolver treats as "try the next source".
func ServerRoots(s *server.MCPServer, timeout time.Duration) RootsFunc {
	if timeout <= 0 {
		timeout = DefaultRootsTimeout
	}
	return func(ctx context.Context) ([]string, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		result, err := s.RequestRoots(ctx, mcp.ListRootsRequest{})
		if err != nil {
			return nil, err
		}

		var paths []string
		for _, root := range result.Roots {
			if p := PathFromURI(root.URI); p != "" {
				paths = append(paths, p)
			}
		}
		return paths, nil
	}
}

// PathFromURI converts a file:// root URI to a local path. Non-file URIs
// yield an empty string; a bare absolute path is accepted as a courtesy to
// clients that skip the scheme.
func PathFromURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if !strings.Contains(uri, "://") {
		if filepath.IsAbs(uri) {
			return filepath.Clean(uri)
		}
		return ""
	}

	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	// file://host/path is only meaningful for localhost.
	if parsed.Host != "" && parsed.Host != "localhost" {
		return ""
	}
	if parsed.Path == "" {
		return ""
	}
	return filepath.Clean(parsed.Path)
}

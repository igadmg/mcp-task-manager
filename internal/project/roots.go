package project

// This file is deliberately free of any MCP dependency: PathFromURI is pure
// string work, and the roots/list request that produces those URIs lives in
// internal/app with the rest of the MCP transport. That is what keeps
// mcp-go out of the dashboard binary, which imports this package for
// BuildReadOnly (cmd/boundary_test.go).

import (
	"net/url"
	"path/filepath"
	"strings"
)

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

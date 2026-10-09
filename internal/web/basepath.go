package web

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"unicode"
)

// NormalizeBasePath validates a trusted external mount, not a request header.
// Empty or / means root. One trailing slash and surrounding whitespace are
// accepted; ambiguous URL components and non-canonical paths are rejected.
func NormalizeBasePath(value string) (string, error) {
	base := strings.TrimSpace(value)
	if base == "" || base == "/" {
		return "", nil
	}
	if strings.HasPrefix(base, "//") {
		return "", fmt.Errorf("invalid MCP_WEB_BASE_PATH %q: URL authority is not a mount path", value)
	}
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(base, "/") || strings.HasPrefix(base, "//") ||
		strings.ContainsAny(base, "?#\\%") || strings.IndexFunc(base, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(base, unicode.IsControl) >= 0 || path.Clean(base) != base {
		return "", fmt.Errorf("invalid MCP_WEB_BASE_PATH %q: expected a canonical absolute URL path", value)
	}
	return base, nil
}

// mountRedirectWriter covers ServeMux's automatic slash/canonical redirects.
// Explicit handler redirects already include the mount. Only Location is
// adapted; response bodies and incoming paths are never rewritten.
type mountRedirectWriter struct {
	http.ResponseWriter
	base string
}

func (w *mountRedirectWriter) WriteHeader(status int) {
	location := w.Header().Get("Location")
	if status >= 300 && status < 400 && strings.HasPrefix(location, "/") &&
		!strings.HasPrefix(location, "//") && location != w.base &&
		!strings.HasPrefix(location, w.base+"/") {
		w.Header().Set("Location", w.base+location)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *mountRedirectWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

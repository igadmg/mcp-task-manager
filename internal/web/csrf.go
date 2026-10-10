package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"mime"
	"net/http"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

// csrfHeader is the mutation guard of the session API (design §7): the page
// hands the token out in a <meta> tag and every POST must echo it back in
// this header.
const csrfHeader = "X-Dashboard-CSRF"

// maxSessionBody bounds every session-API POST body. A prompt is the largest
// legitimate payload (64 KiB); anything bigger is not a session request.
const maxSessionBody = sessionapi.MaxPromptBytes + 4096

func newCSRFToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("web: csrf token: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// csrf guards the session API's mutations: the body must be JSON of bounded
// size and the request must carry this process's CSRF token, compared in
// constant time. A cross-origin page can do neither - it cannot read the
// token (same-origin policy) nor send a custom header without a preflight
// this server does not answer. The middleware is deliberately reusable:
// task-notes takes the same one.
func (h *handler) csrf(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeSessionError(w, &sessionapi.Error{Code: errCodeUnsupportedMediaType,
				Message: "session API posts take a JSON body"})
			return
		}
		got := r.Header.Get(csrfHeader)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.csrfToken)) != 1 {
			writeSessionError(w, &sessionapi.Error{Code: errCodeBadCSRF,
				Message: "missing or invalid CSRF token; reload the page and try again"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxSessionBody)
		next(w, r)
	}
}

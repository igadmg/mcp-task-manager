package sessionhost

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

const tokenFile = "host.token"

// EnsureToken returns the host bearer token, creating it on first use: 32
// random bytes as hex in <state-dir>/host.token with 0600 permissions
// (design §6). A corrupt or short file is replaced.
func EnsureToken(stateDir string) (string, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(stateDir, tokenFile)
	if data, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(data)); validToken(tok) {
			return tok, nil
		}
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b[:])
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return tok, nil
}

func validToken(tok string) bool {
	if len(tok) != 64 {
		return false
	}
	_, err := hex.DecodeString(tok)
	return err == nil
}

// CheckToken compares the presented bearer token against the expected one in
// constant time (design §6).
func CheckToken(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

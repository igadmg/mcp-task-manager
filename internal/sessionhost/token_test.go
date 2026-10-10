package sessionhost

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureTokenCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()
	tok, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if len(tok) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(tok))
	}
	for _, c := range tok {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			t.Fatalf("token is not hex: %q", tok)
		}
	}
	again, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if again != tok {
		t.Fatal("token changed between calls")
	}
	data, err := os.ReadFile(filepath.Join(dir, "host.token"))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != tok+"\n" {
		t.Fatalf("token file content = %q", string(data))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "host.token"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("token file mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestEnsureTokenRegeneratesGarbage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "host.token"), []byte("not-hex!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	tok, err := EnsureToken(dir)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if len(tok) != 64 {
		t.Fatalf("token not regenerated: %q", tok)
	}
}

func TestCheckToken(t *testing.T) {
	tok, err := EnsureToken(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !CheckToken(tok, tok) {
		t.Fatal("valid token rejected")
	}
	if CheckToken("x", tok) {
		t.Fatal("invalid token accepted")
	}
	if CheckToken(tok+"0", tok) {
		t.Fatal("prefix token accepted")
	}
	if CheckToken("", tok) {
		t.Fatal("empty token accepted")
	}
}

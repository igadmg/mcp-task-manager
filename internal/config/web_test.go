package config

import "testing"

func TestWebDefaults(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Web.Enabled {
		t.Error("Web.Enabled = true, want false by default: the dashboard opens no port unless asked")
	}
	if cfg.Web.Addr != DefaultWebAddr {
		t.Errorf("Web.Addr = %q, want %q", cfg.Web.Addr, DefaultWebAddr)
	}
	if cfg.Web.WithMCP {
		t.Error("Web.WithMCP = true, want false by default")
	}
}

func TestPartialWebSectionKeepsDefaultAddr(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  enabled: true\n")
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !cfg.Web.Enabled {
		t.Error("Web.Enabled = false, want true from the file")
	}
	if cfg.Web.Addr != DefaultWebAddr {
		t.Errorf("Web.Addr = %q, want %q: a partial section must not zero the rest", cfg.Web.Addr, DefaultWebAddr)
	}
}

// TestPartialAutoArchiveKeepsDefaultAfterDays is a regression guard for the
// same trap one section up: `auto_archive: {enabled: true}` used to leave
// AfterDays at 0, which archives every done task immediately.
func TestPartialAutoArchiveKeepsDefaultAfterDays(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "auto_archive:\n  enabled: true\n")
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !cfg.AutoArchive.Enabled {
		t.Error("AutoArchive.Enabled = false, want true from the file")
	}
	if cfg.AutoArchive.AfterDays != 30 {
		t.Errorf("AutoArchive.AfterDays = %d, want 30", cfg.AutoArchive.AfterDays)
	}
}

func TestEnvWebOverrides(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	t.Setenv(EnvProjectDir, root)
	t.Setenv(EnvWebAddr, "127.0.0.1:9999")
	t.Setenv(EnvWebEnabled, "true")

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Web.Addr != "127.0.0.1:9999" {
		t.Errorf("Web.Addr = %q, want 127.0.0.1:9999", cfg.Web.Addr)
	}
	if !cfg.Web.Enabled {
		t.Error("Web.Enabled = false, want true from MCP_WEB_ENABLED")
	}
}

func TestEnvWebAddrDoesNotEnable(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	t.Setenv(EnvProjectDir, root)
	t.Setenv(EnvWebAddr, "127.0.0.1:9999")

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Web.Enabled {
		t.Error("Web.Enabled = true: an address must never enable the dashboard on its own")
	}
}

func TestEnvWebEnabledInvalidIgnored(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  enabled: true\n")
	t.Setenv(EnvProjectDir, root)
	t.Setenv(EnvWebEnabled, "maybe")

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !cfg.Web.Enabled {
		t.Error("Web.Enabled = false: an unparseable MCP_WEB_ENABLED must be ignored, not guessed at")
	}
}

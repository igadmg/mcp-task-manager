package config

import "testing"

// The interactive session surface is opt-in (design §7): off by default,
// and the host address defaults to the host's own default.
func TestSessionsDisabledByDefault(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Web.Sessions.Enabled {
		t.Error("Web.Sessions.Enabled = true, want false by default")
	}
	if cfg.Web.Sessions.HostAddr != DefaultSessionHostAddr {
		t.Errorf("Web.Sessions.HostAddr = %q, want %q", cfg.Web.Sessions.HostAddr, DefaultSessionHostAddr)
	}
	if cfg.Web.Sessions.TokenFile != "" {
		t.Errorf("Web.Sessions.TokenFile = %q, want empty by default", cfg.Web.Sessions.TokenFile)
	}
}

// A partial `sessions: {enabled: true}` must not zero the address - the same
// trap the plain web section had (TestPartialWebSectionKeepsDefaultAddr).
func TestPartialSessionsSectionKeepsDefaultAddr(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  sessions:\n    enabled: true\n")
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !cfg.Web.Sessions.Enabled {
		t.Error("Web.Sessions.Enabled = false, want true from the file")
	}
	if cfg.Web.Sessions.HostAddr != DefaultSessionHostAddr {
		t.Errorf("Web.Sessions.HostAddr = %q, want the default %q", cfg.Web.Sessions.HostAddr, DefaultSessionHostAddr)
	}
}

func TestSessionsSectionRoundTrip(t *testing.T) {
	isolateEnv(t)
	root := tempDir(t)
	writeConfig(t, root, "web:\n  sessions:\n    enabled: true\n"+
		"    host_addr: 127.0.0.1:8888\n"+
		"    token_file: /var/lib/mcp/host.token\n")
	t.Setenv(EnvProjectDir, root)

	cfg, err := Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Web.Sessions.HostAddr != "127.0.0.1:8888" {
		t.Errorf("Web.Sessions.HostAddr = %q, want 127.0.0.1:8888", cfg.Web.Sessions.HostAddr)
	}
	if cfg.Web.Sessions.TokenFile != "/var/lib/mcp/host.token" {
		t.Errorf("Web.Sessions.TokenFile = %q, want /var/lib/mcp/host.token", cfg.Web.Sessions.TokenFile)
	}
}

// The web server may only reach the host over loopback (design §6/§7); a
// configured non-loopback address is a startup error, not a silent tunnel to
// somewhere else. Disabled means "not validated": the address is irrelevant.
func TestSessionsValidateLoopbackOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     WebSessionsConfig
		wantErr bool
	}{
		{"ipv4 loopback", WebSessionsConfig{Enabled: true, HostAddr: "127.0.0.1:7778"}, false},
		{"localhost", WebSessionsConfig{Enabled: true, HostAddr: "localhost:7778"}, false},
		{"ipv6 loopback", WebSessionsConfig{Enabled: true, HostAddr: "[::1]:7778"}, false},
		{"wildcard", WebSessionsConfig{Enabled: true, HostAddr: "0.0.0.0:7778"}, true},
		{"remote host", WebSessionsConfig{Enabled: true, HostAddr: "example.com:7778"}, true},
		{"garbage", WebSessionsConfig{Enabled: true, HostAddr: "not an address"}, true},
		{"disabled ignores the address", WebSessionsConfig{Enabled: false, HostAddr: "example.com:7778"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Error("Validate() = nil, want an error for a non-loopback host")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() error = %v, want nil", err)
			}
		})
	}
}

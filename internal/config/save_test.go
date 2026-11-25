package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func layout(t *testing.T) (base, confD, host string) {
	t.Helper()
	dir := t.TempDir()
	base = filepath.Join(dir, "config.yaml")
	confD = filepath.Join(dir, "conf.d")
	host = filepath.Join(dir, "hosts", "eve.yaml")
	writeFile(t, base, "# shared\nlog_level: info\nmcp_servers:\n  - name: shared\n    url: http://x/mcp\n")
	writeFile(t, host, "# eve\nmcp_servers:\n  - name: github\n    command: gh stdio\n    env:\n      TOKEN: \"${keychain:op-t}\"\n")
	if err := os.MkdirAll(confD, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host, filepath.Join(confD, "host.yaml")); err != nil {
		t.Fatal(err)
	}
	return base, confD, host
}

func TestUpsertMCPServerWritesHostFileOnly(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-t": "resolved-secret-value"})
	base, confD, host := layout(t)
	baseBefore := readFile(t, base)

	srv := MCPServerConfig{
		Name: "new", Command: "tool serve", Description: "d",
		Env: map[string]string{"K": "${keychain:op-t}"},
	}
	if err := UpsertMCPServer(base, confD, srv); err != nil {
		t.Fatal(err)
	}
	if readFile(t, base) != baseBefore {
		t.Fatal("base config was modified")
	}
	got := readFile(t, host)
	for _, want := range []string{"# eve", "name: github", "name: new", "${keychain:op-t}"} {
		if !strings.Contains(got, want) {
			t.Fatalf("host file missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "resolved-secret-value") {
		t.Fatalf("secret leaked to disk:\n%s", got)
	}
	cfg, err := LoadWithConfD(base, confD)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MCPServers) != 3 {
		t.Fatalf("want 3 servers after merge, got %d", len(cfg.MCPServers))
	}
}

func TestUpsertExistingServerMovesToHostFile(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-t": "v"})
	base, confD, host := layout(t)
	if err := UpsertMCPServer(base, confD, MCPServerConfig{Name: "shared", URL: "http://y/mcp"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFile(t, base), "name: shared") {
		t.Fatal("old entry left in base")
	}
	if !strings.Contains(readFile(t, host), "http://y/mcp") {
		t.Fatal("updated entry not in host file")
	}
	cfg, err := LoadWithConfD(base, confD)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.MCPServers {
		if s.Name == "shared" && s.URL != "http://y/mcp" {
			t.Fatalf("stale url %q", s.URL)
		}
	}
}

func TestRemoveMCPServerFromAnyFile(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-t": "v"})
	base, confD, host := layout(t)
	found, err := RemoveMCPServer(base, confD, "shared")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	found, err = RemoveMCPServer(base, confD, "github")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if strings.Contains(readFile(t, host), "name: github") || strings.Contains(readFile(t, base), "name: shared") {
		t.Fatal("entries not removed")
	}
	found, err = RemoveMCPServer(base, confD, "nope")
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestUpsertWithoutHostFileWritesBase(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "config.yaml")
	writeFile(t, base, "log_level: info\n")
	if err := UpsertMCPServer(base, filepath.Join(dir, "conf.d"), MCPServerConfig{Name: "a", URL: "http://a"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, base), "name: a") {
		t.Fatal("not written to base")
	}
}

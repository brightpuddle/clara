package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
)

func fakeKeychain(t *testing.T, items map[string]string) {
	t.Helper()
	oldLookup := keychainLookup
	keychainLookup = func(service string) (string, error) {
		if v, ok := items[service]; ok {
			return v, nil
		}
		return "", errors.Newf("keychain item %q not readable", service)
	}
	keychainMu.Lock()
	keychainCache = map[string]string{}
	keychainMu.Unlock()
	t.Cleanup(func() { keychainLookup = oldLookup })
}

func TestParseExpandsKeychainReferences(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-discord-token": "s3cret: with # yaml \"chars\""})
	t.Setenv("CLARA_TEST_DIR", "/tmp/x")

	cfg, err := parse([]byte(`
data_dir: ${CLARA_TEST_DIR}
integrations:
  discord:
    token: ${keychain:op-discord-token}
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/tmp/x" {
		t.Fatalf("env not expanded: %q", cfg.DataDir)
	}
	got := cfg.Integrations["discord"]["token"]
	if got != "s3cret: with # yaml \"chars\"" {
		t.Fatalf("keychain not expanded safely: %v", got)
	}
}

func TestParseMissingKeychainItemFails(t *testing.T) {
	fakeKeychain(t, nil)
	_, err := parse([]byte("integrations:\n  x:\n    token: ${keychain:op-nope}\n"))
	if err == nil || !strings.Contains(err.Error(), "op-nope") {
		t.Fatalf("expected error naming the missing item, got %v", err)
	}
}

func TestResolvedValueIsNotReExpanded(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-a": "pa$$word${HOME}"})
	cfg, err := parse([]byte("integrations:\n  x:\n    token: ${keychain:op-a}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Integrations["x"]["token"] != "pa$$word${HOME}" {
		t.Fatalf("value was re-expanded: %v", cfg.Integrations["x"]["token"])
	}
}

func TestLoadWithConfDExpandsKeychain(t *testing.T) {
	fakeKeychain(t, map[string]string{"op-t": "tok"})
	dir := t.TempDir()
	base := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(base, []byte("integrations:\n  x:\n    token: ${keychain:op-t}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWithConfD(base, filepath.Join(dir, "conf.d"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Integrations["x"]["token"] != "tok" {
		t.Fatalf("got %v", cfg.Integrations["x"]["token"])
	}
}

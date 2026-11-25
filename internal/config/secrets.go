package config

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// keychainPrefix marks a ${keychain:<service>} reference, resolved from the macOS
// login keychain (generic password, account = current user).
const keychainPrefix = "keychain:"

const keychainTimeout = 10 * time.Second

// keychainLookup returns the secret stored under service. It is a variable so tests
// can replace it.
var keychainLookup = securityLookup

var (
	keychainMu    sync.Mutex
	keychainCache = map[string]string{}
)

func securityLookup(service string) (string, error) {
	account := os.Getenv("USER")
	if account == "" {
		if u, err := user.Current(); err == nil {
			account = u.Username
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), keychainTimeout)
	defer cancel()
	out, err := exec.CommandContext(
		ctx, "/usr/bin/security", "find-generic-password", "-a", account, "-s", service, "-w",
	).Output()
	if err != nil {
		return "", errors.Newf("keychain item %q not readable for account %q", service, account)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func lookupKeychain(service string) (string, error) {
	keychainMu.Lock()
	defer keychainMu.Unlock()
	if v, ok := keychainCache[service]; ok {
		return v, nil
	}
	v, err := keychainLookup(service)
	if err != nil {
		return "", err
	}
	keychainCache[service] = v
	return v, nil
}

// expandString expands $VAR, ${VAR} and ${keychain:service} in s in a single pass.
// Resolved values are never re-expanded. A missing keychain item is an error.
func expandString(s string) (string, error) {
	var firstErr error
	out := os.Expand(s, func(name string) string {
		if svc, ok := strings.CutPrefix(name, keychainPrefix); ok {
			v, err := lookupKeychain(svc)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			return v
		}
		return os.Getenv(name)
	})
	return out, firstErr
}

// expandNode expands every scalar value in the YAML tree. Expanding after parsing (not
// on the raw text) means secrets containing YAML metacharacters cannot corrupt the file.
func expandNode(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "$") {
		v, err := expandString(n.Value)
		if err != nil {
			return err
		}
		n.Value = v
		return nil
	}
	for _, c := range n.Content {
		if err := expandNode(c); err != nil {
			return err
		}
	}
	return nil
}

// decodeExpanded parses YAML, expands references in scalar values, and decodes into out.
func decodeExpanded(data []byte, out any) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if err := expandNode(&root); err != nil {
		return err
	}
	if root.Kind == 0 {
		return nil
	}
	return root.Decode(out)
}

// SecretRef is a keychain reference found in a config file.
type SecretRef struct {
	Service string
	Err     error
}

// CheckKeychainRefs lists every ${keychain:service} reference in the YAML files at the
// given paths and reports whether each resolves on this host. Values are never returned.
func CheckKeychainRefs(paths ...string) ([]SecretRef, error) {
	seen := map[string]bool{}
	var refs []SecretRef
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, errors.Wrapf(err, "read %q", p)
		}
		var root yaml.Node
		if err := yaml.Unmarshal(data, &root); err != nil {
			return nil, errors.Wrapf(err, "parse %q", p)
		}
		var walk func(*yaml.Node)
		walk = func(n *yaml.Node) {
			if n.Kind == yaml.ScalarNode {
				os.Expand(n.Value, func(name string) string {
					if svc, ok := strings.CutPrefix(name, keychainPrefix); ok && !seen[svc] {
						seen[svc] = true
						_, err := lookupKeychain(svc)
						refs = append(refs, SecretRef{Service: svc, Err: err})
					}
					return ""
				})
				return
			}
			for _, c := range n.Content {
				walk(c)
			}
		}
		walk(&root)
	}
	return refs, nil
}

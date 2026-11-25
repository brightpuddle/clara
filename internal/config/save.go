package config

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// Config edits are targeted: they change only the entries being added or removed in the
// file that owns them. The merged, secret-expanded Config is never written back, so
// ${keychain:...} references, comments, and the base/host split all survive.

// configFiles returns the base config and the conf.d files that exist, in merge order.
func configFiles(basePath, confDDir string) []string {
	var files []string
	if _, err := os.Stat(basePath); err == nil {
		files = append(files, basePath)
	}
	if confDDir == "" {
		return files
	}
	entries, _ := os.ReadDir(confDDir)
	var extra []string
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			extra = append(extra, filepath.Join(confDDir, e.Name()))
		}
	}
	sort.Strings(extra)
	return append(files, extra...)
}

// hostFile is where new entries are written: conf.d/host.yaml when present, else the base.
func hostFile(basePath, confDDir string) string {
	if confDDir != "" {
		p := filepath.Join(confDDir, "host.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return basePath
}

// UpsertMCPServer adds or replaces an MCP server. Any existing entry with the same name is
// removed from every config file, and the new entry is written to the host file.
func UpsertMCPServer(basePath, confDDir string, srv MCPServerConfig) error {
	for _, f := range configFiles(basePath, confDDir) {
		if _, err := removeMCPServerFrom(f, srv.Name); err != nil {
			return err
		}
	}
	path := hostFile(basePath, confDDir)
	root, err := readDoc(path)
	if err != nil {
		return err
	}
	seq := mapValue(root, "mcp_servers", yaml.SequenceNode, true)
	seq.Content = append(seq.Content, serverNode(srv))
	return writeDoc(path, root)
}

// RemoveMCPServer removes the named server from every config file and reports whether any
// file contained it.
func RemoveMCPServer(basePath, confDDir, name string) (bool, error) {
	found := false
	for _, f := range configFiles(basePath, confDDir) {
		ok, err := removeMCPServerFrom(f, name)
		if err != nil {
			return false, err
		}
		found = found || ok
	}
	return found, nil
}

func removeMCPServerFrom(path, name string) (bool, error) {
	root, err := readDoc(path)
	if err != nil {
		return false, err
	}
	seq := mapValue(root, "mcp_servers", yaml.SequenceNode, false)
	if seq == nil {
		return false, nil
	}
	kept := seq.Content[:0:0]
	removed := false
	for _, item := range seq.Content {
		if n := mapValue(item, "name", yaml.ScalarNode, false); n != nil && n.Value == name {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return false, nil
	}
	seq.Content = kept
	return true, writeDoc(path, root)
}

// readDoc returns the root mapping of a YAML file, or a fresh mapping if it is missing/empty.
func readDoc(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, errors.Wrapf(err, "read %q", path)
	}
	var doc yaml.Node
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, errors.Wrapf(err, "parse %q", path)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 ||
		doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.Newf("%q is not a YAML mapping", path)
	}
	return &doc, nil
}

func writeDoc(path string, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return errors.Wrapf(err, "encode %q", path)
	}
	if err := enc.Close(); err != nil {
		return errors.Wrapf(err, "encode %q", path)
	}
	data := buf.Bytes()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return errors.Wrap(err, "create config directory")
	}
	// WriteFile (not rename) so a conf.d/host.yaml symlink keeps pointing at hosts/<host>.yaml.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return errors.Wrapf(err, "write %q", path)
	}
	return nil
}

// mapValue returns the value node for key in the document's root mapping (or in n when n
// is already a mapping), creating an empty node of the given kind when create is set.
func mapValue(n *yaml.Node, key string, kind yaml.Kind, create bool) *yaml.Node {
	if n.Kind == yaml.DocumentNode {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	if !create {
		return nil
	}
	v := &yaml.Node{Kind: kind}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
	return v
}

func serverNode(s MCPServerConfig) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode}
	add := func(k string, v *yaml.Node) {
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, v)
	}
	str := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	}
	add("name", str(s.Name))
	if s.URL != "" {
		add("url", str(s.URL))
	}
	if s.Token != "" {
		add("token", str(s.Token))
	}
	if s.SkipVerify {
		add("skip_verify", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	}
	if s.Command != "" {
		add("command", str(s.Command))
	}
	if len(s.Env) > 0 {
		env := &yaml.Node{Kind: yaml.MappingNode}
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env.Content = append(env.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: k}, str(s.Env[k]))
		}
		add("env", env)
	}
	if s.Description != "" {
		add("description", str(s.Description))
	}
	return m
}

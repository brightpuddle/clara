package config

import "path"

// MCPExposeProfile is a named, whitelist-only subset of tools that
// `clara mcp serve --profile <name>` may expose to an external MCP client.
//
// A profile is default-deny: a tool must match one of Include to be exposed
// at all, so newly registered tools (new plugins, new MCP servers) are never
// exposed until a profile is explicitly updated to include them. Exclude
// carves out exceptions within an otherwise-included set (e.g. a whole
// namespace minus one destructive tool).
//
// Patterns are matched against fully-qualified tool names
// ("namespace.tool", e.g. "github.create_issue") using path.Match glob
// syntax: "github.*" selects a whole namespace, "github.create_issue"
// selects a single tool.
type MCPExposeProfile struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// Allows reports whether name is exposed under this profile.
func (p MCPExposeProfile) Allows(name string) bool {
	if !matchesAnyGlob(name, p.Include) {
		return false
	}
	return !matchesAnyGlob(name, p.Exclude)
}

func matchesAnyGlob(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

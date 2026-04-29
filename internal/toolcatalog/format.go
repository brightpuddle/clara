package toolcatalog

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ansiBlue    = "\x1b[34m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
	ansiReset   = "\x1b[0m"
)

type Param struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

type Trigger struct {
	Name        string
	Description string
}

type Tool struct {
	Name        string
	Description string
	Parameters  []Param
	Examples    []string
	Triggers    []Trigger
	IsEvent     bool
}

type Provider struct {
	Name        string
	Description string
	Events      []Tool
}

func FormatProviderList(providers []Provider, useColor bool) string {
	var b strings.Builder
	for _, provider := range providers {
		name := provider.Name
		if useColor {
			name = colorize(name, ansiBlue)
		}
		b.WriteString(name)
		b.WriteString("\n")
		if provider.Description != "" {
			b.WriteString("  ")
			b.WriteString(provider.Description)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatToolList(tools []Tool, useColor bool) string {
	var b strings.Builder
	for _, tool := range tools {
		var nameLabel string
		if tool.IsEvent {
			eventTag := "[event]"
			if useColor {
				nameLabel = colorize(tool.Name, ansiMagenta) + "  " + colorize(eventTag, ansiMagenta)
			} else {
				nameLabel = tool.Name + "  " + eventTag
			}
		} else {
			nameLabel = tool.Name
			if useColor {
				nameLabel = colorize(nameLabel, ansiBlue)
			}
		}
		b.WriteString(nameLabel)
		b.WriteString("\n")
		if tool.Description != "" {
			b.WriteString("  ")
			b.WriteString(tool.Description)
			b.WriteString("\n")
		}
		for _, param := range tool.Parameters {
			b.WriteString("  ")
			if tool.IsEvent {
				b.WriteString(formatEventParam(param, useColor))
			} else {
				b.WriteString(formatParam(param, useColor))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func FormatToolDetails(tool Tool, useColor bool) string {
	var b strings.Builder
	var nameLabel string
	if tool.IsEvent {
		eventTag := "[event]"
		if useColor {
			nameLabel = colorize(tool.Name, ansiMagenta) + "  " + colorize(eventTag, ansiMagenta)
		} else {
			nameLabel = tool.Name + "  " + eventTag
		}
	} else {
		nameLabel = tool.Name
		if useColor {
			nameLabel = colorize(nameLabel, ansiBlue)
		}
	}
	b.WriteString(nameLabel)
	b.WriteString("\n")
	if tool.Description != "" {
		b.WriteString("  ")
		b.WriteString(tool.Description)
		b.WriteString("\n")
	}

	paramSection := "Parameters"
	if tool.IsEvent {
		paramSection = "Payload"
	}

	if len(tool.Parameters) > 0 {
		b.WriteString("\n")
		b.WriteString(paramSection)
		b.WriteString(":\n")
		for _, param := range tool.Parameters {
			b.WriteString("  ")
			if tool.IsEvent {
				b.WriteString(formatEventParam(param, useColor))
			} else {
				b.WriteString(formatParam(param, useColor))
			}
			b.WriteString("\n")
			if param.Description != "" {
				b.WriteString("    ")
				b.WriteString(param.Description)
				b.WriteString("\n")
			}
		}
	}

	if len(tool.Triggers) > 0 {
		b.WriteString("\nTriggers:\n")
		for _, trigger := range tool.Triggers {
			b.WriteString("  ")
			label := trigger.Name
			if useColor {
				label = colorize(label, ansiGreen)
			}
			b.WriteString(label)
			b.WriteString("\n")
			if trigger.Description != "" {
				b.WriteString("    ")
				b.WriteString(trigger.Description)
				b.WriteString("\n")
			}
		}
	}

	if len(tool.Examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, example := range tool.Examples {
			b.WriteString("  ")
			b.WriteString(example)
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func ProviderSummariesFromTools(tools []Tool) []Provider {
	descriptions := map[string]string{}
	for _, tool := range tools {
		provider, _, ok := strings.Cut(tool.Name, ".")
		if !ok {
			continue
		}
		if _, exists := descriptions[provider]; exists {
			continue
		}
		descriptions[provider] = tool.Description
	}

	providers := make([]Provider, 0, len(descriptions))
	for name, description := range descriptions {
		providers = append(providers, Provider{Name: name, Description: description})
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return providers
}

func formatParam(param Param, useColor bool) string {
	label := param.Name
	if !param.Required {
		label += "?"
	}
	label += ": " + formatTypeName(param.Type)
	if !useColor {
		return label
	}
	if param.Required {
		return colorize(label, ansiGreen)
	}
	return colorize(label, ansiYellow)
}

// formatEventParam formats a payload field for an event tool. Unlike regular
// params, event payload fields have no required/optional distinction.
func formatEventParam(param Param, useColor bool) string {
	label := param.Name + ": " + formatTypeName(param.Type)
	if useColor {
		return colorize(label, ansiMagenta)
	}
	return label
}

func formatTypeName(typ string) string {
	switch typ {
	case "string":
		return "str"
	case "integer":
		return "int"
	case "number":
		return "number"
	case "boolean":
		return "bool"
	case "array":
		return "list"
	case "object":
		return "map"
	case "":
		return "any"
	default:
		return typ
	}
}

func colorize(text, code string) string {
	return code + text + ansiReset
}

func NormalizeParams(params []Param) []Param {
	out := append([]Param(nil), params...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Required != out[j].Required {
			return out[i].Required
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func HumanStatus(counts map[string]int) string {
	return fmt.Sprintf(
		"servers: %d | tools: %d | intents: %d",
		counts["servers"],
		counts["tools"],
		counts["intents"],
	)
}

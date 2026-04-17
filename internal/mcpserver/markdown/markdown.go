package markdown

import (
	"bytes"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/frontmatter"
	"go.abhg.dev/goldmark/wikilink"
)

// Document holds the indexed metadata for a single Markdown file.
type Document struct {
	// Name is the filename stem (no extension, case-preserved).
	Name string `json:"name"`
	// Content is the raw Markdown content.
	Content string `json:"content"`
	// Frontmatter holds every key/value pair from the YAML front-matter block.
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	// Tags contains the union of front-matter tags and inline #tag references.
	Tags []string `json:"tags,omitempty"`
	// Wikilinks lists every [[target]] referenced in the note body.
	Wikilinks []string `json:"wikilinks,omitempty"`
}

var mdParser = goldmark.New(
	goldmark.WithExtensions(
		&frontmatter.Extender{},
		&wikilink.Extender{},
	),
)

// Parse extracts frontmatter, tags, and wikilinks from Markdown source.
func Parse(data []byte, name string) (*Document, error) {
	ctx := parser.NewContext()
	var buf bytes.Buffer
	if err := mdParser.Convert(data, &buf, parser.WithContext(ctx)); err != nil {
		return nil, errors.Wrap(err, "parse markdown")
	}

	// ── Frontmatter ───────────────────────────────────────────────────────
	var fm map[string]any
	if d := frontmatter.Get(ctx); d != nil {
		_ = d.Decode(&fm)
	}

	// ── Tags ──────────────────────────────────────────────────────────────
	tagSet := make(map[string]struct{})

	// Pull tags from frontmatter ("tags" field can be []any or string).
	if fm != nil {
		switch v := fm["tags"].(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					tagSet[NormalizeTag(s)] = struct{}{}
				}
			}
		case string:
			if v != "" {
				tagSet[NormalizeTag(v)] = struct{}{}
			}
		}
	}

	// Pull inline #tags by scanning the raw source for #word tokens.
	ExtractInlineTags(data, tagSet)

	tags := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tags = append(tags, t)
	}

	// ── Wikilinks ─────────────────────────────────────────────────────────
	// Re-parse so we can walk the AST for wikilink nodes.
	doc := mdParser.Parser().Parse(text.NewReader(data))
	wikilinkTargets := ExtractWikilinks(doc, data)

	return &Document{
		Name:        name,
		Content:     string(data),
		Frontmatter: fm,
		Tags:        tags,
		Wikilinks:   wikilinkTargets,
	}, nil
}

// ExtractInlineTags scans raw Markdown bytes for #tag patterns outside of
// code blocks and frontmatter.
func ExtractInlineTags(src []byte, out map[string]struct{}) {
	inFence := false
	for _, line := range bytes.Split(src, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		// Skip frontmatter delimiter lines.
		if bytes.Equal(trimmed, []byte("---")) || bytes.Equal(trimmed, []byte("+++")) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Skip code fences.
		if bytes.HasPrefix(trimmed, []byte("```")) || bytes.HasPrefix(trimmed, []byte("~~~")) {
			continue
		}
		// Scan for #tag tokens.
		i := 0
		for i < len(line) {
			idx := bytes.IndexByte(line[i:], '#')
			if idx < 0 {
				break
			}
			pos := i + idx
			// Must not be preceded by a non-space/non-start character to
			// avoid matching color hex codes, URL fragments, or Markdown
			// link destinations. A valid inline tag must appear after
			// whitespace (or at the start of a line).
			if pos > 0 {
				prev := line[pos-1]
				if prev != ' ' && prev != '\t' {
					i = pos + 1
					continue
				}
			}
			// Collect tag body: alphanumeric, hyphens, underscores, slashes
			// (for nested tags like #area/topic).
			start := pos + 1
			end := start
			for end < len(line) {
				c := line[end]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '/' {
					end++
				} else {
					break
				}
			}
			if end > start {
				out[NormalizeTag(string(line[start:end]))] = struct{}{}
			}
			i = end
		}
	}
}

// ExtractWikilinks walks the goldmark AST and collects wikilink target strings.
func ExtractWikilinks(doc ast.Node, src []byte) []string {
	seen := make(map[string]struct{})
	var targets []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		wl, ok := n.(*wikilink.Node)
		if !ok {
			return ast.WalkContinue, nil
		}
		target := strings.TrimSpace(string(wl.Target))
		if target == "" {
			return ast.WalkContinue, nil
		}
		// Strip fragment.
		if idx := strings.IndexByte(target, '#'); idx >= 0 {
			target = target[:idx]
		}
		target = strings.TrimSpace(target)
		if target != "" {
			if _, dup := seen[target]; !dup {
				seen[target] = struct{}{}
				targets = append(targets, target)
			}
		}
		return ast.WalkContinue, nil
	})
	return targets
}

func NormalizeTag(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "#"))
}

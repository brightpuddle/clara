package rag

import (
	"regexp"
	"strings"
)

const (
	defaultChunkSize    = 512 // target tokens (approx chars/4)
	defaultChunkOverlap = 64  // overlap between consecutive chunks
)

var (
	// Split on blank lines (paragraph boundaries) first.
	paragraphSep = regexp.MustCompile(`\n{2,}`)
	// Strip markdown heading markers for display, keep text.
	headingRe = regexp.MustCompile(`^#{1,6}\s+`)
)

// Chunk splits markdown content into overlapping text chunks suitable for
// embedding. It respects paragraph boundaries where possible.
func Chunk(content string) []string {
	// Strip YAML front matter
	content = stripFrontMatter(content)

	paragraphs := paragraphSep.Split(content, -1)

	var chunks []string
	var current strings.Builder
	currentLen := 0

	flush := func() {
		text := strings.TrimSpace(current.String())
		if len(text) > 0 {
			chunks = append(chunks, text)
		}
		current.Reset()
		currentLen = 0
	}

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		// Approximate token count as len/4
		paraTokens := len(para) / 4

		if currentLen+paraTokens > defaultChunkSize && currentLen > 0 {
			flush()
			// Carry overlap: last N chars of previous chunk
			if len(chunks) > 0 {
				prev := chunks[len(chunks)-1]
				overlapStart := len(prev) - min(defaultChunkOverlap*4, len(prev))
				current.WriteString(prev[overlapStart:])
				current.WriteString("\n\n")
				currentLen = len(prev[overlapStart:]) / 4
			}
		}

		current.WriteString(para)
		current.WriteString("\n\n")
		currentLen += paraTokens
	}
	flush()

	return chunks
}

func stripFrontMatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	// Find closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx == -1 {
		return content
	}
	return rest[idx+4:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

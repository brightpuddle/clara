package webexapi

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	tagRe         = regexp.MustCompile(`(?i)<(/?[a-z0-9_-]+)([^>]*)>`)
	extraBlankRe  = regexp.MustCompile(`\n{3,}`)
)

// HTMLToMarkdown converts simple Webex rich text HTML to clean Markdown.
// Webex sends rich-text messages formatted with <p>, <ol>, <ul>, <li>,
// <code>, <pre>, <blockquote>, <strong>, <em>, and <a> tags, while leaving
// the "markdown" API field empty and stripping numbers/bullets in "text".
func HTMLToMarkdown(rawHTML string) string {
	if rawHTML == "" {
		return ""
	}

	// Fast pre-check: if no HTML tags, return unescaped text
	if !strings.Contains(rawHTML, "<") {
		return html.UnescapeString(rawHTML)
	}

	var buf strings.Builder
	pos := 0

	type listState struct {
		isOrdered bool
		index     int
	}
	var listStack []listState

	matches := tagRe.FindAllStringSubmatchIndex(rawHTML, -1)
	for _, m := range matches {
		matchStart, matchEnd := m[0], m[1]
		tagNameStart, tagNameEnd := m[2], m[3]

		// Append text between tags
		if matchStart > pos {
			buf.WriteString(html.UnescapeString(rawHTML[pos:matchStart]))
		}
		pos = matchEnd

		tagName := strings.ToLower(rawHTML[tagNameStart:tagNameEnd])
		isClosing := strings.HasPrefix(tagName, "/")
		if isClosing {
			tagName = tagName[1:]
		}

		switch tagName {
		case "p", "div":
			buf.WriteString("\n\n")
		case "br":
			buf.WriteString("\n")
		case "ol":
			if !isClosing {
				listStack = append(listStack, listState{isOrdered: true, index: 1})
				buf.WriteString("\n")
			} else if len(listStack) > 0 {
				listStack = listStack[:len(listStack)-1]
				buf.WriteString("\n")
			}
		case "ul":
			if !isClosing {
				listStack = append(listStack, listState{isOrdered: false, index: 0})
				buf.WriteString("\n")
			} else if len(listStack) > 0 {
				listStack = listStack[:len(listStack)-1]
				buf.WriteString("\n")
			}
		case "li":
			if !isClosing {
				indent := strings.Repeat("  ", max(0, len(listStack)-1))
				if len(listStack) > 0 && listStack[len(listStack)-1].isOrdered {
					cur := &listStack[len(listStack)-1]
					buf.WriteString("\n" + indent + strconv.Itoa(cur.index) + ". ")
					cur.index++
				} else {
					buf.WriteString("\n" + indent + "- ")
				}
			}
		case "b", "strong":
			buf.WriteString("**")
		case "i", "em":
			buf.WriteString("*")
		case "code":
			buf.WriteString("`")
		case "pre":
			if !isClosing {
				buf.WriteString("\n```\n")
			} else {
				buf.WriteString("\n```\n")
			}
		case "blockquote":
			if !isClosing {
				buf.WriteString("\n> ")
			} else {
				buf.WriteString("\n")
			}
		}
	}

	// Remaining text
	if pos < len(rawHTML) {
		buf.WriteString(html.UnescapeString(rawHTML[pos:]))
	}

	res := buf.String()
	// Collapse excessive newlines
	res = extraBlankRe.ReplaceAllString(res, "\n\n")
	return strings.TrimSpace(res)
}

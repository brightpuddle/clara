package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func RenderJSON(theme Theme, v any) string {
	var buf bytes.Buffer
	renderJSONValue(&buf, theme, v, 0)
	return buf.String()
}

func renderJSONValue(buf *bytes.Buffer, theme Theme, v any, indent int) {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case string:
		buf.WriteString(theme.Green(mustJSONString(val)))
	case bool, float64, float32, int, int64, int32, uint, uint64, uint32:
		buf.WriteString(fmt.Sprint(val))
	case []any:
		renderJSONArray(buf, theme, val, indent)
	case map[string]any:
		renderJSONObject(buf, theme, val, indent)
	default:
		raw, err := json.Marshal(val)
		if err != nil {
			buf.WriteString(theme.Green(mustJSONString(fmt.Sprint(val))))
			return
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			buf.Write(raw)
			return
		}
		renderJSONValue(buf, theme, decoded, indent)
	}
}

func renderJSONObject(buf *bytes.Buffer, theme Theme, obj map[string]any, indent int) {
	if len(obj) == 0 {
		buf.WriteString("{}")
		return
	}
	buf.WriteString("{\n")
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		buf.WriteString(strings.Repeat("  ", indent+1))
		buf.WriteString(theme.Blue(mustJSONString(key)))
		buf.WriteString(": ")
		renderJSONValue(buf, theme, obj[key], indent+1)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString(strings.Repeat("  ", indent))
	buf.WriteString("}")
}

func renderJSONArray(buf *bytes.Buffer, theme Theme, arr []any, indent int) {
	if len(arr) == 0 {
		buf.WriteString("[]")
		return
	}
	buf.WriteString("[\n")
	for i, item := range arr {
		buf.WriteString(strings.Repeat("  ", indent+1))
		renderJSONValue(buf, theme, item, indent+1)
		if i < len(arr)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString(strings.Repeat("  ", indent))
	buf.WriteString("]")
}

func mustJSONString(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func renderNotificationLine(theme Theme, n Notification) string {
	var parts []string
	parts = append(parts, theme.Dimmed(n.CreatedAt.Format(time.Kitchen)))
	parts = append(parts, theme.Cyan(n.SourceTool))
	if n.Title != "" {
		parts = append(parts, n.Title)
	}
	if n.Subtitle != "" {
		parts = append(parts, "— "+n.Subtitle)
	}
	if n.Body != "" {
		parts = append(parts, n.Body)
	}
	line := strings.Join(parts, " ")
	if len(n.Actions) > 0 {
		actions := make([]string, 0, len(n.Actions))
		for i, action := range n.Actions {
			actions = append(actions, fmt.Sprintf("[%d] %s", i+1, action.Title))
		}
		line += " " + theme.Dimmed(strings.Join(actions, "  "))
	}
	return line
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

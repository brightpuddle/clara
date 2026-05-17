package webui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	ui "github.com/brightpuddle/clara/internal/webui/templ"
	"github.com/labstack/echo/v4"
)

// handleLogs renders the agent log page with recent log lines.
func (w *WebUI) handleLogs(c echo.Context) error {
	level := c.QueryParam("level")
	lines := tailFileFiltered(w.cfg.LogPath(), 200, level)
	return render(c, http.StatusOK, ui.Logs(ui.LogsData{
		Lines:       lines,
		LevelFilter: level,
	}))
}

// handleLogsStream streams new daemon log lines via SSE.
func (w *WebUI) handleLogsStream(c echo.Context) error {
	level := c.QueryParam("level")

	w2 := c.Response()
	w2.Header().Set("Content-Type", "text/event-stream")
	w2.Header().Set("Cache-Control", "no-cache")
	w2.Header().Set("Connection", "keep-alive")
	w2.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w2.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming unsupported")
	}

	f, err := os.Open(w.cfg.LogPath())
	if err != nil {
		// Log file doesn't exist yet — stay connected and poll
		fmt.Fprintf(w2, ": log not found\n\n")
		flusher.Flush()
		// Wait for the client to disconnect
		<-c.Request().Context().Done()
		return nil
	}
	defer f.Close()

	// Seek to end
	if _, err := f.Seek(0, 2); err != nil {
		return err
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 256*1024)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			for sc.Scan() {
				line := sc.Text()
				if level != "" && !logLineMatchesLevel(line, level) {
					continue
				}
				fmt.Fprintf(w2, "data: %s\n\n", line)
				flusher.Flush()
			}
		}
	}
}

// logLineMatchesLevel reports whether a JSONL log line has the given level or
// higher. Falls back to including the line if it cannot be parsed.
func logLineMatchesLevel(line, minLevel string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return true
	}
	lineLevel, _ := m["level"].(string)
	order := map[string]int{"trace": 0, "debug": 1, "info": 2, "warn": 3, "error": 4}
	return order[lineLevel] >= order[minLevel]
}

// tailFileFiltered reads the last n lines matching the given level filter.
func tailFileFiltered(path string, n int, level string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 256*1024), 256*1024)
	for sc.Scan() {
		line := sc.Text()
		if level != "" && !logLineMatchesLevel(line, level) {
			continue
		}
		if len(ring) >= n {
			ring = ring[1:]
		}
		ring = append(ring, line)
	}
	return ring
}

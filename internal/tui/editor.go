package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type editorFinishedMsg struct {
	text string
	err  error
}

func openEditorCmd(initial string) tea.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		return func() tea.Msg {
			return editorFinishedMsg{err: fmt.Errorf("$EDITOR is not set")}
		}
	}

	parts, err := splitCommandLine(editor)
	if err != nil || len(parts) == 0 {
		return func() tea.Msg {
			if err == nil {
				err = fmt.Errorf("invalid $EDITOR command")
			}
			return editorFinishedMsg{err: err}
		}
	}

	file, err := os.CreateTemp("", "clara-prompt-*.txt")
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{err: err} }
	}
	path := file.Name()
	if _, err := file.WriteString(initial); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return func() tea.Msg { return editorFinishedMsg{err: err} }
	}
	_ = file.Close()

	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return editorFinishedMsg{err: err}
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return editorFinishedMsg{err: readErr}
		}
		return editorFinishedMsg{text: strings.TrimRight(string(data), "\n")}
	})
}

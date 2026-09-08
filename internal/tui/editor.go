package tui

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// EditorFinishedMsg is sent when the external $EDITOR process exits.
type EditorFinishedMsg struct {
	Err  error
	Path string
}

// OpenInEditor launches $EDITOR for the specified file path.
func OpenInEditor(filePath string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	c := exec.Command(editor, filePath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return EditorFinishedMsg{
			Err:  err,
			Path: filePath,
		}
	})
}

package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func Test_contentModel_Update(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for receiver constructor.
		theme *Theme
		// Named input parameters for target function.
		msg   tea.Msg
		want  tea.Model
		want2 tea.Cmd
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newContentModel(tt.theme)
			got, got2 := m.Update(tt.msg)
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("Update() = %v, want %v", got, tt.want)
			}
			if true {
				t.Errorf("Update() = %v, want %v", got2, tt.want2)
			}
		})
	}
}

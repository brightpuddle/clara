package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type vimMode int

const (
	modeNormal vimMode = iota
	modeInsert
)

var (
	normalCursorStyle = lipgloss.NewStyle().Reverse(true)
	insertCursorStyle = lipgloss.NewStyle().Underline(true)
)

// vimInput is a minimal vim-modal text input with cursor tracking.
// The buffer may contain newlines (inserted via alt+enter in insert mode).
type vimInput struct {
	buf    []rune
	cursor int
	mode   vimMode
}

func newVimInput() vimInput {
	return vimInput{mode: modeNormal}
}

func (v vimInput) value() string { return string(v.buf) }

func (v *vimInput) clear() {
	v.buf = v.buf[:0]
	v.cursor = 0
}

// ---- Mode transitions -------------------------------------------------------

func (v *vimInput) enterInsert() { v.mode = modeInsert }

func (v *vimInput) enterInsertAfter() {
	v.mode = modeInsert
	if v.cursor < len(v.buf) {
		v.cursor++
	}
}

func (v *vimInput) enterInsertEnd() {
	v.mode = modeInsert
	v.cursor = len(v.buf)
}

func (v *vimInput) enterInsertBeginning() {
	v.mode = modeInsert
	v.cursor = 0
}

func (v *vimInput) exitInsert() {
	v.mode = modeNormal
	// In normal mode the cursor sits on a character, not past the end.
	if v.cursor > 0 && v.cursor >= len(v.buf) {
		v.cursor = len(v.buf) - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

// ---- Editing ----------------------------------------------------------------

func (v *vimInput) insertChar(r rune) {
	v.buf = append(v.buf, 0)
	copy(v.buf[v.cursor+1:], v.buf[v.cursor:])
	v.buf[v.cursor] = r
	v.cursor++
}

func (v *vimInput) backspace() {
	if v.cursor == 0 || len(v.buf) == 0 {
		return
	}
	v.buf = append(v.buf[:v.cursor-1], v.buf[v.cursor:]...)
	v.cursor--
}

func (v *vimInput) deleteAtCursor() {
	if v.cursor >= len(v.buf) {
		return
	}
	v.buf = append(v.buf[:v.cursor], v.buf[v.cursor+1:]...)
	if v.cursor >= len(v.buf) && v.cursor > 0 {
		v.cursor = len(v.buf) - 1
	}
}

func (v *vimInput) deleteToEnd() {
	if v.cursor < len(v.buf) {
		v.buf = v.buf[:v.cursor]
	}
}

// ---- Cursor movement --------------------------------------------------------

func (v *vimInput) moveCursorLeft() {
	if v.cursor > 0 {
		v.cursor--
	}
}

func (v *vimInput) moveCursorRight() {
	limit := len(v.buf)
	if v.mode == modeNormal && limit > 0 {
		limit-- // normal mode: cursor stays on last char
	}
	if v.cursor < limit {
		v.cursor++
	}
}

func (v *vimInput) moveCursorBeginning() { v.cursor = 0 }

func (v *vimInput) moveCursorEnd() {
	if v.mode == modeNormal && len(v.buf) > 0 {
		v.cursor = len(v.buf) - 1
	} else {
		v.cursor = len(v.buf)
	}
}

func (v *vimInput) moveWordForward() {
	// skip current word, then skip whitespace
	for v.cursor < len(v.buf) && !isWordBoundary(v.buf[v.cursor]) {
		v.cursor++
	}
	for v.cursor < len(v.buf) && isWordBoundary(v.buf[v.cursor]) {
		v.cursor++
	}
}

func (v *vimInput) moveWordBackward() {
	if v.cursor == 0 {
		return
	}
	v.cursor--
	for v.cursor > 0 && isWordBoundary(v.buf[v.cursor]) {
		v.cursor--
	}
	for v.cursor > 0 && !isWordBoundary(v.buf[v.cursor-1]) {
		v.cursor--
	}
}

func isWordBoundary(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// ---- Rendering --------------------------------------------------------------

// view renders the buffer with a visible cursor:
//   - normal mode: reverse-video block cursor
//   - insert mode: underline cursor (bar-style)
func (v vimInput) view() string {
	if len(v.buf) == 0 {
		if v.mode == modeInsert {
			return insertCursorStyle.Render(" ")
		}
		return normalCursorStyle.Render(" ")
	}

	var sb strings.Builder
	for i, r := range v.buf {
		if i == v.cursor {
			ch := string(r)
			if v.mode == modeNormal {
				sb.WriteString(normalCursorStyle.Render(ch))
			} else {
				sb.WriteString(insertCursorStyle.Render(ch))
			}
		} else {
			sb.WriteRune(r)
		}
	}
	// Insert mode: show cursor past end of buffer.
	if v.mode == modeInsert && v.cursor == len(v.buf) {
		sb.WriteString(insertCursorStyle.Render(" "))
	}
	return sb.String()
}

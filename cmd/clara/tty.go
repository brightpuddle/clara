package main

import (
	"os"

	"golang.org/x/term"
)

func isTerminalFile(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

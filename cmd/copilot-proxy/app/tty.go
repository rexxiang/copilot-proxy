package app

import "golang.org/x/term"

func isTTY(fd uintptr) bool {
	return term.IsTerminal(int(fd))
}

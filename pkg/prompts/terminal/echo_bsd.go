//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package terminal

import "golang.org/x/sys/unix"

const (
	terminalEchoFlag = unix.ECHO
	terminalOutputProcessingFlag = unix.OPOST
)

type terminalState = unix.Termios

func readTerminalState(descriptor uintptr) (*terminalState, error) {
	return unix.IoctlGetTermios(int(descriptor), unix.TIOCGETA)
}

func writeTerminalState(descriptor uintptr, state *terminalState) error {
	return unix.IoctlSetTermios(int(descriptor), unix.TIOCSETA, state)
}

//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package terminal

import "golang.org/x/sys/unix"

func setEcho(descriptor uintptr, enabled bool) error {
	state, err := unix.IoctlGetTermios(int(descriptor), unix.TIOCGETA)
	if err != nil {
		return err
	}
	if enabled {
		state.Lflag |= unix.ECHO
	} else {
		state.Lflag &^= unix.ECHO
	}

	return unix.IoctlSetTermios(int(descriptor), unix.TIOCSETA, state)
}

func setOutputProcessing(descriptor uintptr) error {
	state, err := unix.IoctlGetTermios(int(descriptor), unix.TIOCGETA)
	if err != nil {
		return err
	}
	state.Oflag |= unix.OPOST

	return unix.IoctlSetTermios(int(descriptor), unix.TIOCSETA, state)
}

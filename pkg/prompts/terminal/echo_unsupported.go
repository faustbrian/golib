//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package terminal

import "errors"

func setEcho(uintptr, bool) error {
	return errors.New("terminal echo control is unsupported on this platform")
}

func setOutputProcessing(uintptr) error {
	return errors.New("terminal output control is unsupported on this platform")
}

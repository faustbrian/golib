//go:build unix

package externalsort

import "syscall"

func setRestrictiveUmask() func() {
	previous := syscall.Umask(0o777)

	return func() {
		syscall.Umask(previous)
	}
}

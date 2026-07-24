//go:build darwin

package sourceio

import (
	"fmt"
	"io/fs"
	"syscall"
)

func systemChangeToken(info fs.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprint(stat.Ctimespec), true
}

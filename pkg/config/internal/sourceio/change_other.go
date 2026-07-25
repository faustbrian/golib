//go:build !darwin && !linux

package sourceio

import "io/fs"

func systemChangeToken(fs.FileInfo) (string, bool) {
	return "", false
}

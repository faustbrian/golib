//go:build !windows

package discover

import "os"

func isLinkLike(info os.FileInfo) bool {
	return info.Mode()&os.ModeSymlink != 0
}

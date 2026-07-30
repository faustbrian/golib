//go:build !windows

package discover

import "os"

func isLinkLike(info os.FileInfo) bool {
	return hasModeFlag(info.Mode(), os.ModeSymlink)
}

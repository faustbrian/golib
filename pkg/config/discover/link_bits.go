package discover

import "io/fs"

func hasModeFlag(mode, flag fs.FileMode) bool {
	return mode&flag != 0
}

func hasUint32Flag(value, flag uint32) bool {
	return value&flag != 0
}

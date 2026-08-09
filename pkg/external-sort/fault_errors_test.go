//go:build !plan9

package externalsort

import "syscall"

var (
	faultDiskFull   error = syscall.ENOSPC
	faultQuota      error = syscall.EDQUOT
	faultPermission error = syscall.EACCES
	faultMissing    error = syscall.ENOENT
	faultReadOnly   error = syscall.EROFS
	faultIO         error = syscall.EIO
)

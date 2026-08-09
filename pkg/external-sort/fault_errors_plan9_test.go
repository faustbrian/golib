//go:build plan9

package externalsort

import "errors"

var (
	faultDiskFull   = errors.New("disk full")
	faultQuota      = errors.New("quota exhausted")
	faultPermission = errors.New("permission denied")
	faultMissing    = errors.New("directory missing")
	faultReadOnly   = errors.New("read-only filesystem")
	faultIO         = errors.New("input/output failure")
)

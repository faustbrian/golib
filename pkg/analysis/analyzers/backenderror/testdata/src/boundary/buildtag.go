//go:build !windows

package boundary

import "backend"

func Platform() (string, error) {
	return backend.Load() // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Platform`
}

package child

import "backend"

func Load() (string, error) {
	return backend.Load() // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundarytree/child.Load`
}

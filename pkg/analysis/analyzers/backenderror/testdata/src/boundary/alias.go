package boundary

import service "backend"

func Alias() (string, error) {
	return service.Load() // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Alias`
}

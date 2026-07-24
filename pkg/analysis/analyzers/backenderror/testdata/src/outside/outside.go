package outside

import "backend"

func Load() (string, error) { return backend.Load() }

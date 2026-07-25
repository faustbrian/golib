package backendconsumer

import backend "arch/backend/client" // want `architecture/import-boundary: arch/backend/client may only be imported by an approved adapter`

// Open calls a restricted backend directly through an alias import.
func Open() { backend.Open[int]() }

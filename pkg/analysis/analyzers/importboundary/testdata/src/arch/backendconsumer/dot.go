package backendconsumer

import . "arch/backend/subclient" // want `architecture/import-boundary: arch/backend/subclient may only be imported by an approved adapter`

// OpenSubclient calls a restricted backend directly through a dot import.
func OpenSubclient() { OpenSub() }

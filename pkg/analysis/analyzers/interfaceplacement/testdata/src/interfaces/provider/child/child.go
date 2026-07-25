package child

// Repository proves package-tree policy applies across packages.
type Repository interface { // want `api/interface-placement: exported interface Repository is declared in a configured provider package`
	Load()
}

package child

type ChildClient interface { // want `api/interface-naming: exported interface ChildClient must start with Order and end with Port`
	Call()
}

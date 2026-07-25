//go:build go1.1

package provider

type BuildClient interface { // want `api/interface-naming: exported interface BuildClient must start with Order and end with Port`
	Build()
}

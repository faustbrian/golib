//go:build !windows

package provider

// PlatformClient is an exported provider-owned interface in a build-tagged file.
type PlatformClient interface { // want `api/interface-placement: exported interface PlatformClient is declared in a configured provider package`
	PlatformCall()
}

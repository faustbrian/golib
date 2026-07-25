//go:build go1.1

package unsafeuse

import unsafealias "unsafe" // want `security/no-unsafe: production package imports unsafe`

func BuildTaggedPointer[T any](value *T) unsafealias.Pointer {
	return unsafealias.Pointer(value)
}

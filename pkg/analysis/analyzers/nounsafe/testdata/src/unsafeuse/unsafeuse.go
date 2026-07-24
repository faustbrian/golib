package unsafeuse

import (
	"fmt"
	unsafealias "unsafe" // want `security/no-unsafe: production package imports unsafe`
)

//

//go:linkname runtimeName runtime.nameOff // want `security/no-unsafe: production code uses go:linkname`
func runtimeName()

func Pointer(value *int) unsafealias.Pointer {
	return unsafealias.Pointer(value)
}

func Safe(value int) string {
	return fmt.Sprint(value)
}

//go:build !unix

package externalsort

func setRestrictiveUmask() func() {
	return func() {}
}

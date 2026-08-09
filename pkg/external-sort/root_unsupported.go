//go:build js || plan9

package externalsort

func openRootDirectory(string) (rootDirectory, error) {
	return nil, ErrUnsafeParent
}

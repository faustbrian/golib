//go:build !js && !plan9

package externalsort

import "os"

func openRootDirectory(path string) (rootDirectory, error) {
	return os.OpenRoot(path)
}

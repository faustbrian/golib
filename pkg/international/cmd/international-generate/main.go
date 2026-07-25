// Command international-generate updates pinned international metadata.
package main

import (
	"fmt"
	"os"

	"github.com/faustbrian/golib/pkg/international/internal/generate"
)

func main() {
	if err := generate.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/faustbrian/golib/pkg/wire/internal/semver"
)

func main() {
	if len(os.Args) == 4 && os.Args[1] == "next" {
		next, err := semver.NextStable(os.Args[3], os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "calculate next release: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(next)
		return
	}
	if len(os.Args) == 2 {
		if err := semver.ValidateTag(os.Args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "invalid release tag %q: %v\n", os.Args[1], err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "usage: semvercheck vX.Y.Z | next <patch|minor|major> vX.Y.Z")
	os.Exit(2)
}

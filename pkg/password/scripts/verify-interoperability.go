//go:build ignore

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"

	password "github.com/faustbrian/golib/pkg/password"
)

const syntheticPassword = "synthetic password"

func main() {
	if err := run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 2 {
		return errors.New("usage: verify-interoperability <corpus-file>")
	}
	file, err := os.Open(arguments[1]) //nolint:gosec // The release gate supplies its own temporary corpus path.
	if err != nil {
		return errors.New("open PHP interoperability corpus")
	}
	defer file.Close() //nolint:errcheck // Read-only temporary file cleanup cannot affect verification.

	service, err := password.New(password.DefaultPolicy())
	if err != nil {
		return errors.New("create interoperability service")
	}
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
		if count > 2 {
			return errors.New("PHP interoperability corpus has extra records")
		}
		result, err := service.Verify(context.Background(), []byte(syntheticPassword), scanner.Text())
		if err != nil || !result.Match() {
			return errors.New("Go verification of fresh PHP hash failed")
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("read PHP interoperability corpus")
	}
	if count != 2 {
		return errors.New("PHP interoperability corpus is incomplete")
	}
	return nil
}

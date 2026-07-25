// Package ecmascript implements a bounded ECMA-262 regular expression engine.
//
// The package does not delegate matching to Go's regexp package. Its accepted
// syntax and behavior are closed over an explicitly selected ECMAScript
// edition.
package ecmascript

import (
	"errors"
	"fmt"
)

// Edition identifies a closed ECMA-262 language edition.
type Edition uint16

const (
	// Edition2025 is ECMA-262, 16th edition.
	Edition2025 Edition = 2025
)

var ErrUnsupportedEdition = errors.New("unsupported ECMAScript edition")

// ParseEdition parses a public edition name without accepting aliases for
// editions whose differences have not been inventoried.
func ParseEdition(name string) (Edition, error) {
	if name == Edition2025.String() {
		return Edition2025, nil
	}

	return 0, fmt.Errorf("%w: %q", ErrUnsupportedEdition, name)
}

func (e Edition) String() string {
	if e == Edition2025 {
		return "ECMAScript 2025"
	}

	return fmt.Sprintf("ECMAScript edition %d (unsupported)", uint16(e))
}

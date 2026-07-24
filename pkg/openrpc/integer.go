package openrpc

import (
	"errors"
	"regexp"
)

// ErrInvalidInteger reports a non-canonical JSON integer lexeme.
var ErrInvalidInteger = errors.New("openrpc: invalid integer")

var integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// Integer is an immutable arbitrary-precision JSON integer lexeme.
type Integer struct {
	value string
}

// ParseInteger validates a canonical JSON integer without narrowing it to a Go
// machine integer.
func ParseInteger(value string) (Integer, error) {
	if !integerPattern.MatchString(value) {
		return Integer{}, ErrInvalidInteger
	}
	return Integer{value: value}, nil
}

// String returns the exact integer lexeme.
func (integer Integer) String() string { return integer.value }

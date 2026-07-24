// Package valkeystream provides a Valkey Streams queue backend.
package valkeystream

import (
	"errors"
	"fmt"
)

// ErrInvalidConfiguration identifies unsafe Valkey Streams configuration.
var ErrInvalidConfiguration = errors.New("valkeystream: invalid configuration")

// ConfigurationError identifies the invalid configuration field without
// exposing its potentially sensitive value.
type ConfigurationError struct {
	// Field identifies the invalid package-owned option without its value.
	Field string
	// Cause retains the validation cause for errors.Is and errors.As.
	Cause error
}

// Error returns credential-safe configuration error text.
func (e *ConfigurationError) Error() string {
	return fmt.Sprintf("valkeystream: invalid %s configuration", e.Field)
}

// Unwrap retains both stable classification and the underlying cause.
func (e *ConfigurationError) Unwrap() []error {
	return []error{ErrInvalidConfiguration, e.Cause}
}

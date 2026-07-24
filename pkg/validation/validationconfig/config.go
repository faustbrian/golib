// Package validationconfig adapts typed validators to a small config contract.
package validationconfig

import validation "github.com/faustbrian/golib/pkg/validation"

// Validator is the minimal validation contract used by configuration loaders.
type Validator interface {
	Validate() error
}

// Check binds a value, immutable context, and typed validator.
type Check[T any] struct {
	value     T
	context   validation.Context
	validator validation.Validator[T]
}

// CheckValue constructs a reusable configuration validation contract.
func CheckValue[T any](value T, ctx validation.Context,
	validator validation.Validator[T],
) Check[T] {
	return Check[T]{value: value, context: ctx, validator: validator}
}

// Validate returns the typed invalid error when validation fails.
func (check Check[T]) Validate() error {
	return check.validator.Validate(check.context, check.value).Err()
}

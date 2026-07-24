package rules

import (
	"cmp"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// FieldsEqual requires two explicitly selected comparable fields to match.
func FieldsEqual[T any, V comparable](leftName, rightName string,
	left func(T) V, right func(T) V,
) validation.Validator[T] {
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if left(value) == right(value) {
			return pass(ctx)
		}
		return fail(ctx.WithPath(validation.Field(rightName)), "equal",
			map[string]string{"other": leftName})
	})
}

// RequiredWhen requires a selected value when condition is true.
func RequiredWhen[T any, V any](field string, condition func(T) bool,
	accessor func(T) validation.Value[V],
) validation.Validator[T] {
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if !condition(value) {
			return pass(ctx)
		}
		return Required[V]().Validate(ctx.WithPath(validation.Field(field)), accessor(value))
	})
}

// ExcludedWhen requires a selected value to be omitted when condition is true.
func ExcludedWhen[T any, V any](field string, condition func(T) bool,
	accessor func(T) validation.Value[V],
) validation.Validator[T] {
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if !condition(value) || accessor(value).Presence() == validation.MissingState {
			return pass(ctx)
		}
		return fail(ctx.WithPath(validation.Field(field)), "excluded", nil)
	})
}

// FieldsOrdered requires the left selected value not to exceed the right
// selected value and locates failure on the right field.
func FieldsOrdered[T any, V cmp.Ordered](leftName, rightName string,
	left func(T) V, right func(T) V,
) validation.Validator[T] {
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if left(value) <= right(value) {
			return pass(ctx)
		}
		return fail(ctx.WithPath(validation.Field(rightName)), "ordered",
			map[string]string{"other": leftName})
	})
}

// Nested validates a typed nested value at an explicit field path.
func Nested[T, V any](field string, accessor func(T) V,
	validator validation.Validator[V],
) validation.Validator[T] {
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		return validator.Validate(ctx.WithPath(validation.Field(field)), accessor(value))
	})
}

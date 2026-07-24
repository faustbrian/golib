package rules

import validation "github.com/faustbrian/golib/pkg/validation"

// Required accepts only present, non-empty values.
func Required[T any]() validation.Validator[validation.Value[T]] {
	return validation.ValidatorFunc[validation.Value[T]](func(
		ctx validation.Context, value validation.Value[T],
	) validation.Report {
		if value.IsPresent() && !value.IsEmpty() {
			return pass(ctx)
		}
		return fail(ctx, "required", nil)
	})
}

// Present accepts every supplied typed value, including empty and zero values.
func Present[T any]() validation.Validator[validation.Value[T]] {
	return validation.ValidatorFunc[validation.Value[T]](func(
		ctx validation.Context, value validation.Value[T],
	) validation.Report {
		if value.IsPresent() {
			return pass(ctx)
		}
		return fail(ctx, "present", nil)
	})
}

// Omitted accepts only a missing value.
func Omitted[T any]() validation.Validator[validation.Value[T]] {
	return state[T]("omitted", validation.MissingState)
}

// Prohibited accepts only a missing value.
func Prohibited[T any]() validation.Validator[validation.Value[T]] {
	return state[T]("prohibited", validation.MissingState)
}

// Empty accepts only a present empty value.
func Empty[T any]() validation.Validator[validation.Value[T]] {
	return validation.ValidatorFunc[validation.Value[T]](func(
		ctx validation.Context, value validation.Value[T],
	) validation.Report {
		if value.IsPresent() && value.IsEmpty() {
			return pass(ctx)
		}
		return fail(ctx, "empty", nil)
	})
}

// ZeroValue accepts only a present Go zero value.
func ZeroValue[T any]() validation.Validator[validation.Value[T]] {
	return validation.ValidatorFunc[validation.Value[T]](func(
		ctx validation.Context, value validation.Value[T],
	) validation.Report {
		if value.IsZero() {
			return pass(ctx)
		}
		return fail(ctx, "zero", nil)
	})
}

func state[T any](code string,
	want validation.Presence,
) validation.Validator[validation.Value[T]] {
	return validation.ValidatorFunc[validation.Value[T]](func(
		ctx validation.Context, value validation.Value[T],
	) validation.Report {
		if value.Presence() == want {
			return pass(ctx)
		}
		return fail(ctx, code, nil)
	})
}

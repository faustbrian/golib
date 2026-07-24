package rules

import (
	"cmp"
	"math"
	"strconv"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Range requires an ordered value within inclusive bounds.
func Range[T cmp.Ordered](minimum, maximum T) validation.Validator[T] {
	checkStringLimit := isStringType[T]()
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if checkStringLimit && valueExceedsStringLimit(ctx, value) {
			return fail(ctx, "string_limit", nil)
		}
		if value >= minimum && value <= maximum {
			return pass(ctx)
		}
		return fail(ctx, "range", nil)
	})
}

// GreaterThan requires a value strictly greater than boundary.
func GreaterThan[T cmp.Ordered](boundary T) validation.Validator[T] {
	checkStringLimit := isStringType[T]()
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if checkStringLimit && valueExceedsStringLimit(ctx, value) {
			return fail(ctx, "string_limit", nil)
		}
		if value > boundary {
			return pass(ctx)
		}
		return fail(ctx, "greater_than", nil)
	})
}

// LessThan requires a value strictly less than boundary.
func LessThan[T cmp.Ordered](boundary T) validation.Validator[T] {
	checkStringLimit := isStringType[T]()
	return validation.ValidatorFunc[T](func(ctx validation.Context, value T) validation.Report {
		if checkStringLimit && valueExceedsStringLimit(ctx, value) {
			return fail(ctx, "string_limit", nil)
		}
		if value < boundary {
			return pass(ctx)
		}
		return fail(ctx, "less_than", nil)
	})
}

// Finite rejects NaN and infinities.
func Finite() validation.Validator[float64] {
	return validation.ValidatorFunc[float64](func(ctx validation.Context, value float64) validation.Report {
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			return pass(ctx)
		}
		return fail(ctx, "finite", nil)
	})
}

// Precision requires no more than decimalPlaces fractional decimal places.
func Precision(decimalPlaces int) validation.Validator[float64] {
	scale := math.Pow10(decimalPlaces)
	return validation.ValidatorFunc[float64](func(ctx validation.Context, value float64) validation.Report {
		if decimalPlaces >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) &&
			math.Abs(value*scale-math.Round(value*scale)) <= 1e-9 {
			return pass(ctx)
		}
		return fail(ctx, "precision", map[string]string{"places": strconv.Itoa(decimalPlaces)})
	})
}

// MultipleOf requires a finite value to be an integer multiple of divisor.
func MultipleOf(divisor float64) validation.Validator[float64] {
	return validation.ValidatorFunc[float64](func(ctx validation.Context, value float64) validation.Report {
		quotient := value / divisor
		if divisor > 0 && !math.IsInf(divisor, 0) && !math.IsNaN(quotient) &&
			!math.IsInf(quotient, 0) &&
			math.Abs(quotient-math.Round(quotient)) <= 1e-9 {
			return pass(ctx)
		}
		return fail(ctx, "multiple_of", nil)
	})
}

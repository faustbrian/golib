package rules

import (
	"cmp"
	"math"
	"strconv"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Range requires an ordered value within inclusive bounds.
func Range[T cmp.Ordered](minimum, maximum T) validation.Validator[T] {
	return rangeValidator(minimum, maximum, isStringType[T]())
}

func rangeValidator[T cmp.Ordered](
	minimum, maximum T,
	checkStringLimit bool,
) validation.Validator[T] {
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
	return greaterThanValidator(boundary, isStringType[T]())
}

func greaterThanValidator[T cmp.Ordered](
	boundary T,
	checkStringLimit bool,
) validation.Validator[T] {
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
	return lessThanValidator(boundary, isStringType[T]())
}

func lessThanValidator[T cmp.Ordered](
	boundary T,
	checkStringLimit bool,
) validation.Validator[T] {
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
	return precisionValidator(decimalPlaces, math.Pow10(decimalPlaces))
}

func precisionValidator(decimalPlaces int, scale float64) validation.Validator[float64] {
	return validation.ValidatorFunc[float64](func(ctx validation.Context, value float64) validation.Report {
		if decimalPlaces < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fail(ctx, "precision", map[string]string{"places": strconv.Itoa(decimalPlaces)})
		}
		scaled := value * scale
		if math.IsNaN(scaled) || math.IsInf(scaled, 0) ||
			math.Abs(scaled-math.Round(scaled)) > 1e-9 {
			return fail(ctx, "precision", map[string]string{"places": strconv.Itoa(decimalPlaces)})
		}
		return pass(ctx)
	})
}

// MultipleOf requires a finite value to be an integer multiple of divisor.
func MultipleOf(divisor float64) validation.Validator[float64] {
	return validation.ValidatorFunc[float64](func(ctx validation.Context, value float64) validation.Report {
		if divisor == 0 || math.Signbit(divisor) || math.IsInf(divisor, 0) ||
			math.IsNaN(divisor) {
			return fail(ctx, "multiple_of", nil)
		}
		quotient := value / divisor
		if math.IsNaN(quotient) || math.IsInf(quotient, 0) {
			return fail(ctx, "multiple_of", nil)
		}
		if math.Abs(quotient-math.Round(quotient)) > 1e-9 {
			return fail(ctx, "multiple_of", nil)
		}
		return pass(ctx)
	})
}

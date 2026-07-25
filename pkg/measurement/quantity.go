package measurement

import (
	"context"
	"fmt"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

// ConversionContext makes exact versus rounded conversion an explicit caller
// choice.
type ConversionContext struct {
	mode     conversionMode
	scale    int32
	rounding decimal.RoundingMode
	limits   gomath.Limits
}

type conversionMode uint8

const (
	conversionUnset conversionMode = iota
	conversionExact
	conversionRounded
)

// ExactConversion returns a context that rejects non-terminating quotients.
func ExactConversion() ConversionContext {
	return ConversionContext{mode: conversionExact, limits: gomath.DefaultLimits()}
}

// RoundedConversion returns a context that rounds the combined final ratio at
// scale fractional places.
func RoundedConversion(scale int32, mode decimal.RoundingMode) ConversionContext {
	return ConversionContext{
		mode:     conversionRounded,
		scale:    scale,
		rounding: mode,
		limits:   gomath.DefaultLimits(),
	}
}

// WithLimits returns a copy using explicit arithmetic resource limits.
func (c ConversionContext) WithLimits(limits gomath.Limits) ConversionContext {
	c.limits = limits

	return c
}

// Quantity is an immutable decimal amount with explicit unit identity.
type Quantity struct {
	amount decimal.Decimal
	unit   Unit
}

// New validates unit and constructs an immutable quantity.
func New(amount decimal.Decimal, unit Unit) (Quantity, error) {
	if _, err := definitionFor(unit); err != nil {
		return Quantity{}, err
	}
	if err := validateAmount(amount); err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: unit}, nil
}

func validateAmount(amount decimal.Decimal) error {
	if _, err := decimal.FromBig(amount.Coefficient(), amount.Exponent(), gomath.DefaultLimits()); err != nil {
		return fmt.Errorf("%w: amount: %w", ErrInvalidQuantity, err)
	}

	return nil
}

// MustNew constructs a trusted quantity and panics for an unknown unit.
func MustNew(amount decimal.Decimal, unit Unit) Quantity {
	quantity, err := New(amount, unit)
	if err != nil {
		panic(err)
	}

	return quantity
}

// Amount returns the immutable decimal amount.
func (q Quantity) Amount() decimal.Decimal { return q.amount }

// Unit returns the quantity's unit identity.
func (q Quantity) Unit() Unit { return q.unit }

// Dimension returns the unit's physical dimension.
func (q Quantity) Dimension() (Dimension, error) { return q.unit.Dimension() }

func (q Quantity) String() string { return q.amount.String() + " " + string(q.unit) }

// Convert converts q into target under an explicit conversion context.
func (q Quantity) Convert(target Unit, context ConversionContext) (Quantity, error) {
	if err := context.validate(); err != nil {
		return Quantity{}, err
	}
	from, err := definitionFor(q.unit)
	if err != nil {
		return Quantity{}, err
	}
	to, err := definitionFor(target)
	if err != nil {
		return Quantity{}, err
	}
	if from.dimension != to.dimension {
		return Quantity{}, fmt.Errorf("%w: %s and %s", ErrDimensionMismatch, from.dimension, to.dimension)
	}
	if q.unit == target {
		return q, nil
	}

	offsetAmount, err := context.add(q.amount, from.preOffset)
	if err != nil {
		return Quantity{}, err
	}
	numerator, err := context.multiply(offsetAmount, from.numerator)
	if err != nil {
		return Quantity{}, err
	}
	numerator, err = context.multiply(numerator, to.denominator)
	if err != nil {
		return Quantity{}, err
	}
	denominator, err := context.multiply(from.denominator, to.numerator)
	if err != nil {
		return Quantity{}, err
	}
	targetAmount, err := context.divide(numerator, denominator)
	if err != nil {
		return Quantity{}, fmt.Errorf("convert %s to %s: %w", q.unit, target, err)
	}

	amount, err := context.subtract(targetAmount, to.preOffset)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: target}, nil
}

// Add converts other into q's unit and returns their exact sum.
func (q Quantity) Add(other Quantity, context ConversionContext) (Quantity, error) {
	if dimension, err := q.Dimension(); err != nil {
		return Quantity{}, err
	} else if dimension == TemperatureDimension {
		return Quantity{}, ErrAffineArithmetic
	}
	converted, err := q.compatible(other, context)
	if err != nil {
		return Quantity{}, err
	}

	amount, err := context.add(q.amount, converted.amount)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: q.unit}, nil
}

// Subtract converts other into q's unit and returns their exact difference.
func (q Quantity) Subtract(other Quantity, context ConversionContext) (Quantity, error) {
	if dimension, err := q.Dimension(); err != nil {
		return Quantity{}, err
	} else if dimension == TemperatureDimension {
		return Quantity{}, ErrAffineArithmetic
	}
	converted, err := q.compatible(other, context)
	if err != nil {
		return Quantity{}, err
	}

	amount, err := context.subtract(q.amount, converted.amount)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: q.unit}, nil
}

// Compare converts other into q's unit and compares numeric amounts.
func (q Quantity) Compare(other Quantity, context ConversionContext) (int, error) {
	converted, err := q.compatible(other, context)
	if err != nil {
		return 0, err
	}

	return q.amount.Cmp(converted.amount), nil
}

// Equal reports numeric equality after explicit compatible-unit conversion.
func (q Quantity) Equal(other Quantity, context ConversionContext) (bool, error) {
	comparison, err := q.Compare(other, context)
	if err != nil {
		return false, err
	}

	return comparison == 0, nil
}

// Multiply returns a supported canonical derived quantity.
func (q Quantity) Multiply(other Quantity, context ConversionContext) (Quantity, error) {
	leftDimension, err := q.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	rightDimension, err := other.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	resultDimension, err := leftDimension.multiply(rightDimension)
	if err != nil {
		return Quantity{}, err
	}
	resultUnit := canonicalUnits[resultDimension]

	left, err := q.Convert(canonicalUnits[leftDimension], context)
	if err != nil {
		return Quantity{}, err
	}
	right, err := other.Convert(canonicalUnits[rightDimension], context)
	if err != nil {
		return Quantity{}, err
	}

	amount, err := context.multiply(left.amount, right.amount)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: resultUnit}, nil
}

// Divide returns a supported canonical derived quantity.
func (q Quantity) Divide(other Quantity, context ConversionContext) (Quantity, error) {
	leftDimension, err := q.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	rightDimension, err := other.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	resultDimension, err := leftDimension.divide(rightDimension)
	if err != nil {
		return Quantity{}, err
	}
	resultUnit := canonicalUnits[resultDimension]

	left, err := q.Convert(canonicalUnits[leftDimension], context)
	if err != nil {
		return Quantity{}, err
	}
	right, err := other.Convert(canonicalUnits[rightDimension], context)
	if err != nil {
		return Quantity{}, err
	}
	amount, err := context.divide(left.amount, right.amount)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: resultUnit}, nil
}

// Round quantizes q at scale fractional places.
func (q Quantity) Round(scale int32, mode decimal.RoundingMode) (Quantity, error) {
	result, err := q.amount.Quantize(context.Background(), scale, mode, gomath.DefaultLimits())
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: result.Value, unit: q.unit}, nil
}

// Times multiplies an amount by a bounded positive package count.
func (q Quantity) Times(count uint64) (Quantity, error) {
	if count == 0 || count > MaxPackageQuantity {
		return Quantity{}, fmt.Errorf("%w: count must be in [1,%d]", ErrInvalidQuantity, MaxPackageQuantity)
	}
	amount, err := ExactConversion().multiply(q.amount, decimal.New(int64(count)))
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: q.unit}, nil
}

// Clamp restricts q to an inclusive compatible interval.
func (q Quantity) Clamp(minimum, maximum Quantity, context ConversionContext) (Quantity, error) {
	convertedMinimum, err := q.compatible(minimum, context)
	if err != nil {
		return Quantity{}, err
	}
	convertedMaximum, err := q.compatible(maximum, context)
	if err != nil {
		return Quantity{}, err
	}
	amount, err := q.amount.Clamp(convertedMinimum.amount, convertedMaximum.amount)
	if err != nil {
		return Quantity{}, err
	}

	return Quantity{amount: amount, unit: q.unit}, nil
}

func (q Quantity) compatible(other Quantity, context ConversionContext) (Quantity, error) {
	leftDimension, err := q.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	rightDimension, err := other.Dimension()
	if err != nil {
		return Quantity{}, err
	}
	if leftDimension != rightDimension {
		return Quantity{}, fmt.Errorf("%w: %s and %s", ErrDimensionMismatch, leftDimension, rightDimension)
	}

	return other.Convert(q.unit, context)
}

func (c ConversionContext) divide(numerator, denominator decimal.Decimal) (decimal.Decimal, error) {
	if err := c.validate(); err != nil {
		return decimal.Decimal{}, err
	}
	limits := c.arithmeticLimits()
	if c.mode == conversionExact {
		return numerator.QuoExact(context.Background(), denominator, limits)
	}
	result, err := decimal.QuantizedQuo(
		context.Background(),
		numerator,
		denominator,
		c.scale,
		c.rounding,
		limits,
	)
	if err != nil {
		return decimal.Decimal{}, err
	}

	return result.Value, nil
}

func (c ConversionContext) add(left, right decimal.Decimal) (decimal.Decimal, error) {
	if err := c.validate(); err != nil {
		return decimal.Decimal{}, err
	}
	return left.AddExact(context.Background(), right, c.arithmeticLimits())
}

func (c ConversionContext) subtract(left, right decimal.Decimal) (decimal.Decimal, error) {
	if err := c.validate(); err != nil {
		return decimal.Decimal{}, err
	}
	return left.SubExact(context.Background(), right, c.arithmeticLimits())
}

func (c ConversionContext) multiply(left, right decimal.Decimal) (decimal.Decimal, error) {
	if err := c.validate(); err != nil {
		return decimal.Decimal{}, err
	}
	return left.MulExact(context.Background(), right, c.arithmeticLimits())
}

func (c ConversionContext) validate() error {
	if c.mode == conversionUnset || c.mode > conversionRounded {
		return ErrInvalidContext
	}
	limits := c.arithmeticLimits()
	if err := limits.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidContext, err)
	}
	if c.mode == conversionRounded && (!c.rounding.Valid() ||
		int64(c.scale) > int64(limits.MaxExponentMagnitude) ||
		int64(c.scale) < -int64(limits.MaxExponentMagnitude)) {
		return ErrInvalidContext
	}

	return nil
}

func (c ConversionContext) arithmeticLimits() gomath.Limits {
	if c.limits == (gomath.Limits{}) {
		return gomath.DefaultLimits()
	}

	return c.limits
}

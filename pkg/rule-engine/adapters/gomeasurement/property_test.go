package ruleenginemeasurement_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

var dimensions = []measurement.Dimension{
	measurement.Dimensionless,
	measurement.LengthDimension,
	measurement.AreaDimension,
	measurement.VolumeDimension,
	measurement.MassDimension,
	measurement.TemperatureDimension,
	measurement.DensityDimension,
	measurement.LoadingMetreDimension,
}

func TestOperatorsMatchDirectExactComparisonAcrossUnitMatrix(t *testing.T) {
	t.Parallel()

	operators := ruleenginemeasurement.Operators()
	amounts := []decimal.Decimal{
		decimal.MustParse("-1000.25"),
		decimal.New(0),
		decimal.MustParse("1000.25"),
	}
	for _, dimension := range dimensions {
		units := measurement.Units(dimension)
		for _, leftUnit := range units {
			for _, rightUnit := range units {
				for _, leftAmount := range amounts {
					for _, rightAmount := range amounts {
						left := measurement.MustNew(leftAmount, leftUnit)
						right := measurement.MustNew(rightAmount, rightUnit)
						comparison, directErr := left.Compare(right, measurement.ExactConversion())
						for _, operator := range operators {
							got, err := operator.Evaluate(
								context.Background(),
								ruleenginemeasurement.Quantity(left),
								ruleenginemeasurement.Quantity(right),
							)
							if directErr != nil {
								if !errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity) {
									t.Fatalf("%s %s to %s error = %v, direct = %v", operator.Name(), leftUnit, rightUnit, err, directErr)
								}
								continue
							}
							if err != nil {
								t.Fatalf("%s %s to %s error = %v", operator.Name(), leftUnit, rightUnit, err)
							}
							want := relationMatches(operator.Name(), comparison)
							if got != want {
								t.Fatalf("%s comparison %d = %t, want %t", operator.Name(), comparison, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestOrderingIsAntisymmetricAndTransitive(t *testing.T) {
	t.Parallel()

	less := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityLessThan)
	values := []ruleengine.Value{
		ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1), measurement.Metre)),
		ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(150), measurement.Centimetre)),
		ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(2), measurement.Metre)),
	}
	for left := range values {
		for right := range values {
			leftRight, err := less.Evaluate(context.Background(), values[left], values[right])
			if err != nil {
				t.Fatal(err)
			}
			rightLeft, err := less.Evaluate(context.Background(), values[right], values[left])
			if err != nil {
				t.Fatal(err)
			}
			if leftRight && rightLeft {
				t.Fatalf("values %d and %d are mutually less", left, right)
			}
		}
	}
	firstSecond, _ := less.Evaluate(context.Background(), values[0], values[1])
	secondThird, _ := less.Evaluate(context.Background(), values[1], values[2])
	firstThird, _ := less.Evaluate(context.Background(), values[0], values[2])
	if !firstSecond || !secondThird || !firstThird {
		t.Fatal("less-than is not transitive")
	}
}

func TestEveryDimensionPairIsRejectedAsIncompatible(t *testing.T) {
	t.Parallel()

	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	for leftIndex, leftDimension := range dimensions {
		for rightIndex, rightDimension := range dimensions {
			if leftIndex == rightIndex {
				continue
			}
			left := measurement.MustNew(decimal.New(1), measurement.Units(leftDimension)[0])
			right := measurement.MustNew(decimal.New(1), measurement.Units(rightDimension)[0])
			_, err := equal.Evaluate(
				context.Background(),
				ruleenginemeasurement.Quantity(left),
				ruleenginemeasurement.Quantity(right),
			)
			if !errors.Is(err, ruleenginemeasurement.ErrIncompatibleQuantity) ||
				!errors.Is(err, measurement.ErrDimensionMismatch) {
				t.Fatalf("%s and %s error = %v", leftDimension, rightDimension, err)
			}
		}
	}
}

func TestCanonicalBoundaryAmountRoundTrips(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	amount := decimal.MustParse("-0." + strings.Repeat("0", limits.MaxInputDigits-2) + "1")
	value := ruleenginemeasurement.Quantity(
		measurement.MustNew(amount, measurement.KilogramPerCubicMetre),
	)
	text, _ := value.StringValue()
	if len(text) != ruleenginemeasurement.MaxTaggedValueBytes {
		t.Fatalf("boundary encoding length = %d", len(text))
	}
	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	matched, err := equal.Evaluate(context.Background(), value, value)
	if err != nil || !matched {
		t.Fatalf("Evaluate(boundary) = %t, %v", matched, err)
	}
}

func TestOperatorsAreSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()

	operator := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityGreaterThan)
	left := ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1001), measurement.Gram))
	right := ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1), measurement.Kilogram))
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				matched, err := operator.Evaluate(context.Background(), left, right)
				if err != nil || !matched {
					t.Errorf("Evaluate() = %t, %v", matched, err)
					return
				}
			}
		}()
	}
	wait.Wait()
}

func relationMatches(name ruleengine.OperatorName, comparison int) bool {
	switch name { //nolint:exhaustive // callers pass the adapter's closed operator set
	case ruleenginemeasurement.OpQuantityEqual:
		return comparison == 0
	case ruleenginemeasurement.OpQuantityLessThan:
		return comparison < 0
	case ruleenginemeasurement.OpQuantityLessOrEqual:
		return comparison <= 0
	case ruleenginemeasurement.OpQuantityGreaterThan:
		return comparison > 0
	case ruleenginemeasurement.OpQuantityGreaterOrEqual:
		return comparison >= 0
	default:
		panic("unknown operator")
	}
}

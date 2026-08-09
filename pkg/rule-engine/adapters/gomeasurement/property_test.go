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
		decimal.MustParse("-999999999999999.999999"),
		decimal.MustParse("-1000.25"),
		decimal.MustParse("-0.000000000000000001"),
		decimal.New(0),
		decimal.MustParse("0.00"),
		decimal.MustParse("0.000000000000000001"),
		decimal.MustParse("1000.25"),
		decimal.MustParse("999999999999999.999999"),
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
						if directErr == nil {
							reverse, reverseErr := right.Compare(left, measurement.ExactConversion())
							if reverseErr == nil && comparison != -reverse {
								t.Fatalf("%s and %s comparisons are not antisymmetric: %d, %d", leftUnit, rightUnit, comparison, reverse)
							}
						}
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
	chains := [][3]decimal.Decimal{
		{decimal.MustParse("-999999999999999.999999"), decimal.MustParse("-0.000000000000000001"), decimal.New(0)},
		{decimal.MustParse("-0.000000000000000001"), decimal.New(0), decimal.MustParse("0.000000000000000001")},
		{decimal.New(0), decimal.MustParse("0.000000000000000001"), decimal.MustParse("999999999999999.999999")},
	}
	for _, dimension := range dimensions {
		units := measurement.Units(dimension)
		for _, leftUnit := range units {
			for _, middleUnit := range units {
				for _, rightUnit := range units {
					for _, chain := range chains {
						left := measurement.MustNew(chain[0], leftUnit)
						middle := measurement.MustNew(chain[1], middleUnit)
						right := measurement.MustNew(chain[2], rightUnit)
						leftMiddle, firstErr := left.Compare(middle, measurement.ExactConversion())
						middleRight, secondErr := middle.Compare(right, measurement.ExactConversion())
						leftRight, thirdErr := left.Compare(right, measurement.ExactConversion())
						if firstErr != nil || secondErr != nil || thirdErr != nil || leftMiddle >= 0 || middleRight >= 0 {
							continue
						}
						for _, pair := range [][2]measurement.Quantity{{left, middle}, {middle, right}, {left, right}} {
							matched, err := less.Evaluate(context.Background(), ruleenginemeasurement.Quantity(pair[0]), ruleenginemeasurement.Quantity(pair[1]))
							if err != nil || !matched {
								t.Fatalf("%s, %s, %s operator transitivity = %t, %v", leftUnit, middleUnit, rightUnit, matched, err)
							}
						}
						if leftRight >= 0 {
							t.Fatalf("%s, %s, %s direct comparison is not transitive", leftUnit, middleUnit, rightUnit)
						}
					}
				}
			}
		}
	}
}

func TestCrossUnitConversionLimitsMatchDirectComparison(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	maximumText := strings.Repeat("9", limits.MaxInputDigits)
	minimumText := "0." + strings.Repeat("0", limits.MaxInputDigits-2) + "1"
	maximum := decimal.MustParse(maximumText)
	minimum := decimal.MustParse("-" + minimumText)
	equal := measurementOperatorByName(t, ruleenginemeasurement.OpQuantityEqual)
	for _, dimension := range dimensions {
		t.Run(dimension.String(), func(t *testing.T) {
			t.Parallel()
			units := measurement.Units(dimension)
			for _, leftUnit := range units {
				for _, rightUnit := range units {
					t.Run(rightUnit.String()+"-to-"+leftUnit.String(), func(t *testing.T) {
						t.Parallel()
						left := measurement.MustNew(decimal.New(0), leftUnit)
						right := measurement.MustNew(maximum, rightUnit)
						comparison, directErr := left.Compare(right, measurement.ExactConversion())
						matched, err := equal.Evaluate(context.Background(), ruleenginemeasurement.Quantity(left), ruleenginemeasurement.Quantity(right))
						if directErr != nil {
							if !errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity) {
								t.Fatalf("maximum error = %v, direct = %v", err, directErr)
							}
							return
						}
						if err != nil || matched != (comparison == 0) {
							t.Fatalf("maximum = %t, %v; direct comparison = %d", matched, err, comparison)
						}
					})
				}
			}
			t.Run("minimum", func(t *testing.T) {
				t.Parallel()
				left := measurement.MustNew(decimal.New(0), units[0])
				right := measurement.MustNew(minimum, units[len(units)-1])
				comparison, directErr := left.Compare(right, measurement.ExactConversion())
				matched, err := equal.Evaluate(context.Background(), ruleenginemeasurement.Quantity(left), ruleenginemeasurement.Quantity(right))
				if directErr != nil && !errors.Is(err, ruleenginemeasurement.ErrInvalidQuantity) {
					t.Fatalf("minimum conversion error = %v, direct = %v", err, directErr)
				}
				if directErr == nil && (err != nil || matched != (comparison == 0)) {
					t.Fatalf("minimum conversion = %t, %v; direct comparison = %d", matched, err, comparison)
				}
			})
		})
	}
}

func TestEveryDimensionPairIsRejectedAsIncompatible(t *testing.T) {
	t.Parallel()

	operators := ruleenginemeasurement.Operators()
	amounts := []decimal.Decimal{
		decimal.MustParse("-999999999999999.999999"),
		decimal.MustParse("-0.000000000000000001"),
		decimal.New(0),
		decimal.MustParse("0.000000000000000001"),
		decimal.MustParse("999999999999999.999999"),
	}
	for _, leftDimension := range dimensions {
		for _, rightDimension := range dimensions {
			if leftDimension == rightDimension {
				continue
			}
			for _, leftUnit := range measurement.Units(leftDimension) {
				for _, rightUnit := range measurement.Units(rightDimension) {
					for index, amount := range amounts {
						left := measurement.MustNew(amount, leftUnit)
						right := measurement.MustNew(amounts[len(amounts)-1-index], rightUnit)
						for _, operator := range operators {
							_, err := operator.Evaluate(context.Background(), ruleenginemeasurement.Quantity(left), ruleenginemeasurement.Quantity(right))
							if !errors.Is(err, ruleenginemeasurement.ErrIncompatibleQuantity) || !errors.Is(err, measurement.ErrDimensionMismatch) {
								t.Fatalf("%s: %s and %s error = %v", operator.Name(), leftUnit, rightUnit, err)
							}
						}
					}
				}
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

func TestOperatorMetadataMutationCannotAffectConcurrentCalls(t *testing.T) {
	t.Parallel()

	left := ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1001), measurement.Gram))
	right := ruleenginemeasurement.Quantity(measurement.MustNew(decimal.New(1), measurement.Kilogram))
	shared := ruleenginemeasurement.Operators()
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				operators := ruleenginemeasurement.Operators()
				operators[0] = nil
				signatures := shared[1].Signatures()
				signatures[0] = ruleengine.Signature{Left: ruleengine.KindInt, Right: ruleengine.KindInt}
				matched, err := shared[3].Evaluate(context.Background(), left, right)
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

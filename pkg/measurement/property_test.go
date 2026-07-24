package measurement_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestConversionRoundTripsAcrossEveryCompatibleUnitPair(t *testing.T) {
	t.Parallel()

	dimensions := []measurement.Dimension{
		measurement.Dimensionless,
		measurement.LengthDimension,
		measurement.AreaDimension,
		measurement.VolumeDimension,
		measurement.MassDimension,
		measurement.TemperatureDimension,
		measurement.DensityDimension,
		measurement.LoadingMetreDimension,
	}
	samples := []decimal.Decimal{
		decimal.MustParse("-123.456"),
		decimal.New(0),
		decimal.New(1),
		decimal.MustParse("99999.0001"),
	}
	conversion := measurement.RoundedConversion(18, decimal.HalfEven)

	for _, dimension := range dimensions {
		for _, source := range measurement.Units(dimension) {
			for _, target := range measurement.Units(dimension) {
				for _, sample := range samples {
					original := measurement.MustNew(sample, source)
					converted, err := original.Convert(target, conversion)
					if err != nil {
						t.Fatalf("%s to %s: %v", source, target, err)
					}
					restored, err := converted.Convert(source, conversion)
					if err != nil {
						t.Fatalf("%s to %s to %s: %v", source, target, source, err)
					}
					want, err := original.Round(9, decimal.HalfEven)
					if err != nil {
						t.Fatal(err)
					}
					got, err := restored.Round(9, decimal.HalfEven)
					if err != nil {
						t.Fatal(err)
					}
					if !got.Amount().Equal(want.Amount()) {
						t.Fatalf("round trip %s -> %s: got %s, want %s", original, target, got, want)
					}
				}
			}
		}
	}
}

func TestDerivedDimensionIdentitiesAndCommutativity(t *testing.T) {
	t.Parallel()

	left := measurement.MustNew(decimal.MustParse("1.25"), measurement.Metre)
	right := measurement.MustNew(decimal.New(80), measurement.Centimetre)
	leftRight, err := left.Multiply(right, measurement.ExactConversion())
	if err != nil {
		t.Fatal(err)
	}
	rightLeft, err := right.Multiply(left, measurement.ExactConversion())
	if err != nil {
		t.Fatal(err)
	}
	equal, err := leftRight.Equal(rightLeft, measurement.ExactConversion())
	if err != nil || !equal {
		t.Fatalf("multiplication is not commutative: %s and %s, %v", leftRight, rightLeft, err)
	}

	ratio, err := left.Divide(right, measurement.ExactConversion())
	if err != nil {
		t.Fatal(err)
	}
	if ratio.Unit() != measurement.One || ratio.String() != "1.5625 1" {
		t.Fatalf("length ratio = %s", ratio)
	}
}

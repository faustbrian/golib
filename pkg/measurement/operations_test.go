package measurement_test

import (
	"errors"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestOfficialExactUnitDefinitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount string
		from   measurement.Unit
		to     measurement.Unit
		want   string
	}{
		{"inch", "1", measurement.Inch, measurement.Millimetre, "25.4000 mm"},
		{"foot", "1", measurement.Foot, measurement.Metre, "0.3048 m"},
		{"yard", "1", measurement.Yard, measurement.Metre, "0.9144 m"},
		{"pound", "1", measurement.Pound, measurement.Kilogram, "0.45359237 kg"},
		{"ounce", "1", measurement.Ounce, measurement.Kilogram, "0.028349523125 kg"},
		{"litre", "1", measurement.Litre, measurement.CubicMetre, "0.001 m3"},
		{"density", "1", measurement.GramPerCubicCentimetre, measurement.KilogramPerCubicMetre, "1000 kg/m3"},
		{"celsius", "0", measurement.Celsius, measurement.Kelvin, "273.15 K"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			quantity := measurement.MustNew(decimal.MustParse(test.amount), test.from)
			converted, err := quantity.Convert(test.to, measurement.ExactConversion())
			if err != nil {
				t.Fatalf("Convert() error = %v", err)
			}
			if got := converted.String(); got != test.want {
				t.Fatalf("Convert() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompatibleArithmeticComparisonClampAndCount(t *testing.T) {
	t.Parallel()

	oneMetre := measurement.MustNew(decimal.New(1), measurement.Metre)
	fiftyCentimetres := measurement.MustNew(decimal.New(50), measurement.Centimetre)

	sum, err := oneMetre.Add(fiftyCentimetres, measurement.ExactConversion())
	if err != nil || sum.String() != "1.5 m" {
		t.Fatalf("Add() = %s, %v", sum, err)
	}
	difference, err := oneMetre.Subtract(fiftyCentimetres, measurement.ExactConversion())
	if err != nil || difference.String() != "0.5 m" {
		t.Fatalf("Subtract() = %s, %v", difference, err)
	}
	comparison, err := oneMetre.Compare(fiftyCentimetres, measurement.ExactConversion())
	if err != nil || comparison <= 0 {
		t.Fatalf("Compare() = %d, %v", comparison, err)
	}
	clamped, err := oneMetre.Clamp(
		measurement.MustNew(decimal.New(125), measurement.Centimetre),
		measurement.MustNew(decimal.New(2), measurement.Metre),
		measurement.ExactConversion(),
	)
	if err != nil || clamped.String() != "1.25 m" {
		t.Fatalf("Clamp() = %s, %v", clamped, err)
	}
	total, err := fiftyCentimetres.Times(4)
	if err != nil || total.String() != "200 cm" {
		t.Fatalf("Times() = %s, %v", total, err)
	}
}

func TestDivideMassByVolumeAndMultiplyDensityByVolume(t *testing.T) {
	t.Parallel()

	mass := measurement.MustNew(decimal.New(12), measurement.Kilogram)
	volume := measurement.MustNew(decimal.New(3), measurement.CubicMetre)
	density, err := mass.Divide(volume, measurement.ExactConversion())
	if err != nil || density.String() != "4 kg/m3" {
		t.Fatalf("Divide() = %s, %v", density, err)
	}
	restored, err := density.Multiply(volume, measurement.ExactConversion())
	if err != nil || restored.String() != "12 kg" {
		t.Fatalf("Multiply() = %s, %v", restored, err)
	}
}

func TestVolumetricIndexCalculatesWeight(t *testing.T) {
	t.Parallel()

	index, err := measurement.NewVolumetricIndex(
		measurement.MustNew(decimal.New(200), measurement.KilogramPerCubicMetre),
	)
	if err != nil {
		t.Fatalf("NewVolumetricIndex() error = %v", err)
	}
	weight, err := index.Weight(
		measurement.MustNew(decimal.MustParse("0.25"), measurement.CubicMetre),
		measurement.ExactConversion(),
	)
	if err != nil || weight.String() != "50.00 kg" {
		t.Fatalf("Weight() = %s, %v", weight, err)
	}
}

func TestCountAndDerivedDimensionBounds(t *testing.T) {
	t.Parallel()

	quantity := measurement.MustNew(decimal.New(1), measurement.Kilogram)
	if _, err := quantity.Times(0); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("Times(0) error = %v", err)
	}
	if _, err := quantity.Times(measurement.MaxPackageQuantity + 1); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("Times(too large) error = %v", err)
	}
	loading := measurement.MustNew(decimal.New(1), measurement.LoadingMetre)
	if _, err := loading.Multiply(quantity, measurement.ExactConversion()); !errors.Is(err, measurement.ErrUnsupportedDimension) {
		t.Fatalf("loading metre multiplication error = %v", err)
	}
}

func TestAbsoluteTemperaturesRejectAffineArithmetic(t *testing.T) {
	t.Parallel()

	freezing := measurement.MustNew(decimal.New(0), measurement.Celsius)
	boiling := measurement.MustNew(decimal.New(100), measurement.Celsius)
	if _, err := freezing.Add(boiling, measurement.ExactConversion()); !errors.Is(err, measurement.ErrAffineArithmetic) {
		t.Fatalf("temperature Add() error = %v", err)
	}
	if _, err := boiling.Subtract(freezing, measurement.ExactConversion()); !errors.Is(err, measurement.ErrAffineArithmetic) {
		t.Fatalf("temperature Subtract() error = %v", err)
	}
	comparison, err := freezing.Compare(boiling, measurement.ExactConversion())
	if err != nil || comparison >= 0 {
		t.Fatalf("temperature Compare() = %d, %v", comparison, err)
	}
}

func TestZeroConversionContextDoesNotInferRoundingPolicy(t *testing.T) {
	t.Parallel()

	quantity := measurement.MustNew(decimal.New(1), measurement.Metre)
	if _, err := quantity.Convert(measurement.Centimetre, measurement.ConversionContext{}); !errors.Is(err, measurement.ErrInvalidContext) {
		t.Fatalf("Convert() error = %v, want ErrInvalidContext", err)
	}
}

func TestTextAndSerializationInputsAreBounded(t *testing.T) {
	t.Parallel()

	tooLong := strings.Repeat("1", measurement.MaxTextBytes) + " m"
	if _, err := measurement.Parse(tooLong, measurement.SymbolProfile()); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("Parse(oversize) error = %v", err)
	}
	var quantity measurement.Quantity
	oversizeJSON := []byte(`{"value":"` + strings.Repeat("1", measurement.MaxSerializedBytes) + `","unit":"m"}`)
	if err := quantity.UnmarshalJSON(oversizeJSON); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("UnmarshalJSON(oversize) error = %v", err)
	}
}

func TestConstructorsRejectDecimalsBeyondDefaultResourceLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	input := strings.Repeat("9", limits.MaxInputDigits+1)
	limits.MaxInputDigits++
	limits.MaxOutputDigits++
	amount, err := decimal.ParseWithOptions(input, decimal.ParseOptions{Limits: limits})
	if err != nil {
		t.Fatalf("construct oversized fixture: %v", err)
	}

	if _, err := measurement.New(amount, measurement.Metre); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("New(oversize) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := measurement.NewStackingFactor(amount); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("NewStackingFactor(oversize) error = %v, want ErrLimitExceeded", err)
	}
	if _, err := measurement.NewVolumetricDivisor(amount, measurement.CubicCentimetre); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("NewVolumetricDivisor(oversize) error = %v, want ErrLimitExceeded", err)
	}
}

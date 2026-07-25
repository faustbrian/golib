package measurement_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestEveryFiniteUnitRatioAgainstCanonicalUnit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		unit   measurement.Unit
		target measurement.Unit
		want   string
	}{
		{measurement.One, measurement.One, "1 1"},
		{measurement.Millimetre, measurement.Metre, "0.001 m"},
		{measurement.Centimetre, measurement.Metre, "0.01 m"},
		{measurement.Metre, measurement.Metre, "1 m"},
		{measurement.Kilometre, measurement.Metre, "1000 m"},
		{measurement.Inch, measurement.Metre, "0.0254 m"},
		{measurement.Foot, measurement.Metre, "0.3048 m"},
		{measurement.Yard, measurement.Metre, "0.9144 m"},
		{measurement.SquareMillimetre, measurement.SquareMetre, "0.000001 m2"},
		{measurement.SquareCentimetre, measurement.SquareMetre, "0.0001 m2"},
		{measurement.SquareMetre, measurement.SquareMetre, "1 m2"},
		{measurement.SquareInch, measurement.SquareMetre, "0.00064516 m2"},
		{measurement.SquareFoot, measurement.SquareMetre, "0.09290304 m2"},
		{measurement.CubicMillimetre, measurement.CubicMetre, "0.000000001 m3"},
		{measurement.CubicCentimetre, measurement.CubicMetre, "0.000001 m3"},
		{measurement.CubicMetre, measurement.CubicMetre, "1 m3"},
		{measurement.Millilitre, measurement.CubicMetre, "0.000001 m3"},
		{measurement.Litre, measurement.CubicMetre, "0.001 m3"},
		{measurement.CubicInch, measurement.CubicMetre, "0.000016387064 m3"},
		{measurement.CubicFoot, measurement.CubicMetre, "0.028316846592 m3"},
		{measurement.Milligram, measurement.Kilogram, "0.000001 kg"},
		{measurement.Gram, measurement.Kilogram, "0.001 kg"},
		{measurement.Kilogram, measurement.Kilogram, "1 kg"},
		{measurement.Tonne, measurement.Kilogram, "1000 kg"},
		{measurement.Ounce, measurement.Kilogram, "0.028349523125 kg"},
		{measurement.Pound, measurement.Kilogram, "0.45359237 kg"},
		{measurement.Kelvin, measurement.Kelvin, "1 K"},
		{measurement.Celsius, measurement.Kelvin, "274.15 K"},
		{measurement.KilogramPerCubicMetre, measurement.KilogramPerCubicMetre, "1 kg/m3"},
		{measurement.GramPerCubicCentimetre, measurement.KilogramPerCubicMetre, "1000 kg/m3"},
		{measurement.LoadingMetre, measurement.LoadingMetre, "1 ldm"},
	}

	for _, test := range tests {
		t.Run(test.unit.String(), func(t *testing.T) {
			t.Parallel()
			converted, err := measurement.MustNew(decimal.New(1), test.unit).
				Convert(test.target, measurement.ExactConversion())
			if err != nil {
				t.Fatalf("Convert() error = %v", err)
			}
			if got := converted.String(); got != test.want {
				t.Fatalf("Convert() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFahrenheitUsesExactDefinedRatioBeforeRounding(t *testing.T) {
	t.Parallel()

	converted, err := measurement.MustNew(decimal.New(1), measurement.Fahrenheit).
		Convert(measurement.Kelvin, measurement.RoundedConversion(12, decimal.HalfEven))
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got := converted.String(); got != "255.927777777778 K" {
		t.Fatalf("Convert() = %q", got)
	}

	minusForty := measurement.MustNew(decimal.New(-40), measurement.Celsius)
	fahrenheit, err := minusForty.Convert(measurement.Fahrenheit, measurement.ExactConversion())
	if err != nil || fahrenheit.String() != "-40.00 degF" {
		t.Fatalf("-40 Celsius = %s, %v", fahrenheit, err)
	}
}

func TestEveryUnitHasTheAuditedSymbolAndDimension(t *testing.T) {
	t.Parallel()

	units := map[measurement.Unit]measurement.Dimension{
		measurement.One:        measurement.Dimensionless,
		measurement.Millimetre: measurement.LengthDimension, measurement.Centimetre: measurement.LengthDimension,
		measurement.Metre: measurement.LengthDimension, measurement.Kilometre: measurement.LengthDimension,
		measurement.Inch: measurement.LengthDimension, measurement.Foot: measurement.LengthDimension,
		measurement.Yard:             measurement.LengthDimension,
		measurement.SquareMillimetre: measurement.AreaDimension, measurement.SquareCentimetre: measurement.AreaDimension,
		measurement.SquareMetre: measurement.AreaDimension, measurement.SquareInch: measurement.AreaDimension,
		measurement.SquareFoot:      measurement.AreaDimension,
		measurement.CubicMillimetre: measurement.VolumeDimension, measurement.CubicCentimetre: measurement.VolumeDimension,
		measurement.CubicMetre: measurement.VolumeDimension, measurement.Millilitre: measurement.VolumeDimension,
		measurement.Litre: measurement.VolumeDimension, measurement.CubicInch: measurement.VolumeDimension,
		measurement.CubicFoot: measurement.VolumeDimension,
		measurement.Milligram: measurement.MassDimension, measurement.Gram: measurement.MassDimension,
		measurement.Kilogram: measurement.MassDimension, measurement.Tonne: measurement.MassDimension,
		measurement.Ounce: measurement.MassDimension, measurement.Pound: measurement.MassDimension,
		measurement.Kelvin: measurement.TemperatureDimension, measurement.Celsius: measurement.TemperatureDimension,
		measurement.Fahrenheit:             measurement.TemperatureDimension,
		measurement.KilogramPerCubicMetre:  measurement.DensityDimension,
		measurement.GramPerCubicCentimetre: measurement.DensityDimension,
		measurement.LoadingMetre:           measurement.LoadingMetreDimension,
	}

	profile := measurement.SymbolProfile()
	seen := 0
	for unit, wantDimension := range units {
		dimension, err := unit.Dimension()
		if err != nil || dimension != wantDimension {
			t.Fatalf("%s dimension = %s, %v; want %s", unit, dimension, err, wantDimension)
		}
		resolved, err := profile.Resolve(unit.String())
		if err != nil || resolved != unit {
			t.Fatalf("Resolve(%q) = %s, %v", unit, resolved, err)
		}
		seen++
	}

	catalogCount := 0
	for dimension := measurement.Dimensionless; dimension <= measurement.LoadingMetreDimension; dimension++ {
		catalogCount += len(measurement.Units(dimension))
	}
	if seen != catalogCount {
		t.Fatalf("audited units = %d, catalog units = %d", seen, catalogCount)
	}
}

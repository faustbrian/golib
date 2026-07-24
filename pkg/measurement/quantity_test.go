package measurement_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestExactLengthConversionRetainsOperands(t *testing.T) {
	t.Parallel()

	metres := measurement.MustNew(decimal.MustParse("1.25"), measurement.Metre)
	centimetres, err := metres.Convert(measurement.Centimetre, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got := centimetres.Amount().String(); got != "125.00" {
		t.Fatalf("Convert() = %s, want 125.00", got)
	}
	if got := metres.String(); got != "1.25 m" {
		t.Fatalf("source mutated: got %q", got)
	}
}

func TestIncompatibleDimensionsCannotBeCombined(t *testing.T) {
	t.Parallel()

	length := measurement.MustNew(decimal.New(1), measurement.Metre)
	mass := measurement.MustNew(decimal.New(1), measurement.Kilogram)

	if _, err := length.Add(mass, measurement.ExactConversion()); !errors.Is(err, measurement.ErrDimensionMismatch) {
		t.Fatalf("Add() error = %v, want ErrDimensionMismatch", err)
	}
	if _, err := length.Compare(mass, measurement.ExactConversion()); !errors.Is(err, measurement.ErrDimensionMismatch) {
		t.Fatalf("Compare() error = %v, want ErrDimensionMismatch", err)
	}
}

func TestDerivedDimensionsSelectCanonicalUnits(t *testing.T) {
	t.Parallel()

	length := measurement.MustNew(decimal.New(2), measurement.Metre)
	width := measurement.MustNew(decimal.New(3), measurement.Metre)
	area, err := length.Multiply(width, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("Multiply() error = %v", err)
	}
	if got := area.String(); got != "6 m2" {
		t.Fatalf("area = %q, want %q", got, "6 m2")
	}

	depth := measurement.MustNew(decimal.MustParse("0.5"), measurement.Metre)
	volume, err := area.Multiply(depth, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("Multiply() error = %v", err)
	}
	if got := volume.String(); got != "3.0 m3" {
		t.Fatalf("volume = %q, want %q", got, "3.0 m3")
	}
}

func TestTemperatureConversionRequiresExplicitRounding(t *testing.T) {
	t.Parallel()

	fahrenheit := measurement.MustNew(decimal.New(32), measurement.Fahrenheit)
	celsius, err := fahrenheit.Convert(measurement.Celsius, measurement.RoundedConversion(2, decimal.HalfEven))
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got := celsius.Amount().String(); got != "0.00" {
		t.Fatalf("Convert() = %s, want 0.00", got)
	}

	boiling := measurement.MustNew(decimal.New(100), measurement.Celsius)
	f, err := boiling.Convert(measurement.Fahrenheit, measurement.RoundedConversion(2, decimal.HalfEven))
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got := f.Amount().String(); got != "212.00" {
		t.Fatalf("Convert() = %s, want 212.00", got)
	}
}

func TestParsingUsesOnlyExplicitProfileAliases(t *testing.T) {
	t.Parallel()

	profile, err := measurement.NewProfile(map[string]measurement.Unit{"meters": measurement.Metre})
	if err != nil {
		t.Fatalf("NewProfile() error = %v", err)
	}
	quantity, err := measurement.Parse("12.5 meters", profile)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := quantity.String(); got != "12.5 m" {
		t.Fatalf("Parse() = %q", got)
	}
	if _, err := measurement.Parse("12.5 meters", measurement.SymbolProfile()); !errors.Is(err, measurement.ErrUnknownUnit) {
		t.Fatalf("Parse() error = %v, want ErrUnknownUnit", err)
	}
}

func TestRoundedConversionRoundsOnlyTheFinalRatio(t *testing.T) {
	t.Parallel()

	metres := measurement.MustNew(decimal.MustParse("1.234"), measurement.Metre)
	centimetres, err := metres.Convert(
		measurement.Centimetre,
		measurement.RoundedConversion(2, decimal.HalfEven),
	)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if got := centimetres.String(); got != "123.40 cm" {
		t.Fatalf("Convert() = %q, want %q", got, "123.40 cm")
	}
}

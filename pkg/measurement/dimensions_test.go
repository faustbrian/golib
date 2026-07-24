package measurement_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestDimensionsCalculatePackageAndShipmentVolume(t *testing.T) {
	t.Parallel()

	dimensions, err := measurement.NewDimensions(
		measurement.MustNew(decimal.MustParse("1.2"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("0.8"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("1.5"), measurement.Metre),
		10,
	)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}

	volume, err := dimensions.CubicVolume(measurement.CubicMetre, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("CubicVolume() error = %v", err)
	}
	if got := volume.String(); got != "1.440 m3" {
		t.Fatalf("CubicVolume() = %q, want %q", got, "1.440 m3")
	}

	total, err := dimensions.TotalVolume(measurement.CubicMetre, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("TotalVolume() error = %v", err)
	}
	if got := total.String(); got != "14.400 m3" {
		t.Fatalf("TotalVolume() = %q, want %q", got, "14.400 m3")
	}
}

func TestDimensionsCalculateLoadingMetresWithStacking(t *testing.T) {
	t.Parallel()

	dimensions, err := measurement.NewDimensions(
		measurement.MustNew(decimal.MustParse("1.2"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("0.8"), measurement.Metre),
		measurement.MustNew(decimal.New(1), measurement.Metre),
		10,
	)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}
	truckWidth, err := measurement.NewTruckWidth(
		measurement.MustNew(decimal.MustParse("2.4"), measurement.Metre),
	)
	if err != nil {
		t.Fatalf("NewTruckWidth() error = %v", err)
	}
	stacking, err := measurement.NewStackingFactor(decimal.New(2))
	if err != nil {
		t.Fatalf("NewStackingFactor() error = %v", err)
	}

	loadingMetres, err := dimensions.LoadingMetres(truckWidth, stacking, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("LoadingMetres() error = %v", err)
	}
	if got := loadingMetres.String(); got != "2.0 ldm" {
		t.Fatalf("LoadingMetres() = %q, want %q", got, "2.0 ldm")
	}
}

func TestVolumetricDivisorUsesExplicitVolumeUnit(t *testing.T) {
	t.Parallel()

	dimensions, err := measurement.NewDimensions(
		measurement.MustNew(decimal.New(40), measurement.Centimetre),
		measurement.MustNew(decimal.New(30), measurement.Centimetre),
		measurement.MustNew(decimal.New(20), measurement.Centimetre),
		1,
	)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}
	volume, err := dimensions.CubicVolume(measurement.CubicCentimetre, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("CubicVolume() error = %v", err)
	}
	divisor, err := measurement.NewVolumetricDivisor(decimal.New(5000), measurement.CubicCentimetre)
	if err != nil {
		t.Fatalf("NewVolumetricDivisor() error = %v", err)
	}
	weight, err := divisor.Weight(volume, measurement.ExactConversion())
	if err != nil {
		t.Fatalf("Weight() error = %v", err)
	}
	if got := weight.String(); got != "4.800 kg" {
		t.Fatalf("Weight() = %q, want %q", got, "4.800 kg")
	}
}

func TestLogisticsValuesRejectInvalidDimensions(t *testing.T) {
	t.Parallel()

	length := measurement.MustNew(decimal.New(1), measurement.Metre)
	mass := measurement.MustNew(decimal.New(1), measurement.Kilogram)
	if _, err := measurement.NewDimensions(length, mass, length, 1); !errors.Is(err, measurement.ErrDimensionMismatch) {
		t.Fatalf("NewDimensions() error = %v, want ErrDimensionMismatch", err)
	}
	if _, err := measurement.NewDimensions(length, length, length, 0); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("NewDimensions() quantity error = %v", err)
	}
	if _, err := measurement.NewStackingFactor(decimal.New(0)); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("NewStackingFactor() error = %v", err)
	}
	if _, err := measurement.NewTruckWidth(mass); !errors.Is(err, measurement.ErrDimensionMismatch) {
		t.Fatalf("NewTruckWidth() error = %v", err)
	}
}

package measurement_test

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestUnitCatalogIsStableSortedAndAliasSafe(t *testing.T) {
	t.Parallel()

	want := []measurement.Unit{
		measurement.Centimetre,
		measurement.Foot,
		measurement.Inch,
		measurement.Kilometre,
		measurement.Metre,
		measurement.Millimetre,
		measurement.Yard,
	}
	got := measurement.Units(measurement.LengthDimension)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Units(length) = %v, want %v", got, want)
	}
	if len(got) == 0 {
		t.Fatal("Units(length) unexpectedly returned an empty catalog")
	}
	got[0] = measurement.Kilogram
	if again := measurement.Units(measurement.LengthDimension); !reflect.DeepEqual(again, want) {
		t.Fatalf("catalog changed through returned slice: %v", again)
	}
	if got := measurement.Units(measurement.Dimension(255)); got != nil {
		t.Fatalf("Units(invalid) = %v, want nil", got)
	}
}

func TestProfileDefensivelyCopiesAndResolvesAliases(t *testing.T) {
	t.Parallel()

	aliases := map[string]measurement.Unit{"metres": measurement.Metre}
	profile, err := measurement.NewProfile(aliases)
	if err != nil {
		t.Fatalf("NewProfile() error = %v", err)
	}
	aliases["metres"] = measurement.Kilogram

	unit, err := profile.Resolve("metres")
	if err != nil || unit != measurement.Metre {
		t.Fatalf("Resolve() = %s, %v", unit, err)
	}
	if _, err := profile.Resolve("meters"); !errors.Is(err, measurement.ErrUnknownUnit) {
		t.Fatalf("Resolve(unknown) error = %v", err)
	}
}

func TestProfileConstructionRejectsUnboundedOrInvalidCatalogs(t *testing.T) {
	t.Parallel()

	aliases := make(map[string]measurement.Unit, measurement.MaxProfileAliases+1)
	for index := range measurement.MaxProfileAliases + 1 {
		aliases[strconv.Itoa(index)] = measurement.Metre
	}
	if _, err := measurement.NewProfile(aliases); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("NewProfile(oversize) error = %v, want ErrInvalidQuantity", err)
	}
	if _, err := measurement.NewProfile(map[string]measurement.Unit{"": measurement.Metre}); !errors.Is(err, measurement.ErrUnknownUnit) {
		t.Fatalf("NewProfile(empty alias) error = %v, want ErrUnknownUnit", err)
	}
	if _, err := measurement.NewProfile(map[string]measurement.Unit{"metres": "unknown"}); !errors.Is(err, measurement.ErrUnknownUnit) {
		t.Fatalf("NewProfile(unknown unit) error = %v, want ErrUnknownUnit", err)
	}
}

func TestQuantityEqualityConvertsCompatibleUnits(t *testing.T) {
	t.Parallel()

	oneMetre := measurement.MustNew(decimal.New(1), measurement.Metre)
	hundredCentimetres := measurement.MustNew(decimal.New(100), measurement.Centimetre)
	equal, err := oneMetre.Equal(hundredCentimetres, measurement.ExactConversion())
	if err != nil || !equal {
		t.Fatalf("Equal() = %t, %v", equal, err)
	}
	if _, err := oneMetre.Equal(
		measurement.MustNew(decimal.New(1), measurement.Kilogram),
		measurement.ExactConversion(),
	); !errors.Is(err, measurement.ErrDimensionMismatch) {
		t.Fatalf("Equal(incompatible) error = %v", err)
	}
}

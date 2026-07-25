package measurement_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func BenchmarkConvertMetresToFeet(b *testing.B) {
	quantity := measurement.MustNew(decimal.MustParse("123.456"), measurement.Metre)
	context := measurement.RoundedConversion(6, decimal.HalfEven)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := quantity.Convert(measurement.Foot, context); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDimensionsLoadingMetres(b *testing.B) {
	dimensions, _ := measurement.NewDimensions(
		measurement.MustNew(decimal.MustParse("1.2"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("0.8"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("1.5"), measurement.Metre),
		10,
	)
	truckWidth, _ := measurement.NewTruckWidth(
		measurement.MustNew(decimal.MustParse("2.4"), measurement.Metre),
	)
	stacking, _ := measurement.NewStackingFactor(decimal.New(2))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := dimensions.LoadingMetres(truckWidth, stacking, measurement.ExactConversion()); err != nil {
			b.Fatal(err)
		}
	}
}

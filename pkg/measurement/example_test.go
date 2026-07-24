package measurement_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func ExampleQuantity_Convert() {
	length := measurement.MustNew(decimal.MustParse("1.25"), measurement.Metre)
	converted, _ := length.Convert(measurement.Centimetre, measurement.ExactConversion())
	fmt.Println(converted)

	// Output:
	// 125.00 cm
}

func ExampleDimensions_LoadingMetres() {
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
	loadingMetres, _ := dimensions.LoadingMetres(
		truckWidth,
		stacking,
		measurement.ExactConversion(),
	)
	fmt.Println(loadingMetres)

	// Output:
	// 2.0 ldm
}

func ExampleVolumetricDivisor_Weight() {
	divisor, _ := measurement.NewVolumetricDivisor(
		decimal.New(5000),
		measurement.CubicCentimetre,
	)
	volume := measurement.MustNew(decimal.New(24000), measurement.CubicCentimetre)
	weight, _ := divisor.Weight(volume, measurement.ExactConversion())
	fmt.Println(weight)

	// Output:
	// 4.8 kg
}

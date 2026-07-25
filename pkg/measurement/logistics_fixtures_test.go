package measurement_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestDBSchenkerEuroPalletLoadingMetreFixture(t *testing.T) {
	t.Parallel()

	dimensions, err := measurement.NewDimensions(
		measurement.MustNew(decimal.MustParse("1.2"), measurement.Metre),
		measurement.MustNew(decimal.MustParse("0.8"), measurement.Metre),
		measurement.MustNew(decimal.New(1), measurement.Metre),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}
	truck, err := measurement.NewTruckWidth(measurement.MustNew(decimal.MustParse("2.4"), measurement.Metre))
	if err != nil {
		t.Fatal(err)
	}

	for name, fixture := range map[string]struct {
		stacking string
		want     string
	}{
		"not stackable": {stacking: "1", want: "4.0 ldm"},
		"two high":      {stacking: "2", want: "2.0 ldm"},
	} {
		t.Run(name, func(t *testing.T) {
			stacking, err := measurement.NewStackingFactor(decimal.MustParse(fixture.stacking))
			if err != nil {
				t.Fatal(err)
			}
			got, err := dimensions.LoadingMetres(truck, stacking, measurement.ExactConversion())
			if err != nil || got.String() != fixture.want {
				t.Fatalf("LoadingMetres() = %s, %v; want %s", got, err, fixture.want)
			}
		})
	}
}

func TestCarrierVolumetricWeightFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		length     string
		width      string
		height     string
		divisor    string
		wantWeight string
	}{
		{name: "DHL 50 by 40 by 30 centimetres", length: "50", width: "40", height: "30", divisor: "5000", wantWeight: "12"},
		{name: "FedEx 36 by 25 by 16 centimetres before tariff rounding", length: "36", width: "25", height: "16", divisor: "5000", wantWeight: "2.88"},
	}
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			dimensions, err := measurement.NewDimensions(
				measurement.MustNew(decimal.MustParse(fixture.length), measurement.Centimetre),
				measurement.MustNew(decimal.MustParse(fixture.width), measurement.Centimetre),
				measurement.MustNew(decimal.MustParse(fixture.height), measurement.Centimetre),
				1,
			)
			if err != nil {
				t.Fatal(err)
			}
			volume, err := dimensions.CubicVolume(measurement.CubicCentimetre, measurement.ExactConversion())
			if err != nil {
				t.Fatal(err)
			}
			divisor, err := measurement.NewVolumetricDivisor(decimal.MustParse(fixture.divisor), measurement.CubicCentimetre)
			if err != nil {
				t.Fatal(err)
			}
			weight, err := divisor.Weight(volume, measurement.ExactConversion())
			want := decimal.MustParse(fixture.wantWeight)
			if err != nil || weight.Unit() != measurement.Kilogram || !weight.Amount().Equal(want) {
				t.Fatalf("Weight() = %s, %v; want %s kg", weight, err, fixture.wantWeight)
			}
		})
	}
}

func TestDSVEuroPalletVolumetricIndexFixture(t *testing.T) {
	t.Parallel()

	volume := measurement.MustNew(decimal.MustParse("0.96"), measurement.CubicMetre)
	index, err := measurement.NewVolumetricIndex(
		measurement.MustNew(decimal.New(333), measurement.KilogramPerCubicMetre),
	)
	if err != nil {
		t.Fatal(err)
	}
	weight, err := index.Weight(volume, measurement.ExactConversion())
	if err != nil || weight.String() != "319.68 kg" {
		t.Fatalf("Weight() = %s, %v; want 319.68 kg", weight, err)
	}
}

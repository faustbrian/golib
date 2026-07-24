package measurement

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestInternalDimensionAndUnitFailurePaths(t *testing.T) {
	t.Parallel()

	if got := Dimension(255).String(); got != "Dimension(255)" {
		t.Fatalf("invalid dimension string = %q", got)
	}
	if _, err := Dimension(255).multiply(LengthDimension); !errors.Is(err, ErrUnsupportedDimension) {
		t.Fatalf("invalid multiply error = %v", err)
	}
	if _, err := LengthDimension.multiply(TemperatureDimension); !errors.Is(err, ErrUnsupportedDimension) {
		t.Fatalf("unsupported multiply error = %v", err)
	}
	if _, err := Dimension(255).divide(LengthDimension); !errors.Is(err, ErrUnsupportedDimension) {
		t.Fatalf("invalid divide error = %v", err)
	}
	if _, err := MassDimension.divide(LengthDimension); !errors.Is(err, ErrUnsupportedDimension) {
		t.Fatalf("unsupported divide error = %v", err)
	}
	multiplications := []struct {
		left, right Dimension
		want        Dimension
	}{
		{Dimensionless, LengthDimension, LengthDimension},
		{LengthDimension, Dimensionless, LengthDimension},
		{AreaDimension, Dimensionless, AreaDimension},
		{VolumeDimension, Dimensionless, VolumeDimension},
		{MassDimension, Dimensionless, MassDimension},
		{TemperatureDimension, Dimensionless, TemperatureDimension},
		{DensityDimension, Dimensionless, DensityDimension},
		{LengthDimension, LengthDimension, AreaDimension},
		{AreaDimension, LengthDimension, VolumeDimension},
		{LengthDimension, AreaDimension, VolumeDimension},
		{DensityDimension, VolumeDimension, MassDimension},
		{VolumeDimension, DensityDimension, MassDimension},
	}
	for _, test := range multiplications {
		if got, err := test.left.multiply(test.right); err != nil || got != test.want {
			t.Fatalf("%s * %s = %s, %v", test.left, test.right, got, err)
		}
	}
	divisions := []struct {
		left, right Dimension
		want        Dimension
	}{
		{LengthDimension, Dimensionless, LengthDimension},
		{LengthDimension, LengthDimension, Dimensionless},
		{AreaDimension, LengthDimension, LengthDimension},
		{VolumeDimension, LengthDimension, AreaDimension},
		{VolumeDimension, AreaDimension, LengthDimension},
		{MassDimension, VolumeDimension, DensityDimension},
		{MassDimension, DensityDimension, VolumeDimension},
	}
	for _, test := range divisions {
		if got, err := test.left.divide(test.right); err != nil || got != test.want {
			t.Fatalf("%s / %s = %s, %v", test.left, test.right, got, err)
		}
	}
	if _, err := Unit("unknown").Dimension(); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown Dimension() error = %v", err)
	}
	if _, err := definitionFor("unknown"); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("definitionFor() error = %v", err)
	}
	if _, err := New(decimal.New(1), "unknown"); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustNew() did not panic for an unknown unit")
		}
	}()
	MustNew(decimal.New(1), "unknown")
}

func TestInternalContextAndQuantityFailurePaths(t *testing.T) {
	t.Parallel()

	invalidLimits := gomath.DefaultLimits()
	invalidLimits.MaxInputDigits = 0
	if err := ExactConversion().WithLimits(invalidLimits).validate(); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid limits error = %v", err)
	}
	badMode := RoundedConversion(2, decimal.RoundingMode(255))
	if err := badMode.validate(); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid rounding error = %v", err)
	}
	smallExponent := gomath.DefaultLimits()
	smallExponent.MaxExponentMagnitude = 1
	if err := RoundedConversion(2, decimal.HalfEven).WithLimits(smallExponent).validate(); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid scale error = %v", err)
	}
	if _, err := (ConversionContext{}).add(decimal.New(1), decimal.New(1)); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid add context error = %v", err)
	}
	if _, err := (ConversionContext{}).subtract(decimal.New(1), decimal.New(1)); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid subtract context error = %v", err)
	}
	if _, err := (ConversionContext{}).multiply(decimal.New(1), decimal.New(1)); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid multiply context error = %v", err)
	}
	if _, err := (ConversionContext{}).divide(decimal.New(1), decimal.New(1)); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("invalid divide context error = %v", err)
	}

	zero := Quantity{}
	oneMetre := MustNew(decimal.New(1), Metre)
	if _, err := zero.Convert(Metre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Convert() error = %v", err)
	}
	if _, err := oneMetre.Convert("unknown", ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown target error = %v", err)
	}
	if _, err := zero.Add(oneMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Add() error = %v", err)
	}
	if _, err := zero.Subtract(oneMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Subtract() error = %v", err)
	}
	if _, err := zero.Compare(oneMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Compare() error = %v", err)
	}
	if _, err := zero.Multiply(oneMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Multiply() error = %v", err)
	}
	if _, err := zero.Divide(oneMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Divide() error = %v", err)
	}
	if _, err := oneMetre.Divide(MustNew(decimal.New(0), Metre), ExactConversion()); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("division by zero error = %v", err)
	}
	if _, err := MustNew(decimal.New(1), Kilogram).Divide(oneMetre, ExactConversion()); !errors.Is(err, ErrUnsupportedDimension) {
		t.Fatalf("unsupported quotient error = %v", err)
	}
	if _, err := oneMetre.Round(2, decimal.RoundingMode(255)); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Round() error = %v", err)
	}
	if _, err := oneMetre.Clamp(MustNew(decimal.New(2), Metre), MustNew(decimal.New(1), Metre), ExactConversion()); !errors.Is(err, decimal.ErrInvalid) {
		t.Fatalf("Clamp() range error = %v", err)
	}
	if _, err := oneMetre.Clamp(MustNew(decimal.New(1), Kilogram), oneMetre, ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("Clamp() dimension error = %v", err)
	}

	tiny := gomath.DefaultLimits()
	tiny.MaxIntermediateBits = 1
	tight := ExactConversion().WithLimits(tiny)
	if _, err := tight.add(decimal.New(1), decimal.New(1)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("bounded add error = %v", err)
	}
	if _, err := tight.multiply(decimal.New(2), decimal.New(2)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("bounded multiply error = %v", err)
	}
	if _, err := tight.divide(decimal.New(1), decimal.New(3)); err == nil {
		t.Fatal("bounded or non-terminating division unexpectedly succeeded")
	}
	if got := (ConversionContext{mode: conversionExact}).arithmeticLimits(); got != gomath.DefaultLimits() {
		t.Fatal("zero limits did not select math defaults")
	}
}

func TestInternalQuantityArithmeticResourcePaths(t *testing.T) {
	t.Parallel()

	limited := func(bits int) ConversionContext {
		limits := gomath.DefaultLimits()
		limits.MaxIntermediateBits = bits

		return ExactConversion().WithLimits(limits)
	}
	oneMetre := MustNew(decimal.New(1), Metre)
	twoMetres := MustNew(decimal.New(2), Metre)
	if _, err := twoMetres.Convert(Centimetre, limited(1)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("conversion offset add error = %v", err)
	}
	if _, err := MustNew(decimal.New(1), Kilometre).Convert(Metre, limited(5)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("conversion source numerator error = %v", err)
	}
	if _, err := oneMetre.Convert(Millimetre, limited(5)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("conversion target denominator error = %v", err)
	}
	if _, err := MustNew(decimal.New(1), Millimetre).Convert(Inch, limited(15)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("conversion combined denominator error = %v", err)
	}
	if _, err := oneMetre.Convert(Inch, ExactConversion()); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("conversion quotient error = %v", err)
	}
	if _, err := MustNew(decimal.New(1), Kelvin).Convert(Celsius, limited(10)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("conversion target offset error = %v", err)
	}

	if _, err := twoMetres.Add(twoMetres, limited(1)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Add() arithmetic error = %v", err)
	}
	if _, err := MustNew(decimal.New(1), Inch).Subtract(oneMetre, ExactConversion()); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Subtract() conversion error = %v", err)
	}
	if _, err := twoMetres.Subtract(oneMetre, limited(1)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Subtract() arithmetic error = %v", err)
	}
	if _, err := oneMetre.Multiply(Quantity{}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("Multiply() right dimension error = %v", err)
	}
	if _, err := oneMetre.Multiply(oneMetre, ConversionContext{}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Multiply() left conversion error = %v", err)
	}
	if _, err := oneMetre.Multiply(MustNew(decimal.New(1), Kilometre), limited(5)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Multiply() right conversion error = %v", err)
	}
	if _, err := twoMetres.Multiply(twoMetres, limited(1)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Multiply() arithmetic error = %v", err)
	}
	if _, err := oneMetre.Divide(Quantity{}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("Divide() right dimension error = %v", err)
	}
	if _, err := oneMetre.Divide(oneMetre, ConversionContext{}); !errors.Is(err, ErrInvalidContext) {
		t.Fatalf("Divide() left conversion error = %v", err)
	}
	if _, err := oneMetre.Divide(MustNew(decimal.New(1), Kilometre), limited(5)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Divide() right conversion error = %v", err)
	}
	if _, err := oneMetre.Divide(MustNew(decimal.New(3), Metre), ExactConversion()); !errors.Is(err, gomath.ErrConversion) {
		t.Fatalf("Divide() quotient error = %v", err)
	}
	if _, err := oneMetre.Add(Quantity{}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("compatible right dimension error = %v", err)
	}
	if _, err := oneMetre.Clamp(oneMetre, MustNew(decimal.New(1), Kilogram), ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("Clamp() maximum error = %v", err)
	}
	if _, err := RoundedConversion(2, decimal.HalfEven).divide(decimal.New(1), decimal.New(0)); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("rounded division error = %v", err)
	}
	maximumDigits := strings.Repeat("9", gomath.DefaultLimits().MaxInputDigits)
	if _, err := MustNew(decimal.MustParse(maximumDigits), Kilogram).Times(2); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Times() output limit error = %v", err)
	}
}

func TestInternalDimensionTripleFailurePaths(t *testing.T) {
	t.Parallel()

	one := MustNew(decimal.New(1), Metre)
	zero := MustNew(decimal.New(0), Metre)
	if _, err := NewDimensions(Quantity{}, one, one, 1); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown side error = %v", err)
	}
	if _, err := NewDimensions(zero, one, one, 1); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("zero side error = %v", err)
	}
	if _, err := NewDimensions(one, one, one, MaxPackageQuantity+1); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("large quantity error = %v", err)
	}
	if _, err := NewTruckWidth(Quantity{}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown truck width error = %v", err)
	}
	if _, err := NewTruckWidth(zero); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("zero truck width error = %v", err)
	}
	if _, err := NewVolumetricDivisor(decimal.New(1), "unknown"); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown divisor unit error = %v", err)
	}
	if _, err := NewVolumetricDivisor(decimal.New(1), Metre); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("divisor dimension error = %v", err)
	}
	if _, err := NewVolumetricDivisor(decimal.New(0), CubicMetre); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("zero divisor error = %v", err)
	}
	if _, err := NewVolumetricIndex(Quantity{}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("unknown index error = %v", err)
	}
	if _, err := NewVolumetricIndex(one); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("index dimension error = %v", err)
	}
	if _, err := NewVolumetricIndex(MustNew(decimal.New(0), KilogramPerCubicMetre)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("zero index error = %v", err)
	}

	dimensions, _ := NewDimensions(one, one, one, 1)
	if _, err := (Dimensions{}).FloorArea(SquareMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero FloorArea error = %v", err)
	}
	if _, err := dimensions.FloorArea(Kilogram, ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("FloorArea target error = %v", err)
	}
	if _, err := dimensions.CubicVolume(Kilogram, ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("CubicVolume target error = %v", err)
	}
	if _, err := (Dimensions{length: one, width: one}).CubicVolume(CubicMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("CubicVolume height error = %v", err)
	}
	if _, err := (Dimensions{}).TotalVolume(CubicMetre, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero TotalVolume error = %v", err)
	}
	stacking := StackingFactor{}
	truck, _ := NewTruckWidth(one)
	if _, err := dimensions.LoadingMetres(truck, stacking, ExactConversion()); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("zero stacking error = %v", err)
	}
	if _, err := dimensions.LoadingMetres(TruckWidth{}, StackingFactor{factor: decimal.New(1)}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero truck error = %v", err)
	}
	if _, err := (Dimensions{}).LoadingMetres(truck, StackingFactor{factor: decimal.New(1)}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("loading area error = %v", err)
	}
	zeroWidth := TruckWidth{width: MustNew(decimal.New(0), Metre)}
	if _, err := dimensions.LoadingMetres(zeroWidth, StackingFactor{factor: decimal.New(1)}, ExactConversion()); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("loading length error = %v", err)
	}
	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 1
	tight := ExactConversion().WithLimits(limits)
	twoPackages, _ := NewDimensions(one, one, one, 2)
	if _, err := twoPackages.TotalVolume(CubicMetre, tight); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("TotalVolume count error = %v", err)
	}
	if _, err := twoPackages.LoadingMetres(truck, StackingFactor{factor: decimal.New(1)}, tight); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("LoadingMetres count error = %v", err)
	}
	if _, err := (VolumetricDivisor{}).Weight(MustNew(decimal.New(1), CubicMetre), ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero divisor Weight error = %v", err)
	}
	divisor, _ := NewVolumetricDivisor(decimal.New(1), CubicMetre)
	if _, err := divisor.Weight(one, ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("weight input dimension error = %v", err)
	}
	if _, err := divisor.Weight(Quantity{}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("weight unknown input error = %v", err)
	}
	zeroDivisor := VolumetricDivisor{volumePerKilogram: decimal.New(0), volumeUnit: CubicMetre}
	if _, err := zeroDivisor.Weight(MustNew(decimal.New(1), CubicMetre), ExactConversion()); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("weight division error = %v", err)
	}
	if _, err := (VolumetricIndex{}).Weight(MustNew(decimal.New(1), CubicMetre), ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero index Weight error = %v", err)
	}
	index, _ := NewVolumetricIndex(MustNew(decimal.New(1), KilogramPerCubicMetre))
	if _, err := index.Weight(one, ExactConversion()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("index input dimension error = %v", err)
	}
	if _, err := index.Weight(Quantity{}, ExactConversion()); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("index unknown input error = %v", err)
	}

	_ = truck.Quantity()
	_ = StackingFactor{factor: decimal.New(2)}.Decimal()
	_ = divisor.VolumePerKilogram()
	_ = divisor.VolumeUnit()
	_ = index.Density()
}

func TestInternalEncodingFailurePaths(t *testing.T) {
	t.Parallel()

	var nilQuantity *Quantity
	if err := nilQuantity.UnmarshalJSON([]byte(`{}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("nil JSON receiver error = %v", err)
	}
	if err := nilQuantity.UnmarshalText([]byte("1 m")); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("nil text receiver error = %v", err)
	}
	if err := nilQuantity.UnmarshalXML(xml.NewDecoder(strings.NewReader("<quantity/>")), xml.StartElement{}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("nil XML receiver error = %v", err)
	}
	zero := Quantity{}
	if _, err := zero.MarshalJSON(); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero MarshalJSON error = %v", err)
	}
	if _, err := zero.MarshalText(); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero MarshalText error = %v", err)
	}
	if _, err := zero.Value(); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero Value error = %v", err)
	}
	if err := zero.UnmarshalJSON([]byte(`{"value":"1","unit":"m"} {}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("trailing JSON error = %v", err)
	}
	if err := zero.UnmarshalJSON([]byte(`{"value":"1","unit":"m"} x`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("malformed trailing JSON error = %v", err)
	}
	if err := zero.UnmarshalJSON([]byte(`{"value":"bad","unit":"m"}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("bad decimal error = %v", err)
	}
	if err := zero.UnmarshalJSON([]byte(`{"value":"1","unit":"bad"}`)); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("bad unit error = %v", err)
	}
	if err := zero.UnmarshalText([]byte("bad")); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("bad text error = %v", err)
	}
	if err := zero.Scan([]byte(`{"value":"1","unit":"m"}`)); err != nil {
		t.Fatalf("Scan([]byte) error = %v", err)
	}
	if err := zero.Scan(nil); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("Scan(nil) error = %v", err)
	}
	if err := (Quantity{}).MarshalXML(xml.NewEncoder(&strings.Builder{}), xml.StartElement{}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero MarshalXML error = %v", err)
	}
	if err := (&Quantity{}).UnmarshalXML(xml.NewDecoder(strings.NewReader("<quantity><value>")), xml.StartElement{Name: xml.Name{Local: "quantity"}}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("malformed quantity XML error = %v", err)
	}
	if _, err := zero.Format(FormatOptions{Unit: Metre, Conversion: ExactConversion(), Scale: 0, Rounding: decimal.HalfEven, Separator: "\n"}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("Format(separator) error = %v", err)
	}
	if _, err := (Quantity{}).Format(FormatOptions{Unit: Metre, Conversion: ExactConversion(), Scale: 0, Rounding: decimal.HalfEven, Separator: " "}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("Format(zero) error = %v", err)
	}
	quantity := MustNew(decimal.New(1), Metre)
	if _, err := quantity.Format(FormatOptions{Unit: Metre, Conversion: ExactConversion(), Scale: 0, Rounding: decimal.RoundingMode(255), Separator: " "}); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Format(rounding) error = %v", err)
	}

	var dimensions Dimensions
	if err := dimensions.UnmarshalJSON([]byte(`{} {}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("dimensions trailing JSON error = %v", err)
	}
	if err := dimensions.UnmarshalJSON([]byte(`{"unknown":1}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("dimensions unknown JSON error = %v", err)
	}
	if err := dimensions.UnmarshalJSON([]byte(strings.Repeat(" ", MaxSerializedBytes+1))); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("dimensions oversize error = %v", err)
	}
	var nilDimensions *Dimensions
	if err := nilDimensions.UnmarshalJSON([]byte(`{}`)); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("nil dimensions JSON error = %v", err)
	}
	if err := nilDimensions.UnmarshalXML(xml.NewDecoder(strings.NewReader("<dimensions/>")), xml.StartElement{}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("nil dimensions XML error = %v", err)
	}
	if _, err := json.Marshal(Dimensions{}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero dimensions MarshalJSON error = %v", err)
	}
	if err := (Dimensions{}).MarshalXML(xml.NewEncoder(&strings.Builder{}), xml.StartElement{}); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("zero dimensions MarshalXML error = %v", err)
	}
	if err := (&Dimensions{}).UnmarshalXML(xml.NewDecoder(strings.NewReader("<dimensions><length>")), xml.StartElement{Name: xml.Name{Local: "dimensions"}}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("malformed dimensions XML error = %v", err)
	}
	invalidDimensionsXML := `<dimensions><length><value>1</value><unit>m</unit></length><width><value>1</value><unit>m</unit></width><height><value>1</value><unit>m</unit></height><quantity>0</quantity></dimensions>`
	if err := xml.Unmarshal([]byte(invalidDimensionsXML), &dimensions); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("invalid dimensions XML error = %v", err)
	}
}

func TestInternalStrictCodecTokenPaths(t *testing.T) {
	t.Parallel()
	start := xml.StartElement{Name: xml.Name{Local: "quantity"}}
	if _, err := decodeQuantityXML(xml.NewDecoder(strings.NewReader("")), start); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("empty quantity token stream error = %v", err)
	}
	start.Name.Local = "dimensions"
	if _, err := decodeDimensionsXML(xml.NewDecoder(strings.NewReader("")), start); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("empty dimensions token stream error = %v", err)
	}

	for _, payload := range []string{
		``,
		`{"`,
		`{"value":`,
		`{"value":"1","unit":"m"`,
	} {
		var quantity Quantity
		if err := quantity.UnmarshalJSON([]byte(payload)); !errors.Is(err, ErrInvalidQuantity) {
			t.Fatalf("quantity JSON %q error = %v", payload, err)
		}
	}

	for _, payload := range []string{
		`<quantity>text<value>1</value><unit>m</unit></quantity>`,
		`<quantity><!-- accepted --><value>1</value><unit>m</unit></quantity>`,
		`<quantity><?policy strict?><value>1</value><unit>m</unit></quantity>`,
		`<quantity><value>1`,
		`<quantity><value>1</value><unit>m`,
	} {
		var quantity Quantity
		err := xml.Unmarshal([]byte(payload), &quantity)
		if strings.Contains(payload, "accepted") {
			if err != nil {
				t.Fatalf("quantity XML comment error = %v", err)
			}
		} else if !errors.Is(err, ErrInvalidQuantity) {
			t.Fatalf("quantity XML %q error = %v", payload, err)
		}
	}

	validSide := `<value>1</value><unit>m</unit>`
	for _, payload := range []string{
		`<dimensions>text</dimensions>`,
		`<dimensions><!-- accepted --><length>` + validSide + `</length><width>` + validSide + `</width><height>` + validSide + `</height><quantity>1</quantity></dimensions>`,
		`<dimensions><?policy strict?></dimensions>`,
		`<dimensions><length><value>1`,
		`<dimensions><quantity>bad</quantity></dimensions>`,
	} {
		var dimensions Dimensions
		err := xml.Unmarshal([]byte(payload), &dimensions)
		if strings.Contains(payload, "accepted") {
			if err != nil {
				t.Fatalf("dimensions XML comment error = %v", err)
			}
		} else if !errors.Is(err, ErrInvalidQuantity) {
			t.Fatalf("dimensions XML %q error = %v", payload, err)
		}
	}
}

func TestInternalProfileBoundaryPaths(t *testing.T) {
	t.Parallel()

	profile := Profile{aliases: map[string]Unit{"bad": "unknown"}}
	if _, err := profile.Resolve(""); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("empty alias error = %v", err)
	}
	if _, err := profile.Resolve(strings.Repeat("x", MaxAliasBytes+1)); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("large alias error = %v", err)
	}
	if _, err := profile.Resolve("bad"); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("invalid mapped unit error = %v", err)
	}
	if _, err := Parse(" 1 m", SymbolProfile()); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("whitespace parse error = %v", err)
	}
	if _, err := Parse("1", SymbolProfile()); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("missing unit parse error = %v", err)
	}
	if _, err := Parse("bad m", SymbolProfile()); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("bad amount parse error = %v", err)
	}

	// Keep context imported in the internal suite so cancellation-sensitive
	// dependency paths remain available to future tight-limit fixtures.
	_ = context.Background()
}

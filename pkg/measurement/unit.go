package measurement

import (
	"errors"
	"fmt"
	"sort"

	"github.com/faustbrian/golib/pkg/math/decimal"
)

// Package errors classify validation, unit, dimension, and context failures.
var (
	// ErrInvalidQuantity reports malformed, nonpositive, or unbounded input.
	ErrInvalidQuantity      = errors.New("measurement: invalid quantity")
	ErrUnknownUnit          = errors.New("measurement: unknown unit")
	ErrDimensionMismatch    = errors.New("measurement: dimension mismatch")
	ErrUnsupportedDimension = errors.New("measurement: unsupported dimension")
	ErrAffineArithmetic     = errors.New("measurement: affine temperature arithmetic")
	ErrInvalidContext       = errors.New("measurement: invalid conversion context")
)

// Unit is a stable identity for a supported measurement unit.
type Unit string

func (u Unit) String() string { return string(u) }

// Supported units carry stable symbols and dimension identity.
const (
	// One is the canonical dimensionless unit.
	One Unit = "1"

	Millimetre Unit = "mm"
	Centimetre Unit = "cm"
	Metre      Unit = "m"
	Kilometre  Unit = "km"
	Inch       Unit = "in"
	Foot       Unit = "ft"
	Yard       Unit = "yd"

	SquareMillimetre Unit = "mm2"
	SquareCentimetre Unit = "cm2"
	SquareMetre      Unit = "m2"
	SquareInch       Unit = "in2"
	SquareFoot       Unit = "ft2"

	CubicMillimetre Unit = "mm3"
	CubicCentimetre Unit = "cm3"
	CubicMetre      Unit = "m3"
	Millilitre      Unit = "mL"
	Litre           Unit = "L"
	CubicInch       Unit = "in3"
	CubicFoot       Unit = "ft3"

	Milligram Unit = "mg"
	Gram      Unit = "g"
	Kilogram  Unit = "kg"
	Tonne     Unit = "t"
	Ounce     Unit = "oz"
	Pound     Unit = "lb"

	Kelvin     Unit = "K"
	Celsius    Unit = "degC"
	Fahrenheit Unit = "degF"

	KilogramPerCubicMetre  Unit = "kg/m3"
	GramPerCubicCentimetre Unit = "g/cm3"

	LoadingMetre Unit = "ldm"
)

type unitDefinition struct {
	dimension   Dimension
	numerator   decimal.Decimal
	denominator decimal.Decimal
	preOffset   decimal.Decimal
}

var unitDefinitions = map[Unit]unitDefinition{
	One: {Dimensionless, decimal.New(1), decimal.New(1), decimal.New(0)},

	Millimetre: {LengthDimension, decimal.New(1), decimal.New(1000), decimal.New(0)},
	Centimetre: {LengthDimension, decimal.New(1), decimal.New(100), decimal.New(0)},
	Metre:      {LengthDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	Kilometre:  {LengthDimension, decimal.New(1000), decimal.New(1), decimal.New(0)},
	Inch:       {LengthDimension, decimal.MustParse("0.0254"), decimal.New(1), decimal.New(0)},
	Foot:       {LengthDimension, decimal.MustParse("0.3048"), decimal.New(1), decimal.New(0)},
	Yard:       {LengthDimension, decimal.MustParse("0.9144"), decimal.New(1), decimal.New(0)},

	SquareMillimetre: {AreaDimension, decimal.New(1), decimal.New(1_000_000), decimal.New(0)},
	SquareCentimetre: {AreaDimension, decimal.New(1), decimal.New(10_000), decimal.New(0)},
	SquareMetre:      {AreaDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	SquareInch:       {AreaDimension, decimal.MustParse("0.00064516"), decimal.New(1), decimal.New(0)},
	SquareFoot:       {AreaDimension, decimal.MustParse("0.09290304"), decimal.New(1), decimal.New(0)},

	CubicMillimetre: {VolumeDimension, decimal.New(1), decimal.New(1_000_000_000), decimal.New(0)},
	CubicCentimetre: {VolumeDimension, decimal.New(1), decimal.New(1_000_000), decimal.New(0)},
	CubicMetre:      {VolumeDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	Millilitre:      {VolumeDimension, decimal.New(1), decimal.New(1_000_000), decimal.New(0)},
	Litre:           {VolumeDimension, decimal.New(1), decimal.New(1000), decimal.New(0)},
	CubicInch:       {VolumeDimension, decimal.MustParse("0.000016387064"), decimal.New(1), decimal.New(0)},
	CubicFoot:       {VolumeDimension, decimal.MustParse("0.028316846592"), decimal.New(1), decimal.New(0)},

	Milligram: {MassDimension, decimal.New(1), decimal.New(1_000_000), decimal.New(0)},
	Gram:      {MassDimension, decimal.New(1), decimal.New(1000), decimal.New(0)},
	Kilogram:  {MassDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	Tonne:     {MassDimension, decimal.New(1000), decimal.New(1), decimal.New(0)},
	Ounce:     {MassDimension, decimal.MustParse("0.028349523125"), decimal.New(1), decimal.New(0)},
	Pound:     {MassDimension, decimal.MustParse("0.45359237"), decimal.New(1), decimal.New(0)},

	Kelvin:     {TemperatureDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	Celsius:    {TemperatureDimension, decimal.New(1), decimal.New(1), decimal.MustParse("273.15")},
	Fahrenheit: {TemperatureDimension, decimal.New(5), decimal.New(9), decimal.MustParse("459.67")},

	KilogramPerCubicMetre:  {DensityDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
	GramPerCubicCentimetre: {DensityDimension, decimal.New(1000), decimal.New(1), decimal.New(0)},

	LoadingMetre: {LoadingMetreDimension, decimal.New(1), decimal.New(1), decimal.New(0)},
}

var canonicalUnits = map[Dimension]Unit{
	Dimensionless:         One,
	LengthDimension:       Metre,
	AreaDimension:         SquareMetre,
	VolumeDimension:       CubicMetre,
	MassDimension:         Kilogram,
	TemperatureDimension:  Kelvin,
	DensityDimension:      KilogramPerCubicMetre,
	LoadingMetreDimension: LoadingMetre,
}

// Dimension reports the unit's physical dimension.
func (u Unit) Dimension() (Dimension, error) {
	definition, ok := unitDefinitions[u]
	if !ok {
		return Dimensionless, fmt.Errorf("%w: %q", ErrUnknownUnit, u)
	}

	return definition.dimension, nil
}

func definitionFor(unit Unit) (unitDefinition, error) {
	definition, ok := unitDefinitions[unit]
	if !ok {
		return unitDefinition{}, fmt.Errorf("%w: %q", ErrUnknownUnit, unit)
	}

	return definition, nil
}

// Units returns a sorted copy of every supported unit for dimension.
func Units(dimension Dimension) []Unit {
	if _, ok := canonicalUnits[dimension]; !ok {
		return nil
	}
	units := make([]Unit, 0)
	for unit, definition := range unitDefinitions {
		if definition.dimension == dimension {
			units = append(units, unit)
		}
	}
	sort.Slice(units, func(left, right int) bool { return units[left] < units[right] })

	return units
}

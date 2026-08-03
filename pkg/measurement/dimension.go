package measurement

import "fmt"

// Dimension is a closed v1 physical-dimension identity. Loading metre has a
// distinct semantic identity even though its displayed unit contains metres.
type Dimension uint8

// Supported dimensions form the closed v1 physical ontology.
const (
	// Dimensionless identifies ratios and counts with unit one.
	Dimensionless Dimension = iota
	LengthDimension
	AreaDimension
	VolumeDimension
	MassDimension
	TemperatureDimension
	DensityDimension
	LoadingMetreDimension
)

func (d Dimension) String() string {
	names := [...]string{
		"dimensionless",
		"length",
		"area",
		"volume",
		"mass",
		"temperature",
		"density",
		"loading-metre",
	}
	if int(d) < len(names) {
		return names[d]
	}

	return fmt.Sprintf("Dimension(%d)", d)
}

func (d Dimension) multiply(other Dimension) (Dimension, error) {
	switch d { //nolint:exhaustive // omitted dimensions fall through to rejection
	case Dimensionless:
		if !other.supportsDerivedArithmetic() {
			return Dimensionless, ErrUnsupportedDimension
		}

		return other, nil
	case LengthDimension:
		switch other { //nolint:exhaustive // only supported length products return
		case Dimensionless:
			return LengthDimension, nil
		case LengthDimension:
			return AreaDimension, nil
		case AreaDimension:
			return VolumeDimension, nil
		}
	case AreaDimension:
		if other == Dimensionless {
			return AreaDimension, nil
		}
		if other == LengthDimension {
			return VolumeDimension, nil
		}
	case VolumeDimension:
		if other == Dimensionless {
			return VolumeDimension, nil
		}
		if other == DensityDimension {
			return MassDimension, nil
		}
	case DensityDimension:
		if other == Dimensionless {
			return DensityDimension, nil
		}
		if other == VolumeDimension {
			return MassDimension, nil
		}
	case MassDimension, TemperatureDimension:
		if other == Dimensionless {
			return d, nil
		}
	}

	return Dimensionless, ErrUnsupportedDimension
}

func (d Dimension) divide(other Dimension) (Dimension, error) {
	switch other { //nolint:exhaustive // only identity cases return early
	case Dimensionless:
		if !d.supportsDerivedArithmetic() {
			return Dimensionless, ErrUnsupportedDimension
		}

		return d, nil
	case d:
		if !d.supportsDerivedArithmetic() {
			return Dimensionless, ErrUnsupportedDimension
		}

		return Dimensionless, nil
	}
	switch d { //nolint:exhaustive // omitted dimensions fall through to rejection
	case AreaDimension:
		if other == LengthDimension {
			return LengthDimension, nil
		}
	case VolumeDimension:
		switch other { //nolint:exhaustive // only supported volume quotients return
		case LengthDimension:
			return AreaDimension, nil
		case AreaDimension:
			return LengthDimension, nil
		}
	case MassDimension:
		switch other { //nolint:exhaustive // only supported mass quotients return
		case VolumeDimension:
			return DensityDimension, nil
		case DensityDimension:
			return VolumeDimension, nil
		}
	}

	return Dimensionless, ErrUnsupportedDimension
}

func (d Dimension) supportsDerivedArithmetic() bool {
	switch d {
	case Dimensionless, LengthDimension, AreaDimension, VolumeDimension,
		MassDimension, TemperatureDimension, DensityDimension:
		return true
	case LoadingMetreDimension:
		return false
	default:
		return false
	}
}

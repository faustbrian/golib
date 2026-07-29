// Package leafvector defines the experimental profile's canonical field inputs
// for stems and fixed-size leaf values. It does not construct commitments.
package leafvector

const (
	// ExtensionMarkerIndex is the extension-presence input position.
	ExtensionMarkerIndex byte = iota
	// StemIndex is the stem scalar input position.
	StemIndex
	// C1HashIndex is the first suffix commitment hash input position.
	C1HashIndex
	// C2HashIndex is the second suffix commitment hash input position.
	C2HashIndex
)

// Scalar is a canonical 32-byte little-endian field input.
//
// Values produced by this package are smaller than the profile's scalar-field
// modulus and therefore require no modular reduction.
type Scalar [32]byte

// Half identifies which suffix commitment contains a value.
type Half byte

const (
	// C1 contains suffixes 0 through 127.
	C1 Half = iota
	// C2 contains suffixes 128 through 255.
	C2
)

// ValueOpening identifies the two field inputs for one suffix.
type ValueOpening struct {
	Half      Half
	LowIndex  byte
	HighIndex byte
	Low       Scalar
	High      Scalar
}

// EncodePresent returns the canonical inputs for a present fixed-size value.
//
// The low half is interpreted little-endian and marked with 2^128. The high
// half is interpreted little-endian without a marker. This makes a present
// all-zero value distinct from absence.
func EncodePresent(suffix byte, value [32]byte) ValueOpening {
	opening := placement(suffix)
	copy(opening.Low[:16], value[:16])
	opening.Low[16] = 1
	copy(opening.High[:16], value[16:])

	return opening
}

// EncodeAbsent returns the canonical zero inputs for an absent suffix.
func EncodeAbsent(suffix byte) ValueOpening {
	return placement(suffix)
}

// EncodeStem returns the stem interpreted as a 31-byte little-endian scalar.
func EncodeStem(stem [31]byte) Scalar {
	var encoded Scalar
	copy(encoded[:31], stem[:])

	return encoded
}

// EncodeExtensionMarker returns the canonical scalar one.
func EncodeExtensionMarker() Scalar {
	var marker Scalar
	marker[0] = 1

	return marker
}

func placement(suffix byte) ValueOpening {
	half := C1
	if suffix >= 128 {
		half = C2
	}
	lowIndex := (suffix & 127) * 2

	return ValueOpening{
		Half:      half,
		LowIndex:  lowIndex,
		HighIndex: lowIndex + 1,
	}
}

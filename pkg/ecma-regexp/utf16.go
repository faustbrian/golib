package ecmascript

import (
	"errors"
	"unicode/utf16"
)

var ErrUnpairedSurrogate = errors.New("UTF-16 value contains an unpaired surrogate")

// UTF16String preserves every ECMAScript string code unit, including lone
// surrogates that cannot be represented as a Unicode scalar Go string.
type UTF16String struct {
	units []uint16
}

func newUTF16String(units []uint16) UTF16String {
	return UTF16String{units: append([]uint16(nil), units...)}
}

// UTF16FromString converts a scalar Go string to its exact UTF-16 form.
func UTF16FromString(value string) UTF16String {
	return newUTF16String(utf16.Encode([]rune(value)))
}

// UTF16FromUnits copies exact ECMAScript string code units, including lone
// surrogates that cannot be represented by a scalar Go string.
func UTF16FromUnits(units []uint16) UTF16String {
	return newUTF16String(units)
}

// Units returns an independent copy of the exact UTF-16 code units.
func (s UTF16String) Units() []uint16 {
	return append([]uint16(nil), s.units...)
}

// GoString converts the value only when every surrogate is paired.
func (s UTF16String) GoString() (string, error) {
	for index := 0; index < len(s.units); index++ {
		unit := s.units[index]
		if isHighSurrogate(unit) {
			if index+1 >= len(s.units) || !isLowSurrogate(s.units[index+1]) {
				return "", ErrUnpairedSurrogate
			}
			index++
		} else if isLowSurrogate(unit) {
			return "", ErrUnpairedSurrogate
		}
	}

	return string(utf16.Decode(s.units)), nil
}

// LossyString explicitly replaces each unpaired surrogate with U+FFFD.
func (s UTF16String) LossyString() string {
	return string(utf16.Decode(s.units))
}

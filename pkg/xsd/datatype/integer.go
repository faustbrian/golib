package datatype

import (
	"errors"
	"fmt"
	"math/big"
)

var ErrUnknownType = errors.New("xsd datatype: unknown built-in type")

// Integer is an immutable arbitrary-precision XML Schema integer value.
type Integer struct {
	value *big.Int
}

// ParseInteger applies integer's fixed collapse whitespace rule and parses its
// lexical space without machine-width or floating-point conversion.
func ParseInteger(lexical string) (Integer, error) {
	if len(lexical) > maxDecimalLexicalBytes {
		return Integer{}, fmt.Errorf("%w: integer exceeds %d bytes", ErrLimitExceeded, maxDecimalLexicalBytes)
	}
	lexical = trimXMLSpace(lexical)
	if lexical == "" {
		return Integer{}, invalidInteger(lexical)
	}
	unsigned := lexical
	if unsigned[0] == '+' || unsigned[0] == '-' {
		unsigned = unsigned[1:]
	}
	if unsigned == "" || !asciiDigits(unsigned) {
		return Integer{}, invalidInteger(lexical)
	}
	value, _ := new(big.Int).SetString(lexical, 10)
	return Integer{value: value}, nil
}

func invalidInteger(lexical string) error {
	return fmt.Errorf("%w: %q is not integer", ErrInvalidLexical, lexical)
}

// String returns the canonical integer representation.
func (i Integer) String() string { return integerValue(i).String() }

// Compare returns -1, 0, or 1 according to the integer value ordering.
func (i Integer) Compare(other Integer) int {
	return integerValue(i).Cmp(integerValue(other))
}

func integerValue(integer Integer) *big.Int {
	if integer.value == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(integer.value)
}

type integerRange struct {
	minimum *big.Int
	maximum *big.Int
}

var builtInIntegerRanges = map[string]integerRange{
	"integer":            {},
	"nonPositiveInteger": {maximum: big.NewInt(0)},
	"negativeInteger":    {maximum: mustBigInteger("-1")},
	"long":               {minimum: mustBigInteger("-9223372036854775808"), maximum: mustBigInteger("9223372036854775807")},
	"int":                {minimum: mustBigInteger("-2147483648"), maximum: mustBigInteger("2147483647")},
	"short":              {minimum: mustBigInteger("-32768"), maximum: mustBigInteger("32767")},
	"byte":               {minimum: mustBigInteger("-128"), maximum: mustBigInteger("127")},
	"nonNegativeInteger": {minimum: big.NewInt(0)},
	"unsignedLong":       {minimum: big.NewInt(0), maximum: mustBigInteger("18446744073709551615")},
	"unsignedInt":        {minimum: big.NewInt(0), maximum: mustBigInteger("4294967295")},
	"unsignedShort":      {minimum: big.NewInt(0), maximum: big.NewInt(65535)},
	"unsignedByte":       {minimum: big.NewInt(0), maximum: big.NewInt(255)},
	"positiveInteger":    {minimum: big.NewInt(1)},
}

// ValidateBuiltInInteger applies the range of integer and its twelve built-in
// derived integer datatypes.
func ValidateBuiltInInteger(name string, value Integer) error {
	rangeValue, ok := builtInIntegerRanges[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownType, name)
	}
	actual := integerValue(value)
	if rangeValue.minimum != nil && actual.Cmp(rangeValue.minimum) < 0 {
		return fmt.Errorf("%w: %s rejects %s", ErrFacetViolation, name, value.String())
	}
	if rangeValue.maximum != nil && actual.Cmp(rangeValue.maximum) > 0 {
		return fmt.Errorf("%w: %s rejects %s", ErrFacetViolation, name, value.String())
	}
	return nil
}

func mustBigInteger(value string) *big.Int {
	integer, _ := new(big.Int).SetString(value, 10)
	return integer
}

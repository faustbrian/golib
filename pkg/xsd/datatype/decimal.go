package datatype

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

var (
	ErrInvalidLexical = errors.New("xsd datatype: invalid lexical value")
	ErrFacetViolation = errors.New("xsd datatype: facet violation")
	ErrLimitExceeded  = errors.New("xsd datatype: resource limit exceeded")
)

const maxDecimalLexicalBytes = 1 << 20

// Decimal is an immutable arbitrary-precision XML Schema decimal value.
type Decimal struct {
	coefficient *big.Int
	scale       int
}

// ParseDecimal applies decimal's fixed collapse whitespace rule and parses
// its lexical space without floating-point conversion.
func ParseDecimal(lexical string) (Decimal, error) {
	if len(lexical) > maxDecimalLexicalBytes {
		return Decimal{}, fmt.Errorf("%w: decimal exceeds %d bytes", ErrLimitExceeded, maxDecimalLexicalBytes)
	}
	lexical = trimXMLSpace(lexical)
	if lexical == "" {
		return Decimal{}, invalidDecimal(lexical)
	}

	negative := false
	switch lexical[0] {
	case '+':
		lexical = lexical[1:]
	case '-':
		negative = true
		lexical = lexical[1:]
	}
	if lexical == "" {
		return Decimal{}, invalidDecimal(lexical)
	}

	dot := strings.IndexByte(lexical, '.')
	if dot != -1 && strings.IndexByte(lexical[dot+1:], '.') != -1 {
		return Decimal{}, invalidDecimal(lexical)
	}
	integerPart := lexical
	fractionPart := ""
	if dot != -1 {
		integerPart = lexical[:dot]
		fractionPart = lexical[dot+1:]
	}
	if integerPart == "" && fractionPart == "" {
		return Decimal{}, invalidDecimal(lexical)
	}
	if !asciiDigits(integerPart) || !asciiDigits(fractionPart) {
		return Decimal{}, invalidDecimal(lexical)
	}

	digits := integerPart + fractionPart
	coefficient, _ := new(big.Int).SetString(digits, 10)
	scale := len(fractionPart)
	for scale > 0 && coefficient.Sign() != 0 {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(coefficient, big.NewInt(10), remainder)
		if remainder.Sign() != 0 {
			break
		}
		coefficient = quotient
		scale--
	}
	if coefficient.Sign() == 0 {
		scale = 0
		negative = false
	}
	if negative {
		coefficient.Neg(coefficient)
	}
	return Decimal{coefficient: coefficient, scale: scale}, nil
}

func invalidDecimal(lexical string) error {
	return fmt.Errorf("%w: %q is not decimal", ErrInvalidLexical, lexical)
}

func asciiDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func trimXMLSpace(value string) string {
	return strings.TrimFunc(value, func(character rune) bool {
		return character == ' ' || character == '\t' || character == '\n' || character == '\r'
	})
}

// String returns the XML Schema canonical representation of the value.
func (d Decimal) String() string {
	if d.coefficient == nil || d.coefficient.Sign() == 0 {
		return "0.0"
	}
	absolute := new(big.Int).Abs(d.coefficient).String()
	negative := ""
	if d.coefficient.Sign() < 0 {
		negative = "-"
	}
	if d.scale == 0 {
		return negative + absolute + ".0"
	}
	if len(absolute) <= d.scale {
		absolute = strings.Repeat("0", d.scale-len(absolute)+1) + absolute
	}
	split := len(absolute) - d.scale
	return negative + absolute[:split] + "." + absolute[split:]
}

// Compare returns -1, 0, or 1 according to the decimal value ordering.
func (d Decimal) Compare(other Decimal) int {
	left := coefficient(d)
	right := coefficient(other)
	if d.scale < other.scale {
		left.Mul(left, powerOfTen(other.scale-d.scale))
	} else if other.scale < d.scale {
		right.Mul(right, powerOfTen(d.scale-other.scale))
	}
	return left.Cmp(right)
}

func coefficient(value Decimal) *big.Int {
	if value.coefficient == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(value.coefficient)
}

func powerOfTen(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}

// TotalDigits reports the smallest number of decimal digits needed for the
// value, excluding sign and decimal point.
func (d Decimal) TotalDigits() int {
	digits := new(big.Int).Abs(coefficient(d)).String()
	if digits == "0" {
		return 1
	}
	return len(digits)
}

// FractionDigits reports the smallest number of fractional digits needed for
// the value.
func (d Decimal) FractionDigits() int { return d.scale }

// DecimalFacets contains decimal's ordered and digit constraining facets.
// A zero digit count means the facet is absent.
type DecimalFacets struct {
	MinInclusive   *Decimal
	MinExclusive   *Decimal
	MaxInclusive   *Decimal
	MaxExclusive   *Decimal
	TotalDigits    int
	FractionDigits *int
	Enumeration    []Decimal
}

// Validate checks a decimal value against every configured facet.
func (f DecimalFacets) Validate(value Decimal) error {
	if f.MinInclusive != nil && value.Compare(*f.MinInclusive) < 0 {
		return facetViolation("minInclusive", value)
	}
	if f.MinExclusive != nil && value.Compare(*f.MinExclusive) <= 0 {
		return facetViolation("minExclusive", value)
	}
	if f.MaxInclusive != nil && value.Compare(*f.MaxInclusive) > 0 {
		return facetViolation("maxInclusive", value)
	}
	if f.MaxExclusive != nil && value.Compare(*f.MaxExclusive) >= 0 {
		return facetViolation("maxExclusive", value)
	}
	if f.TotalDigits > 0 && value.TotalDigits() > f.TotalDigits {
		return facetViolation("totalDigits", value)
	}
	if f.FractionDigits != nil && value.FractionDigits() > *f.FractionDigits {
		return facetViolation("fractionDigits", value)
	}
	if len(f.Enumeration) > 0 {
		for _, allowed := range f.Enumeration {
			if value.Compare(allowed) == 0 {
				return nil
			}
		}
		return facetViolation("enumeration", value)
	}
	return nil
}

func facetViolation(name string, value Decimal) error {
	return fmt.Errorf("%w: %s rejects %s", ErrFacetViolation, name, value.String())
}

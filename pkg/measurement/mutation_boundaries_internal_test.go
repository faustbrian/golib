package measurement

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestDimensionClosedBoundary(t *testing.T) {
	t.Parallel()

	invalid := Dimension(LoadingMetreDimension + 1)
	if got := invalid.String(); got != "Dimension(8)" {
		t.Fatalf("invalid.String() = %q, want Dimension(8)", got)
	}

	for _, test := range []struct {
		name  string
		apply func() error
	}{
		{"multiply-left-loading-metre", func() error { _, err := LoadingMetreDimension.multiply(Dimensionless); return err }},
		{"multiply-right-loading-metre", func() error { _, err := Dimensionless.multiply(LoadingMetreDimension); return err }},
		{"multiply-left-out-of-range", func() error { _, err := invalid.multiply(Dimensionless); return err }},
		{"multiply-right-out-of-range", func() error { _, err := Dimensionless.multiply(invalid); return err }},
		{"divide-left-loading-metre", func() error { _, err := LoadingMetreDimension.divide(Dimensionless); return err }},
		{"divide-right-loading-metre", func() error { _, err := Dimensionless.divide(LoadingMetreDimension); return err }},
		{"divide-left-out-of-range", func() error { _, err := invalid.divide(Dimensionless); return err }},
		{"divide-right-out-of-range", func() error { _, err := Dimensionless.divide(invalid); return err }},
		{"divide-both-out-of-range", func() error { _, err := invalid.divide(invalid); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.apply(); !errors.Is(err, ErrUnsupportedDimension) {
				t.Fatalf("error = %v, want ErrUnsupportedDimension", err)
			}
		})
	}
}

func TestPackageCountAcceptsExactMaximum(t *testing.T) {
	t.Parallel()

	one := MustNew(decimal.New(1), Metre)
	if _, err := NewDimensions(one, one, one, MaxPackageQuantity); err != nil {
		t.Fatalf("NewDimensions(MaxPackageQuantity) error = %v", err)
	}
	if _, err := one.Times(MaxPackageQuantity); err != nil {
		t.Fatalf("Times(MaxPackageQuantity) error = %v", err)
	}
}

func TestSerializedInputsAcceptExactMaximum(t *testing.T) {
	t.Parallel()

	pad := func(data []byte) []byte {
		t.Helper()
		if len(data) > MaxSerializedBytes {
			t.Fatalf("fixture length = %d, exceeds maximum", len(data))
		}

		return append(data, bytes.Repeat([]byte(" "), MaxSerializedBytes-len(data))...)
	}

	var quantity Quantity
	if err := quantity.UnmarshalJSON(pad([]byte(`{"value":"1","unit":"m"}`))); err != nil {
		t.Fatalf("Quantity.UnmarshalJSON(exact maximum) error = %v", err)
	}

	one := MustNew(decimal.New(1), Metre)
	dimensions, err := NewDimensions(one, one, one, 1)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}
	data, err := json.Marshal(dimensions)
	if err != nil {
		t.Fatalf("json.Marshal(Dimensions) error = %v", err)
	}
	var decoded Dimensions
	if err := decoded.UnmarshalJSON(pad(data)); err != nil {
		t.Fatalf("Dimensions.UnmarshalJSON(exact maximum) error = %v", err)
	}
}

func TestFormatOptionBoundaries(t *testing.T) {
	t.Parallel()

	quantity := MustNew(decimal.New(1), Metre)
	separator := strings.Repeat("x", 16)
	formatted, err := quantity.Format(FormatOptions{
		Unit:       Metre,
		Conversion: ExactConversion(),
		Scale:      0,
		Rounding:   decimal.HalfEven,
		Separator:  separator,
	})
	if err != nil {
		t.Fatalf("Format(16-byte separator) error = %v", err)
	}
	if formatted != "1"+separator+"m" {
		t.Fatalf("Format(16-byte separator) = %q", formatted)
	}

	invalidSeparators := []string{
		strings.Repeat("x", 17),
		string([]byte{utf8.RuneSelf}),
		"\r",
		"\n",
		"\x00",
	}
	for _, invalid := range invalidSeparators {
		if _, err := quantity.Format(FormatOptions{Unit: Metre, Separator: invalid}); !errors.Is(err, ErrInvalidQuantity) {
			t.Fatalf("Format(separator %q) error = %v, want ErrInvalidQuantity", invalid, err)
		}
	}
	if _, err := quantity.Format(FormatOptions{Separator: " "}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("Format(empty unit) error = %v, want ErrInvalidQuantity", err)
	}
}

func TestStrictJSONBoundaryDiagnostics(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"[]", "1"} {
		var quantity Quantity
		err := quantity.UnmarshalJSON([]byte(input))
		if err == nil || !strings.Contains(err.Error(), "expected JSON object") {
			t.Fatalf("UnmarshalJSON(%s) error = %v, want object diagnostic", input, err)
		}
	}

	var quantity Quantity
	err := quantity.UnmarshalJSON([]byte(`{"value":"1","unit":"m"} {}`))
	if err == nil || !strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("trailing JSON error = %v, want trailing-value diagnostic", err)
	}
}

func TestDimensionsXMLStartElementBoundaries(t *testing.T) {
	t.Parallel()

	one := MustNew(decimal.New(1), Metre)
	dimensions, err := NewDimensions(one, one, one, 1)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}

	for _, test := range []struct {
		start string
		want  string
	}{
		{"", "dimensions"},
		{"Dimensions", "dimensions"},
		{"parcel", "parcel"},
	} {
		var output bytes.Buffer
		encoder := xml.NewEncoder(&output)
		if err := dimensions.MarshalXML(encoder, xml.StartElement{Name: xml.Name{Local: test.start}}); err != nil {
			t.Fatalf("MarshalXML(%q) error = %v", test.start, err)
		}
		if err := encoder.Flush(); err != nil {
			t.Fatalf("Flush(%q) error = %v", test.start, err)
		}
		if !strings.HasPrefix(output.String(), "<"+test.want+">") {
			t.Fatalf("MarshalXML(%q) = %q, want <%s>", test.start, output.String(), test.want)
		}
	}
}

func TestProfileAndParseExactLimits(t *testing.T) {
	t.Parallel()

	aliases := make(map[string]Unit, MaxProfileAliases)
	for index := range MaxProfileAliases {
		aliases[strconv.Itoa(index)] = Metre
	}
	if _, err := NewProfile(aliases); err != nil {
		t.Fatalf("NewProfile(MaxProfileAliases) error = %v", err)
	}

	exactAlias := strings.Repeat("a", MaxAliasBytes)
	profile, err := NewProfile(map[string]Unit{exactAlias: Metre})
	if err != nil {
		t.Fatalf("NewProfile(MaxAliasBytes) error = %v", err)
	}
	unit, err := profile.Resolve(exactAlias)
	if err != nil || unit != Metre {
		t.Fatalf("Resolve(MaxAliasBytes) = %q, %v", unit, err)
	}

	exactText := strings.Repeat("1", MaxTextBytes-2) + " m"
	if _, err := Parse(exactText, SymbolProfile()); err != nil {
		t.Fatalf("Parse(MaxTextBytes) error = %v", err)
	}
}

func TestRoundedContextAcceptsExactExponentLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxExponentMagnitude = 2
	for _, scale := range []int32{2, -2} {
		context := RoundedConversion(scale, decimal.HalfEven).WithLimits(limits)
		if err := context.validate(); err != nil {
			t.Fatalf("RoundedConversion(%d).validate() error = %v", scale, err)
		}
	}
}

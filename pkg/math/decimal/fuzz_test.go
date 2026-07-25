package decimal_test

import (
	"context"
	"encoding/json"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func FuzzParseContextAndRoundTrip(f *testing.F) {
	for _, seed := range []string{"0", "-1.25", "1e10", "0.000001"} {
		f.Add(seed, uint8(9))
	}
	f.Fuzz(func(t *testing.T, input string, precisionByte uint8) {
		limits := gomath.DefaultLimits()
		limits.MaxInputDigits = 256
		limits.MaxOutputDigits = 512
		limits.MaxExponentMagnitude = 256
		value, err := decimal.ParseWithOptions(input, decimal.ParseOptions{
			AllowExponent: precisionByte&8 != 0, AllowPlus: precisionByte&16 != 0,
			AllowUnderscores:  precisionByte&32 != 0,
			AllowLeadingZeros: precisionByte&64 != 0,
			AllowWhitespace:   precisionByte&128 != 0,
			Limits:            limits,
		})
		if err != nil {
			return
		}
		roundTripLimits := limits
		roundTripLimits.MaxInputDigits = limits.MaxOutputDigits
		roundTrip, err := decimal.ParseWithOptions(value.String(), decimal.ParseOptions{
			AllowLeadingZeros: true, Limits: roundTripLimits,
		})
		if err != nil || !roundTrip.Equal(value) {
			t.Fatalf("round trip changed %s: %v", value, err)
		}
		precision := uint32(precisionByte%8 + 1)
		result, err := (decimal.Context{
			Precision: precision, MinExponent: -100, MaxExponent: 100,
			Rounding: decimal.HalfEven, Limits: limits,
		}).Add(context.Background(), value, value.Neg())
		if err != nil || !result.Value.IsZero() {
			t.Fatalf("additive inverse failed: %s, %v", result.Value, err)
		}
		data, err := json.Marshal(value)
		if err != nil || len(data) < 2 || data[0] != '"' {
			t.Fatalf("unsafe JSON encoding %q: %v", data, err)
		}
		var decoded decimal.Decimal
		if err := decoded.UnmarshalJSON(data); err != nil || !decoded.Equal(value) {
			t.Fatalf("JSON round trip changed %s: %v", value, err)
		}
	})
}

func FuzzJSONDecoding(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`"0"`), []byte(`"-1.25"`), []byte(`1.25`),
		[]byte(`"1" true`), []byte(`null`), []byte(`"\u0661"`),
		{0xff, 0xfe, 0xfd},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		original := decimal.MustParse("123.45")
		decoded := original
		err := json.Unmarshal(input, &decoded)
		if err != nil {
			if !decoded.SameRepresentation(original) {
				t.Fatalf("failed JSON decode mutated destination to %s: %v", decoded, err)
			}
			return
		}
		encoded, err := json.Marshal(decoded)
		if err != nil || len(encoded) < 2 || encoded[0] != '"' {
			t.Fatalf("unsafe canonical JSON %q: %v", encoded, err)
		}
		var roundTrip decimal.Decimal
		if err := json.Unmarshal(encoded, &roundTrip); err != nil || !roundTrip.SameRepresentation(decoded) {
			t.Fatalf("JSON round trip changed %s: %v", decoded, err)
		}
	})
}

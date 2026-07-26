package integer_test

import (
	"context"
	"encoding/json"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func FuzzParseAndArithmetic(f *testing.F) {
	for _, seed := range []string{"0", "-1", "42", "999999999999999999", "+ff", "0b101", "1_000"} {
		f.Add(seed, uint8(10), uint8(0))
	}
	f.Fuzz(func(t *testing.T, input string, baseByte, flags uint8) {
		limits := gomath.DefaultLimits()
		limits.MaxInputDigits = 256
		base := int(baseByte%35) + 2
		value, err := integer.Parse(input, integer.ParseOptions{
			Base: base, AllowUnderscores: flags&1 != 0,
			AllowLeadingZeros: flags&2 != 0, AllowWhitespace: flags&4 != 0,
			RejectSign: flags&8 != 0, Limits: limits,
		})
		if err != nil {
			return
		}
		canonical := value.String()
		encodingLimits := limits
		encodingLimits.MaxInputDigits = len(canonical)
		roundTrip, err := integer.Parse(canonical, integer.ParseOptions{
			Base: 10, AllowLeadingZeros: true, Limits: encodingLimits,
		})
		if err != nil || !roundTrip.Equal(value) {
			t.Fatalf("round trip changed %s: %v", value, err)
		}
		zero, err := value.Add(context.Background(), value.Neg(), limits)
		if err != nil || zero.Sign() != 0 {
			t.Fatalf("additive inverse failed: %s, %v", zero, err)
		}
		data, err := json.Marshal(value)
		if err != nil || len(data) < 2 || data[0] != '"' {
			t.Fatalf("unsafe JSON encoding %q: %v", data, err)
		}
	})
}

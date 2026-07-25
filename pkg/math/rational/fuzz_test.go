package rational_test

import (
	"encoding/json"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func FuzzParseRoundTrip(f *testing.F) {
	for _, seed := range []string{"0", "1/2", "-22/7", "100/25"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		limits := gomath.DefaultLimits()
		limits.MaxInputDigits = 256
		value, err := rational.Parse(input, limits)
		if err != nil {
			return
		}
		roundTrip, err := rational.Parse(value.String(), limits)
		if err != nil || !roundTrip.Equal(value) {
			t.Fatalf("round trip changed %s: %v", value, err)
		}
		data, err := json.Marshal(value)
		if err != nil || len(data) < 2 || data[0] != '"' {
			t.Fatalf("unsafe JSON encoding %q: %v", data, err)
		}
	})
}

package bigfloat

import "testing"

func TestAAARoundTripDecimalDigitBounds(t *testing.T) {
	for _, test := range []struct {
		precision uint
		want      int
	}{
		{precision: 0, want: 1},
		{precision: 1, want: 2},
		{precision: 4, want: 3},
		{precision: 53, want: 17},
		{precision: 64, want: 21},
		{precision: 100, want: 32},
	} {
		if got := roundTripDecimalDigits(test.precision); got != test.want {
			t.Fatalf(
				"roundTripDecimalDigits(%d) = %d, want %d",
				test.precision,
				got,
				test.want,
			)
		}
	}
}

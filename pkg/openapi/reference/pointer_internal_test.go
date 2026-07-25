package reference

import (
	"strconv"
	"testing"
)

func TestArrayIndexAcceptsDigitAndIntegerEndpoints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		want int
	}{
		{raw: "0", want: 0},
		{raw: "9", want: 9},
		{raw: strconv.Itoa(int(^uint(0) >> 1)), want: int(^uint(0) >> 1)},
	} {
		got, valid := arrayIndex(test.raw)
		if !valid || got != test.want {
			t.Fatalf("arrayIndex(%q) = %d, %t", test.raw, got, valid)
		}
	}
}

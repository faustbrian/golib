package render

import (
	"math"
	"testing"
)

func TestBoundedProduct(t *testing.T) {
	for _, test := range []struct {
		name        string
		left, right int
		limit       int
		want        int
		ok          bool
	}{
		{name: "within limit", left: 3, right: 4, limit: 13, want: 12, ok: true},
		{name: "exact limit", left: 3, right: 4, limit: 12, want: 12, ok: true},
		{name: "over limit", left: 3, right: 4, limit: 11, want: 12, ok: false},
		{name: "maximum integer", left: math.MaxInt, right: 1, limit: math.MaxInt, want: math.MaxInt, ok: true},
		{name: "integer overflow", left: math.MaxInt, right: 2, limit: math.MaxInt, ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := boundedProduct(test.left, test.right, test.limit)
			if got != test.want || ok != test.ok {
				t.Fatalf("boundedProduct() = (%d, %t), want (%d, %t)", got, ok, test.want, test.ok)
			}
		})
	}
}

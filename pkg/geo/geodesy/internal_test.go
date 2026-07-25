package geodesy

import (
	"errors"
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestInternalNormalizationAndBoundingValidation(t *testing.T) {
	t.Parallel()

	if got := normalizeBearingDegrees(-1); got != 359 {
		t.Fatalf("normalizeBearingDegrees(-1) = %v, want 359", got)
	}
	if got := normalizeLongitudeDegrees(-541); got != 179 {
		t.Fatalf("normalizeLongitudeDegrees(-541) = %v, want 179", got)
	}

	tests := []struct {
		name              string
		west, south, east float64
		north             float64
	}{
		{name: "west", west: math.NaN()},
		{name: "south", west: 0, south: math.NaN()},
		{name: "east", west: 0, south: 0, east: math.NaN()},
		{name: "north", west: 0, south: 0, east: 0, north: math.NaN()},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := boundingBox(test.west, test.south, test.east, test.north); !errors.Is(err, geo.ErrRange) {
				t.Fatalf("boundingBox() error = %v, want ErrRange", err)
			}
		})
	}
}

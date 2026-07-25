package geo_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestLineStringCopiesInputAndEnforcesLimits(t *testing.T) {
	t.Parallel()

	coordinates := []geo.Coordinate{
		mustCoordinate(t, 0, 0, geo.WGS84()),
		mustCoordinate(t, 1, 1, geo.WGS84()),
	}
	line, err := geo.NewLineString(coordinates)
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	coordinates[0] = mustCoordinate(t, 10, 10, geo.WGS84())
	first, ok := line.At(0)
	if !ok {
		t.Fatal("At(0) = not found")
	}
	if got := first.Longitude().Degrees(); got != 0 {
		t.Fatalf("Coordinate(0) longitude = %v after input mutation, want 0", got)
	}

	_, err = geo.NewLineStringWithLimits(line.Coordinates(), geo.Limits{MaxPoints: 1})
	if !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("NewLineStringWithLimits() error = %v, want ErrTopology", err)
	}
}

func TestPolygonLocatesInsideOutsideBoundaryAndHole(t *testing.T) {
	t.Parallel()

	exterior := ring(t, [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}})
	hole := ring(t, [][2]float64{{3, 3}, {3, 7}, {7, 7}, {7, 3}, {3, 3}})
	polygon, err := geo.NewPolygon(exterior, [][]geo.Coordinate{hole})
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}

	for _, test := range []struct {
		name string
		lon  float64
		lat  float64
		want geo.Location
	}{
		{name: "inside shell", lon: 1, lat: 1, want: geo.Inside},
		{name: "outside shell", lon: 11, lat: 1, want: geo.Outside},
		{name: "shell boundary", lon: 0, lat: 5, want: geo.Boundary},
		{name: "inside hole", lon: 5, lat: 5, want: geo.Outside},
		{name: "hole boundary", lon: 3, lat: 5, want: geo.Boundary},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := polygon.Locate(mustCoordinate(t, test.lon, test.lat, geo.WGS84()))
			if err != nil {
				t.Fatalf("Locate() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Locate() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPolygonRejectsOpenRingsAndMixedCRS(t *testing.T) {
	t.Parallel()

	open := ring(t, [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}})
	_, err := geo.NewPolygon(open, nil)
	if !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("NewPolygon(open) error = %v, want ErrTopology", err)
	}

	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	mixed := ring(t, [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 0}})
	mixed[1] = mustCoordinate(t, 1, 0, webMercator)
	_, err = geo.NewPolygon(mixed, nil)
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("NewPolygon(mixed CRS) error = %v, want ErrCRS", err)
	}
}

func TestPolygonRejectsDegenerateSelfIntersectingAndOutsideHoleTopology(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		exterior [][2]float64
		holes    [][]geo.Coordinate
	}{
		{
			name:     "zero area",
			exterior: [][2]float64{{0, 0}, {1, 0}, {2, 0}, {0, 0}},
		},
		{
			name:     "self intersection",
			exterior: [][2]float64{{0, 0}, {2, 2}, {0, 2}, {2, 0}, {0, 0}},
		},
		{
			name:     "hole outside shell",
			exterior: [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {0, 0}},
			holes: [][]geo.Coordinate{
				ring(t, [][2]float64{{5, 5}, {6, 5}, {6, 6}, {5, 5}}),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := geo.NewPolygon(ring(t, test.exterior), test.holes)
			if !errors.Is(err, geo.ErrTopology) {
				t.Fatalf("NewPolygon() error = %v, want ErrTopology", err)
			}
		})
	}
}

func TestPolygonValidationUnwrapsTheAntimeridian(t *testing.T) {
	t.Parallel()

	polygon, err := geo.NewPolygon(
		ring(t, [][2]float64{{170, -10}, {-170, -10}, {-170, 10}, {170, 10}, {170, -10}}),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	location, err := polygon.Locate(mustCoordinate(t, 180, 0, geo.WGS84()))
	if err != nil {
		t.Fatalf("Locate() error = %v", err)
	}
	if location != geo.Inside {
		t.Fatalf("Locate() = %v, want Inside", location)
	}
}

func TestPolygonAcceptsAndPreservesEitherWindingDirection(t *testing.T) {
	t.Parallel()

	clockwise := ring(t, [][2]float64{{0, 0}, {0, 2}, {2, 2}, {2, 0}, {0, 0}})
	counterclockwise := ring(t, [][2]float64{{0, 0}, {2, 0}, {2, 2}, {0, 2}, {0, 0}})
	for name, input := range map[string][]geo.Coordinate{
		"clockwise":        clockwise,
		"counterclockwise": counterclockwise,
	} {
		polygon, err := geo.NewPolygon(input, nil)
		if err != nil {
			t.Fatalf("NewPolygon(%s) error = %v", name, err)
		}
		output := polygon.Exterior()
		if len(output) != len(input) {
			t.Fatalf("Exterior(%s) length = %d, want %d", name, len(output), len(input))
		}
		for index := range input {
			if !output[index].Equal(input[index]) {
				t.Fatalf("Exterior(%s) changed coordinate %d", name, index)
			}
		}
	}
}

func ring(t *testing.T, positions [][2]float64) []geo.Coordinate {
	t.Helper()

	result := make([]geo.Coordinate, len(positions))
	for index, position := range positions {
		result[index] = mustCoordinate(t, position[0], position[1], geo.WGS84())
	}

	return result
}

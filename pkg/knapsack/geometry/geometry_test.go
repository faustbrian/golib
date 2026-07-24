package geometry_test

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

func TestOrientationsEnumerateAndDeduplicate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dims geometry.Dimensions
		want int
	}{
		{"distinct", geometry.Dimensions{X: 1, Y: 2, Z: 3}, 6},
		{"two equal", geometry.Dimensions{X: 1, Y: 1, Z: 2}, 3},
		{"cube", geometry.Dimensions{X: 1, Y: 1, Z: 1}, 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			orientations, err := geometry.Orientations(test.dims)
			if err != nil {
				t.Fatal(err)
			}
			if len(orientations) != test.want {
				t.Fatalf("got %d orientations, want %d", len(orientations), test.want)
			}
		})
	}
}

func TestCuboidUsesHalfOpenGeometry(t *testing.T) {
	t.Parallel()

	a, err := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 2, Y: 2, Z: 2})
	if err != nil {
		t.Fatal(err)
	}
	touching, _ := geometry.NewCuboid(geometry.Point{X: 2}, geometry.Dimensions{X: 1, Y: 2, Z: 2})
	overlapping, _ := geometry.NewCuboid(geometry.Point{X: 1}, geometry.Dimensions{X: 1, Y: 2, Z: 2})
	outer, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 3, Y: 3, Z: 3})

	if a.Intersects(touching) {
		t.Fatal("touching cuboids intersect")
	}
	if !a.Intersects(overlapping) || !outer.Contains(a) {
		t.Fatal("overlap or containment was not detected")
	}
	if area, ok := a.SupportArea(touching); ok || area != 0 {
		t.Fatalf("unexpected support: %d, %v", area, ok)
	}
}

func TestCuboidSupportAreaAndAdjacency(t *testing.T) {
	t.Parallel()

	base, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 4, Y: 4, Z: 2})
	top, _ := geometry.NewCuboid(geometry.Point{X: 1, Y: 1, Z: 2}, geometry.Dimensions{X: 4, Y: 2, Z: 1})

	area, ok := base.SupportArea(top)
	if !ok || area != 6 {
		t.Fatalf("support area = %d, %v; want 6, true", area, ok)
	}
	if !base.Adjacent(top) {
		t.Fatal("supporting cuboids are not adjacent")
	}
}

func TestGeometryRejectsInvalidAndOverflow(t *testing.T) {
	t.Parallel()

	if _, err := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{}); !errors.Is(err, geometry.ErrInvalidDimensions) {
		t.Fatalf("zero dimensions: %v", err)
	}
	if _, err := geometry.NewCuboid(geometry.Point{X: math.MaxInt64}, geometry.Dimensions{X: 1, Y: 1, Z: 1}); !errors.Is(err, geometry.ErrOverflow) {
		t.Fatalf("coordinate overflow: %v", err)
	}
	if _, err := (geometry.Dimensions{X: math.MaxInt64, Y: 2, Z: 1}).Volume(); !errors.Is(err, geometry.ErrOverflow) {
		t.Fatalf("volume overflow: %v", err)
	}
	if _, err := (geometry.Dimensions{}).Volume(); !errors.Is(err, geometry.ErrInvalidDimensions) {
		t.Fatalf("invalid volume: %v", err)
	}
}

func TestOrientationPhysicalAxes(t *testing.T) {
	t.Parallel()

	dims := geometry.Dimensions{X: 2, Y: 3, Z: 4}
	got, err := geometry.OrientationZXY.Apply(dims)
	if err != nil {
		t.Fatal(err)
	}
	if want := (geometry.Dimensions{X: 4, Y: 2, Z: 3}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEveryOrientationAndInvalidGeometryBranch(t *testing.T) {
	t.Parallel()
	dimensions := geometry.Dimensions{X: 2, Y: 3, Z: 4}
	want := map[geometry.Orientation]geometry.Dimensions{
		geometry.OrientationXYZ: {X: 2, Y: 3, Z: 4},
		geometry.OrientationXZY: {X: 2, Y: 4, Z: 3},
		geometry.OrientationYXZ: {X: 3, Y: 2, Z: 4},
		geometry.OrientationYZX: {X: 3, Y: 4, Z: 2},
		geometry.OrientationZXY: {X: 4, Y: 2, Z: 3},
		geometry.OrientationZYX: {X: 4, Y: 3, Z: 2},
	}
	for orientation, expected := range want {
		t.Run(string(orientation), func(t *testing.T) {
			t.Parallel()
			actual, err := orientation.Apply(dimensions)
			if err != nil || actual != expected {
				t.Fatalf("dimensions=%+v error=%v", actual, err)
			}
		})
	}
	if _, err := geometry.Orientation("invalid").Apply(dimensions); !errors.Is(err, geometry.ErrInvalidOrientation) {
		t.Fatalf("orientation error = %v", err)
	}
	if _, err := geometry.OrientationXYZ.Apply(geometry.Dimensions{}); !errors.Is(err, geometry.ErrInvalidDimensions) {
		t.Fatalf("apply error = %v", err)
	}
	if _, err := geometry.Orientations(geometry.Dimensions{}); !errors.Is(err, geometry.ErrInvalidDimensions) {
		t.Fatalf("orientations error = %v", err)
	}
	if _, err := geometry.NewCuboid(geometry.Point{X: -1}, geometry.Dimensions{X: 1, Y: 1, Z: 1}); !errors.Is(err, geometry.ErrInvalidCoordinate) {
		t.Fatalf("coordinate error = %v", err)
	}
}

func TestCuboidAccessorsEveryAdjacencyAxisAndSupportOverflow(t *testing.T) {
	t.Parallel()
	base, _ := geometry.NewCuboid(geometry.Point{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 2, Y: 3, Z: 4})
	if base.Origin() != (geometry.Point{X: 1, Y: 1, Z: 1}) || base.Dimensions() != (geometry.Dimensions{X: 2, Y: 3, Z: 4}) || base.Max() != (geometry.Point{X: 3, Y: 4, Z: 5}) {
		t.Fatal("cuboid accessors changed")
	}
	if volume, err := base.Volume(); err != nil || volume != 24 {
		t.Fatalf("volume=%d error=%v", volume, err)
	}
	for _, other := range []geometry.Cuboid{
		mustCuboid(t, geometry.Point{X: 3, Y: 1, Z: 1}, geometry.Dimensions{X: 1, Y: 3, Z: 4}),
		mustCuboid(t, geometry.Point{X: 1, Y: 4, Z: 1}, geometry.Dimensions{X: 2, Y: 1, Z: 4}),
		mustCuboid(t, geometry.Point{X: 1, Y: 1, Z: 5}, geometry.Dimensions{X: 2, Y: 3, Z: 1}),
	} {
		if !base.Adjacent(other) {
			t.Fatalf("not adjacent: %+v", other)
		}
	}
	distant := mustCuboid(t, geometry.Point{X: 10, Y: 10, Z: 10}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	if base.Adjacent(distant) {
		t.Fatal("distant cuboids are adjacent")
	}
	huge, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: math.MaxInt64, Y: 2, Z: 1})
	hugeTop, _ := geometry.NewCuboid(geometry.Point{Z: 1}, geometry.Dimensions{X: math.MaxInt64, Y: 2, Z: 1})
	if area, ok := huge.SupportArea(hugeTop); ok || area != 0 {
		t.Fatalf("overflow support area=%d ok=%v", area, ok)
	}
	if _, err := (geometry.Dimensions{X: math.MaxInt64 / 2, Y: 1, Z: 3}).Volume(); !errors.Is(err, geometry.ErrOverflow) {
		t.Fatalf("z volume overflow = %v", err)
	}
}

func mustCuboid(t *testing.T, origin geometry.Point, dimensions geometry.Dimensions) geometry.Cuboid {
	t.Helper()
	cuboid, err := geometry.NewCuboid(origin, dimensions)
	if err != nil {
		t.Fatal(err)
	}
	return cuboid
}

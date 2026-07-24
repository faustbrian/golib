package geometry_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

func FuzzGeometry(f *testing.F) {
	f.Add(int64(0), int64(1), int64(1), int64(1))
	f.Fuzz(func(t *testing.T, x, width, depth, height int64) {
		if x < 0 || width <= 0 || depth <= 0 || height <= 0 || x > 1_000_000 || width > 1_000_000 || depth > 1_000_000 || height > 1_000_000 {
			return
		}
		box, err := geometry.NewCuboid(geometry.Point{X: x}, geometry.Dimensions{X: width, Y: depth, Z: height})
		if err != nil {
			return
		}
		other, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: width, Y: depth, Z: height})
		if box.Intersects(other) != other.Intersects(box) {
			t.Fatal("intersection is not symmetric")
		}
	})
}

func FuzzOrientationEnumeration(f *testing.F) {
	f.Add(int64(1), int64(2), int64(3))
	f.Add(int64(2), int64(2), int64(3))
	f.Add(int64(4), int64(4), int64(4))
	f.Fuzz(func(t *testing.T, x, y, z int64) {
		if x <= 0 || y <= 0 || z <= 0 || x > 1_000_000 || y > 1_000_000 || z > 1_000_000 {
			return
		}
		dimensions := geometry.Dimensions{X: x, Y: y, Z: z}
		orientations, err := geometry.Orientations(dimensions)
		if err != nil {
			t.Fatal(err)
		}
		want := map[geometry.Dimensions]struct{}{
			{X: x, Y: y, Z: z}: {}, {X: x, Y: z, Z: y}: {},
			{X: y, Y: x, Z: z}: {}, {X: y, Y: z, Z: x}: {},
			{X: z, Y: x, Z: y}: {}, {X: z, Y: y, Z: x}: {},
		}
		got := make(map[geometry.Dimensions]struct{}, len(orientations))
		for _, orientation := range orientations {
			rotated, applyErr := orientation.Apply(dimensions)
			if applyErr != nil {
				t.Fatal(applyErr)
			}
			got[rotated] = struct{}{}
		}
		if len(got) != len(want) || len(orientations) != len(want) {
			t.Fatalf("orientations=%v dimensions=%+v", orientations, dimensions)
		}
	})
}

func FuzzCuboidRelations(f *testing.F) {
	f.Add(int64(0), int64(1), int64(1), int64(0), int64(1), int64(1))
	f.Fuzz(func(t *testing.T, ax, aw, ah, bx, bw, bh int64) {
		if ax < 0 || bx < 0 || aw <= 0 || ah <= 0 || bw <= 0 || bh <= 0 ||
			ax > 1_000 || bx > 1_000 || aw > 1_000 || ah > 1_000 || bw > 1_000 || bh > 1_000 {
			return
		}
		a, errA := geometry.NewCuboid(geometry.Point{X: ax}, geometry.Dimensions{X: aw, Y: 1, Z: ah})
		b, errB := geometry.NewCuboid(geometry.Point{X: bx}, geometry.Dimensions{X: bw, Y: 1, Z: bh})
		if errA != nil || errB != nil {
			return
		}
		if a.Intersects(b) != b.Intersects(a) || a.Adjacent(b) != b.Adjacent(a) {
			t.Fatal("cuboid relation is not symmetric")
		}
		if a.Contains(b) && (b.Origin().X < a.Origin().X || b.Max().X > a.Max().X) {
			t.Fatal("containment disagrees with independent interval check")
		}
		area, supported := a.SupportArea(b)
		if supported && (a.Max().Z != b.Origin().Z || area <= 0) {
			t.Fatal("invalid support relation")
		}
	})
}

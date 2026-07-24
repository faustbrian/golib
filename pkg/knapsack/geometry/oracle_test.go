package geometry_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

type voxel struct{ x, y, z int64 }

func TestCuboidPredicatesAgreeWithBoundedVoxelOracle(t *testing.T) {
	t.Parallel()

	cuboids := boundedCuboids(t, 3)
	for _, outer := range cuboids {
		outerVoxels := occupiedVoxels(outer)
		for _, inner := range cuboids {
			innerVoxels := occupiedVoxels(inner)
			wantIntersection := setsIntersect(outerVoxels, innerVoxels)
			if got := outer.Intersects(inner); got != wantIntersection {
				t.Fatalf("Intersects(%+v, %+v) = %v, want %v", outer, inner, got, wantIntersection)
			}
			if outer.Intersects(inner) != inner.Intersects(outer) {
				t.Fatalf("intersection is asymmetric for %+v and %+v", outer, inner)
			}
			wantContainment := setContains(outerVoxels, innerVoxels)
			if got := outer.Contains(inner); got != wantContainment {
				t.Fatalf("Contains(%+v, %+v) = %v, want %v", outer, inner, got, wantContainment)
			}
		}
	}
}

func TestCuboidTranslationAndAxisPermutationMetamorphisms(t *testing.T) {
	t.Parallel()

	left := mustCuboid(t, geometry.Point{X: 1, Y: 2, Z: 3}, geometry.Dimensions{X: 3, Y: 2, Z: 2})
	right := mustCuboid(t, geometry.Point{X: 3, Y: 2, Z: 4}, geometry.Dimensions{X: 2, Y: 3, Z: 1})
	container := mustCuboid(t, geometry.Point{}, geometry.Dimensions{X: 8, Y: 8, Z: 8})

	translatedLeft := translateCuboid(t, left, geometry.Point{X: 5, Y: 7, Z: 11})
	translatedRight := translateCuboid(t, right, geometry.Point{X: 5, Y: 7, Z: 11})
	if left.Intersects(right) != translatedLeft.Intersects(translatedRight) {
		t.Fatal("translation changed intersection")
	}

	permutedLeft := permuteCuboid(t, left)
	permutedRight := permuteCuboid(t, right)
	permutedContainer := permuteCuboid(t, container)
	if left.Intersects(right) != permutedLeft.Intersects(permutedRight) {
		t.Fatal("axis permutation changed intersection")
	}
	if container.Contains(left) != permutedContainer.Contains(permutedLeft) {
		t.Fatal("axis permutation changed containment")
	}
}

func TestIntersectionIsNotAssumedTransitive(t *testing.T) {
	t.Parallel()

	left := mustCuboid(t, geometry.Point{}, geometry.Dimensions{X: 2, Y: 1, Z: 1})
	middle := mustCuboid(t, geometry.Point{X: 1}, geometry.Dimensions{X: 2, Y: 1, Z: 1})
	right := mustCuboid(t, geometry.Point{X: 2}, geometry.Dimensions{X: 2, Y: 1, Z: 1})
	if !left.Intersects(middle) || !middle.Intersects(right) || left.Intersects(right) {
		t.Fatal("fixture must demonstrate non-transitive half-open intersection")
	}
}

func boundedCuboids(t *testing.T, extent int64) []geometry.Cuboid {
	t.Helper()
	result := make([]geometry.Cuboid, 0)
	for x := range extent {
		for y := range extent {
			for z := range extent {
				for sizeX := int64(1); x+sizeX <= extent; sizeX++ {
					for sizeY := int64(1); y+sizeY <= extent; sizeY++ {
						for sizeZ := int64(1); z+sizeZ <= extent; sizeZ++ {
							result = append(result, mustCuboid(t,
								geometry.Point{X: x, Y: y, Z: z},
								geometry.Dimensions{X: sizeX, Y: sizeY, Z: sizeZ},
							))
						}
					}
				}
			}
		}
	}
	return result
}

func occupiedVoxels(cuboid geometry.Cuboid) map[voxel]struct{} {
	origin, dimensions := cuboid.Origin(), cuboid.Dimensions()
	result := make(map[voxel]struct{}, dimensions.X*dimensions.Y*dimensions.Z)
	for x := origin.X; x < origin.X+dimensions.X; x++ {
		for y := origin.Y; y < origin.Y+dimensions.Y; y++ {
			for z := origin.Z; z < origin.Z+dimensions.Z; z++ {
				result[voxel{x, y, z}] = struct{}{}
			}
		}
	}
	return result
}

func setsIntersect(left, right map[voxel]struct{}) bool {
	for point := range left {
		if _, ok := right[point]; ok {
			return true
		}
	}
	return false
}

func setContains(outer, inner map[voxel]struct{}) bool {
	for point := range inner {
		if _, ok := outer[point]; !ok {
			return false
		}
	}
	return true
}

func translateCuboid(t *testing.T, cuboid geometry.Cuboid, delta geometry.Point) geometry.Cuboid {
	t.Helper()
	origin := cuboid.Origin()
	return mustCuboid(t, geometry.Point{X: origin.X + delta.X, Y: origin.Y + delta.Y, Z: origin.Z + delta.Z}, cuboid.Dimensions())
}

func permuteCuboid(t *testing.T, cuboid geometry.Cuboid) geometry.Cuboid {
	t.Helper()
	origin, dimensions := cuboid.Origin(), cuboid.Dimensions()
	return mustCuboid(t,
		geometry.Point{X: origin.Z, Y: origin.X, Z: origin.Y},
		geometry.Dimensions{X: dimensions.Z, Y: dimensions.X, Z: dimensions.Y},
	)
}

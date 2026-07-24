// Package geometry provides exact integer-lattice operations for axis-aligned
// cuboids. Coordinates start at the lower-left-front corner; X is width, Y is
// depth, and Z is height. Occupied intervals are half-open.
package geometry

import (
	"errors"
	"fmt"
	"math"
)

var (
	// ErrInvalidDimensions identifies non-positive cuboid dimensions.
	ErrInvalidDimensions = errors.New("geometry: dimensions must be positive")
	// ErrInvalidCoordinate identifies a negative lattice coordinate.
	ErrInvalidCoordinate = errors.New("geometry: coordinates must be non-negative")
	// ErrInvalidOrientation identifies an unknown axis permutation.
	ErrInvalidOrientation = errors.New("geometry: invalid orientation")
	// ErrOverflow identifies a geometric result outside the int64 lattice.
	ErrOverflow = errors.New("geometry: integer overflow")
)

// Point is an integer-lattice origin.
type Point struct {
	// X, Y, and Z are non-negative lattice coordinates.
	X, Y, Z int64
}

// Dimensions names the physical axes of a cuboid.
type Dimensions struct {
	// X, Y, and Z are positive lattice lengths.
	X, Y, Z int64
}

// Valid reports whether all dimensions are positive.
func (d Dimensions) Valid() bool { return d.X > 0 && d.Y > 0 && d.Z > 0 }

// Volume returns the checked cuboid volume.
func (d Dimensions) Volume() (int64, error) {
	if !d.Valid() {
		return 0, ErrInvalidDimensions
	}
	xy, ok := checkedMul(d.X, d.Y)
	if !ok {
		return 0, ErrOverflow
	}
	xyz, ok := checkedMul(xy, d.Z)
	if !ok {
		return 0, ErrOverflow
	}
	return xyz, nil
}

// Orientation identifies how the original physical axes map to X, Y, and Z.
type Orientation string

const (
	// OrientationXYZ preserves the original X, Y, and Z axes.
	OrientationXYZ Orientation = "xyz"
	// OrientationXZY swaps the original Y and Z axes.
	OrientationXZY Orientation = "xzy"
	// OrientationYXZ swaps the original X and Y axes.
	OrientationYXZ Orientation = "yxz"
	// OrientationYZX maps the original axes to Y, Z, and X.
	OrientationYZX Orientation = "yzx"
	// OrientationZXY maps the original axes to Z, X, and Y.
	OrientationZXY Orientation = "zxy"
	// OrientationZYX reverses the original axis order.
	OrientationZYX Orientation = "zyx"
)

var allOrientations = [...]Orientation{
	OrientationXYZ, OrientationXZY, OrientationYXZ,
	OrientationYZX, OrientationZXY, OrientationZYX,
}

// Apply returns dimensions after the physical-axis permutation.
func (o Orientation) Apply(d Dimensions) (Dimensions, error) {
	if !d.Valid() {
		return Dimensions{}, ErrInvalidDimensions
	}
	switch o {
	case OrientationXYZ:
		return d, nil
	case OrientationXZY:
		return Dimensions{d.X, d.Z, d.Y}, nil
	case OrientationYXZ:
		return Dimensions{d.Y, d.X, d.Z}, nil
	case OrientationYZX:
		return Dimensions{d.Y, d.Z, d.X}, nil
	case OrientationZXY:
		return Dimensions{d.Z, d.X, d.Y}, nil
	case OrientationZYX:
		return Dimensions{d.Z, d.Y, d.X}, nil
	default:
		return Dimensions{}, fmt.Errorf("%w: %q", ErrInvalidOrientation, o)
	}
}

// Orientations returns canonical unique orthogonal rotations.
func Orientations(d Dimensions) ([]Orientation, error) {
	if !d.Valid() {
		return nil, ErrInvalidDimensions
	}
	seen := make(map[Dimensions]struct{}, len(allOrientations))
	result := make([]Orientation, 0, len(allOrientations))
	for _, orientation := range allOrientations {
		rotated, _ := orientation.Apply(d)
		if _, exists := seen[rotated]; exists {
			continue
		}
		seen[rotated] = struct{}{}
		result = append(result, orientation)
	}
	return result, nil
}

// Cuboid is a validated half-open occupied region.
type Cuboid struct {
	origin Point
	dims   Dimensions
	max    Point
}

// NewCuboid validates coordinates, dimensions, and endpoint arithmetic.
func NewCuboid(origin Point, dimensions Dimensions) (Cuboid, error) {
	if origin.X < 0 || origin.Y < 0 || origin.Z < 0 {
		return Cuboid{}, ErrInvalidCoordinate
	}
	if !dimensions.Valid() {
		return Cuboid{}, ErrInvalidDimensions
	}
	x, okX := checkedAdd(origin.X, dimensions.X)
	y, okY := checkedAdd(origin.Y, dimensions.Y)
	z, okZ := checkedAdd(origin.Z, dimensions.Z)
	if !okX || !okY || !okZ {
		return Cuboid{}, ErrOverflow
	}
	return Cuboid{origin: origin, dims: dimensions, max: Point{x, y, z}}, nil
}

// Origin returns the inclusive lower corner.
func (c Cuboid) Origin() Point { return c.origin }

// Dimensions returns the cuboid's lattice lengths.
func (c Cuboid) Dimensions() Dimensions { return c.dims }

// Max returns the exclusive upper corner.
func (c Cuboid) Max() Point { return c.max }

// Volume returns the checked lattice volume.
func (c Cuboid) Volume() (int64, error) { return c.dims.Volume() }

// Contains reports complete half-open containment.
func (c Cuboid) Contains(other Cuboid) bool {
	return c.origin.X <= other.origin.X && c.origin.Y <= other.origin.Y && c.origin.Z <= other.origin.Z &&
		c.max.X >= other.max.X && c.max.Y >= other.max.Y && c.max.Z >= other.max.Z
}

// Intersects reports positive-volume intersection. Touching is permitted.
func (c Cuboid) Intersects(other Cuboid) bool {
	return c.origin.X < other.max.X && other.origin.X < c.max.X &&
		c.origin.Y < other.max.Y && other.origin.Y < c.max.Y &&
		c.origin.Z < other.max.Z && other.origin.Z < c.max.Z
}

// Adjacent reports contact along a face with positive contact area.
func (c Cuboid) Adjacent(other Cuboid) bool {
	xy := overlap(c.origin.X, c.max.X, other.origin.X, other.max.X) > 0 && overlap(c.origin.Y, c.max.Y, other.origin.Y, other.max.Y) > 0
	xz := overlap(c.origin.X, c.max.X, other.origin.X, other.max.X) > 0 && overlap(c.origin.Z, c.max.Z, other.origin.Z, other.max.Z) > 0
	yz := overlap(c.origin.Y, c.max.Y, other.origin.Y, other.max.Y) > 0 && overlap(c.origin.Z, c.max.Z, other.origin.Z, other.max.Z) > 0
	return xy && (c.max.Z == other.origin.Z || other.max.Z == c.origin.Z) ||
		xz && (c.max.Y == other.origin.Y || other.max.Y == c.origin.Y) ||
		yz && (c.max.X == other.origin.X || other.max.X == c.origin.X)
}

// SupportArea returns the XY contact area when c directly supports other.
func (c Cuboid) SupportArea(other Cuboid) (int64, bool) {
	if c.max.Z != other.origin.Z {
		return 0, false
	}
	x := overlap(c.origin.X, c.max.X, other.origin.X, other.max.X)
	y := overlap(c.origin.Y, c.max.Y, other.origin.Y, other.max.Y)
	if x == 0 || y == 0 {
		return 0, false
	}
	area, ok := checkedMul(x, y)
	if !ok {
		return 0, false
	}
	return area, true
}

func overlap(a0, a1, b0, b1 int64) int64 {
	lo, hi := max(a0, b0), min(a1, b1)
	if hi <= lo {
		return 0
	}
	return hi - lo
}

func checkedAdd(a, b int64) (int64, bool) {
	if a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

func checkedMul(a, b int64) (int64, bool) {
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

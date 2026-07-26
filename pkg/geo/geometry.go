package geo

import simplegeom "github.com/peterstace/simplefeatures/geom"

// Limits bounds geometry construction and untrusted codec work. A zero field
// selects the corresponding DefaultLimits value.
type Limits struct {
	MaxPoints          int
	MaxRings           int
	MaxGeometries      int
	MaxCollectionDepth int
	MaxEncodedBytes    int64
}

// DefaultLimits returns conservative process-local resource limits.
func DefaultLimits() Limits {
	return Limits{
		MaxPoints:          1_000_000,
		MaxRings:           10_000,
		MaxGeometries:      100_000,
		MaxCollectionDepth: 32,
		MaxEncodedBytes:    64 << 20,
	}
}

// Location is the explicit result of a point-in-polygon operation.
type Location uint8

const (
	// Outside means the coordinate is not in the polygon or lies in a hole.
	Outside Location = iota
	// Inside means the coordinate lies strictly inside the polygon surface.
	Inside
	// Boundary means the coordinate lies on an exterior or hole edge.
	Boundary
)

// LineString is an immutable sequence of at least two coordinates in one CRS.
type LineString struct {
	coordinates []Coordinate
	crs         CRS
}

// NewLineString validates and owns at least two same-CRS coordinates.
func NewLineString(coordinates []Coordinate) (LineString, error) {
	return NewLineStringWithLimits(coordinates, DefaultLimits())
}

// NewLineStringWithLimits constructs a line under explicit resource limits.
func NewLineStringWithLimits(coordinates []Coordinate, limits Limits) (LineString, error) {
	limits = limits.withDefaults()
	if len(coordinates) < 2 {
		return LineString{}, &TopologyError{Geometry: "line string", Problem: "requires at least two points"}
	}
	if len(coordinates) > limits.MaxPoints {
		return LineString{}, &TopologyError{Geometry: "line string", Problem: "point limit exceeded"}
	}
	crs, err := commonCRS("line string", coordinates)
	if err != nil {
		return LineString{}, err
	}

	return LineString{coordinates: cloneCoordinates(coordinates), crs: crs}, nil
}

// Len returns the number of coordinates.
func (line LineString) Len() int { return len(line.coordinates) }

// At returns a coordinate and false when index is out of range.
func (line LineString) At(index int) (Coordinate, bool) {
	if index < 0 || index >= len(line.coordinates) {
		return Coordinate{}, false
	}

	return line.coordinates[index], true
}

// Coordinates returns an owned coordinate slice.
func (line LineString) Coordinates() []Coordinate { return cloneCoordinates(line.coordinates) }

// CRS returns the shared coordinate reference system.
func (line LineString) CRS() CRS { return line.crs }

// Type returns TypeLineString.
func (line LineString) Type() GeometryType { return TypeLineString }

func (line LineString) geometryMarker() {}

func (line LineString) pointCount() int { return len(line.coordinates) }

func (line LineString) geometryCount() int { return 1 }

func (line LineString) geometryDepth() int { return 1 }

// Polygon is an immutable closed exterior ring with zero or more closed holes.
// Either winding direction is accepted and preserved.
type Polygon struct {
	exterior []Coordinate
	holes    [][]Coordinate
	crs      CRS
}

// NewPolygon validates and owns one exterior ring and optional hole rings.
func NewPolygon(exterior []Coordinate, holes [][]Coordinate) (Polygon, error) {
	return NewPolygonWithLimits(exterior, holes, DefaultLimits())
}

// NewPolygonWithLimits constructs a polygon under explicit resource limits.
func NewPolygonWithLimits(exterior []Coordinate, holes [][]Coordinate, limits Limits) (Polygon, error) {
	limits = limits.withDefaults()
	if len(holes)+1 > limits.MaxRings {
		return Polygon{}, &TopologyError{Geometry: "polygon", Problem: "ring limit exceeded"}
	}
	crs, points, err := validateRing("exterior ring", exterior, CRS{})
	if err != nil {
		return Polygon{}, err
	}
	clonedHoles := make([][]Coordinate, len(holes))
	for index, hole := range holes {
		_, count, ringErr := validateRing("interior ring", hole, crs)
		if ringErr != nil {
			return Polygon{}, ringErr
		}
		points += count
		if points > limits.MaxPoints {
			return Polygon{}, &TopologyError{Geometry: "polygon", Problem: "point limit exceeded"}
		}
		clonedHoles[index] = cloneCoordinates(hole)
	}
	if points > limits.MaxPoints {
		return Polygon{}, &TopologyError{Geometry: "polygon", Problem: "point limit exceeded"}
	}
	if err := validatePolygonTopology(exterior, holes); err != nil {
		return Polygon{}, err
	}

	return Polygon{exterior: cloneCoordinates(exterior), holes: clonedHoles, crs: crs}, nil
}

// CRS returns the shared coordinate reference system.
func (polygon Polygon) CRS() CRS { return polygon.crs }

// Type returns TypePolygon.
func (polygon Polygon) Type() GeometryType { return TypePolygon }

func (polygon Polygon) geometryMarker() {}

func (polygon Polygon) pointCount() int {
	count := len(polygon.exterior)
	for _, hole := range polygon.holes {
		count += len(hole)
	}

	return count
}

func (polygon Polygon) geometryCount() int { return 1 }

func (polygon Polygon) geometryDepth() int { return 1 }

// Exterior returns an owned copy of the closed exterior ring.
func (polygon Polygon) Exterior() []Coordinate { return cloneCoordinates(polygon.exterior) }

// Holes returns owned copies of all closed interior rings.
func (polygon Polygon) Holes() [][]Coordinate {
	holes := make([][]Coordinate, len(polygon.holes))
	for index := range polygon.holes {
		holes[index] = cloneCoordinates(polygon.holes[index])
	}

	return holes
}

// Locate uses an even-odd ray crossing test. It is O(n) in total ring points,
// allocates no memory, treats ring boundaries explicitly, and unwraps
// longitudes relative to the query point for antimeridian-safe comparisons.
func (polygon Polygon) Locate(point Coordinate) (Location, error) {
	if !polygon.crs.Equal(point.crs) {
		return Outside, incompatibleCRSError(polygon.crs, point.crs)
	}
	location := locateInRing(polygon.exterior, point)
	if location != Inside {
		return location, nil
	}
	for _, hole := range polygon.holes {
		holeLocation := locateInRing(hole, point)
		if holeLocation == Boundary {
			return Boundary, nil
		}
		if holeLocation == Inside {
			return Outside, nil
		}
	}

	return Inside, nil
}

func validateRing(name string, ring []Coordinate, expected CRS) (CRS, int, error) {
	if len(ring) < 4 {
		return CRS{}, 0, &TopologyError{Geometry: "polygon", Problem: name + " requires at least four positions"}
	}
	crs, err := commonCRS("polygon", ring)
	if err != nil {
		return CRS{}, 0, err
	}
	if expected.valid() && !crs.Equal(expected) {
		return CRS{}, 0, incompatibleCRSError(expected, crs)
	}
	if !ring[0].Equal(ring[len(ring)-1]) {
		return CRS{}, 0, &TopologyError{Geometry: "polygon", Problem: name + " is not closed"}
	}

	return crs, len(ring), nil
}

func commonCRS(geometry string, coordinates []Coordinate) (CRS, error) {
	crs := coordinates[0].crs
	if !crs.valid() {
		return CRS{}, &CRSError{SRID: crs.srid, Problem: geometry + " requires valid CRS metadata"}
	}
	for _, coordinate := range coordinates[1:] {
		if !coordinate.crs.Equal(crs) {
			return CRS{}, incompatibleCRSError(crs, coordinate.crs)
		}
	}

	return crs, nil
}

func locateInRing(ring []Coordinate, point Coordinate) Location {
	x := point.longitude.degrees
	y := point.latitude.degrees
	inside := false
	for index, previous := 0, len(ring)-1; index < len(ring); previous, index = index, index+1 {
		x1 := unwrapLongitude(ring[previous].longitude.degrees, x)
		y1 := ring[previous].latitude.degrees
		x2 := unwrapLongitude(ring[index].longitude.degrees, x)
		y2 := ring[index].latitude.degrees
		if pointOnSegment(x, y, x1, y1, x2, y2) {
			return Boundary
		}
		if (y1 > y) != (y2 > y) && x < (x2-x1)*(y-y1)/(y2-y1)+x1 {
			inside = !inside
		}
	}
	if inside {
		return Inside
	}

	return Outside
}

func pointOnSegment(x, y, x1, y1, x2, y2 float64) bool {
	cross := (x-x1)*(y2-y1) - (y-y1)*(x2-x1)
	return cross == 0 && x >= min(x1, x2) && x <= max(x1, x2) &&
		y >= min(y1, y2) && y <= max(y1, y2)
}

func unwrapLongitude(longitude, reference float64) float64 {
	for longitude-reference > 180 {
		longitude -= 360
	}
	for longitude-reference < -180 {
		longitude += 360
	}

	return longitude
}

func cloneCoordinates(coordinates []Coordinate) []Coordinate {
	return append([]Coordinate(nil), coordinates...)
}

func validatePolygonTopology(exterior []Coordinate, holes [][]Coordinate) error {
	reference := exterior[0].longitude.degrees
	rings := make([][]float64, 1+len(holes))
	rings[0] = topologyRing(exterior, reference)
	for index, hole := range holes {
		rings[index+1] = topologyRing(hole, reference)
	}
	if err := simplegeom.NewPolygonXY(rings...).Validate(); err != nil {
		return &TopologyError{
			Geometry: "polygon",
			Problem:  "violates OGC polygon constraints",
		}
	}

	return nil
}

func topologyRing(ring []Coordinate, reference float64) []float64 {
	flat := make([]float64, 0, len(ring)*2)
	for _, coordinate := range ring {
		flat = append(
			flat,
			unwrapLongitude(coordinate.longitude.degrees, reference),
			coordinate.latitude.degrees,
		)
	}

	return flat
}

func (limits Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if limits.MaxPoints == 0 {
		limits.MaxPoints = defaults.MaxPoints
	}
	if limits.MaxRings == 0 {
		limits.MaxRings = defaults.MaxRings
	}
	if limits.MaxGeometries == 0 {
		limits.MaxGeometries = defaults.MaxGeometries
	}
	if limits.MaxCollectionDepth == 0 {
		limits.MaxCollectionDepth = defaults.MaxCollectionDepth
	}
	if limits.MaxEncodedBytes == 0 {
		limits.MaxEncodedBytes = defaults.MaxEncodedBytes
	}

	return limits
}

// ResolveLimits replaces zero fields with DefaultLimits values.
func ResolveLimits(limits Limits) Limits { return limits.withDefaults() }

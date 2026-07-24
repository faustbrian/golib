package geo

// GeometryType is a stable geometry-kind identifier shared by codecs.
type GeometryType string

const (
	// TypePoint identifies Point geometry.
	TypePoint GeometryType = "Point"
	// TypeLineString identifies LineString geometry.
	TypeLineString GeometryType = "LineString"
	// TypePolygon identifies Polygon geometry.
	TypePolygon GeometryType = "Polygon"
	// TypeMultiPoint identifies MultiPoint geometry.
	TypeMultiPoint GeometryType = "MultiPoint"
	// TypeMultiLineString identifies MultiLineString geometry.
	TypeMultiLineString GeometryType = "MultiLineString"
	// TypeMultiPolygon identifies MultiPolygon geometry.
	TypeMultiPolygon GeometryType = "MultiPolygon"
	// TypeGeometryCollection identifies GeometryCollection geometry.
	TypeGeometryCollection GeometryType = "GeometryCollection"
)

// Geometry is the closed, package-owned geometry model. The unexported methods
// prevent external implementations from bypassing CRS and resource invariants.
type Geometry interface {
	Type() GeometryType
	CRS() CRS
	geometryMarker()
	pointCount() int
	geometryCount() int
	geometryDepth() int
}

// CloneGeometry returns a canonical owned value. Pointer inputs are copied to
// values so later pointer reassignment cannot mutate the returned geometry.
func CloneGeometry(geometry Geometry) (Geometry, error) { return cloneGeometry(geometry) }

// Point is an immutable coordinate geometry.
type Point struct{ coordinate Coordinate }

// NewPoint constructs a point from one coordinate with valid CRS metadata.
func NewPoint(coordinate Coordinate) (Point, error) {
	if !coordinate.crs.valid() {
		return Point{}, &CRSError{SRID: coordinate.crs.srid, Problem: "point requires valid CRS metadata"}
	}

	return Point{coordinate: coordinate}, nil
}

// Type returns TypePoint.
func (point Point) Type() GeometryType { return TypePoint }

// CRS returns the point's coordinate reference system.
func (point Point) CRS() CRS { return point.coordinate.crs }

// Coordinate returns the point coordinate.
func (point Point) Coordinate() Coordinate { return point.coordinate }

// Equal reports exact coordinate and CRS equality.
func (point Point) Equal(other Point) bool { return point.coordinate.Equal(other.coordinate) }

func (point Point) geometryMarker() {}

func (point Point) pointCount() int { return 1 }

func (point Point) geometryCount() int { return 1 }

func (point Point) geometryDepth() int { return 1 }

// MultiPoint is an immutable, possibly empty coordinate collection in one CRS.
type MultiPoint struct {
	coordinates []Coordinate
	crs         CRS
}

// NewMultiPoint validates and owns zero or more coordinates in one CRS.
func NewMultiPoint(coordinates []Coordinate, crs CRS) (MultiPoint, error) {
	return NewMultiPointWithLimits(coordinates, crs, DefaultLimits())
}

// NewMultiPointWithLimits constructs a multi-point under resource limits.
func NewMultiPointWithLimits(coordinates []Coordinate, crs CRS, limits Limits) (MultiPoint, error) {
	limits = limits.withDefaults()
	if err := validateDeclaredCRS("multi point", crs); err != nil {
		return MultiPoint{}, err
	}
	if len(coordinates) > limits.MaxPoints {
		return MultiPoint{}, limitError("multi point", "point")
	}
	for _, coordinate := range coordinates {
		if !coordinate.crs.Equal(crs) {
			return MultiPoint{}, incompatibleCRSError(crs, coordinate.crs)
		}
	}

	return MultiPoint{coordinates: cloneCoordinates(coordinates), crs: crs}, nil
}

// Type returns TypeMultiPoint.
func (multi MultiPoint) Type() GeometryType { return TypeMultiPoint }

// CRS returns the declared coordinate reference system, including when empty.
func (multi MultiPoint) CRS() CRS { return multi.crs }

// Len returns the number of coordinates.
func (multi MultiPoint) Len() int { return len(multi.coordinates) }

// At returns a coordinate and false when index is out of range.
func (multi MultiPoint) At(index int) (Coordinate, bool) {
	if index < 0 || index >= len(multi.coordinates) {
		return Coordinate{}, false
	}

	return multi.coordinates[index], true
}

// Coordinates returns an owned coordinate slice.
func (multi MultiPoint) Coordinates() []Coordinate { return cloneCoordinates(multi.coordinates) }

func (multi MultiPoint) geometryMarker() {}

func (multi MultiPoint) pointCount() int { return len(multi.coordinates) }

func (multi MultiPoint) geometryCount() int { return 1 }

func (multi MultiPoint) geometryDepth() int { return 1 }

// MultiLineString is an immutable, possibly empty line collection in one CRS.
type MultiLineString struct {
	lines []LineString
	crs   CRS
}

// NewMultiLineString validates and owns zero or more lines in one CRS.
func NewMultiLineString(lines []LineString, crs CRS) (MultiLineString, error) {
	return NewMultiLineStringWithLimits(lines, crs, DefaultLimits())
}

// NewMultiLineStringWithLimits constructs a multi-line under resource limits.
func NewMultiLineStringWithLimits(lines []LineString, crs CRS, limits Limits) (MultiLineString, error) {
	limits = limits.withDefaults()
	if err := validateDeclaredCRS("multi line string", crs); err != nil {
		return MultiLineString{}, err
	}
	if len(lines) > limits.MaxGeometries {
		return MultiLineString{}, limitError("multi line string", "geometry")
	}
	points := 0
	for _, line := range lines {
		if !line.crs.Equal(crs) {
			return MultiLineString{}, incompatibleCRSError(crs, line.crs)
		}
		if exceeds(points, line.pointCount(), limits.MaxPoints) {
			return MultiLineString{}, limitError("multi line string", "point")
		}
		points += line.pointCount()
	}

	return MultiLineString{lines: append([]LineString(nil), lines...), crs: crs}, nil
}

// Type returns TypeMultiLineString.
func (multi MultiLineString) Type() GeometryType { return TypeMultiLineString }

// CRS returns the declared coordinate reference system, including when empty.
func (multi MultiLineString) CRS() CRS { return multi.crs }

// Len returns the number of lines.
func (multi MultiLineString) Len() int { return len(multi.lines) }

// At returns an immutable line and false when index is out of range.
func (multi MultiLineString) At(index int) (LineString, bool) {
	if index < 0 || index >= len(multi.lines) {
		return LineString{}, false
	}

	return multi.lines[index], true
}

// Lines returns owned copies of all lines.
func (multi MultiLineString) Lines() []LineString { return append([]LineString(nil), multi.lines...) }

func (multi MultiLineString) geometryMarker() {}

func (multi MultiLineString) pointCount() int {
	count := 0
	for _, line := range multi.lines {
		count += line.pointCount()
	}
	return count
}

func (multi MultiLineString) geometryCount() int { return 1 }

func (multi MultiLineString) geometryDepth() int { return 1 }

// MultiPolygon is an immutable, possibly empty polygon collection in one CRS.
type MultiPolygon struct {
	polygons []Polygon
	crs      CRS
}

// NewMultiPolygon validates and owns zero or more polygons in one CRS.
func NewMultiPolygon(polygons []Polygon, crs CRS) (MultiPolygon, error) {
	return NewMultiPolygonWithLimits(polygons, crs, DefaultLimits())
}

// NewMultiPolygonWithLimits constructs a multi-polygon under resource limits.
func NewMultiPolygonWithLimits(polygons []Polygon, crs CRS, limits Limits) (MultiPolygon, error) {
	limits = limits.withDefaults()
	if err := validateDeclaredCRS("multi polygon", crs); err != nil {
		return MultiPolygon{}, err
	}
	if len(polygons) > limits.MaxGeometries {
		return MultiPolygon{}, limitError("multi polygon", "geometry")
	}
	points := 0
	for _, polygon := range polygons {
		if !polygon.crs.Equal(crs) {
			return MultiPolygon{}, incompatibleCRSError(crs, polygon.crs)
		}
		if exceeds(points, polygon.pointCount(), limits.MaxPoints) {
			return MultiPolygon{}, limitError("multi polygon", "point")
		}
		points += polygon.pointCount()
	}

	return MultiPolygon{polygons: append([]Polygon(nil), polygons...), crs: crs}, nil
}

// Type returns TypeMultiPolygon.
func (multi MultiPolygon) Type() GeometryType { return TypeMultiPolygon }

// CRS returns the declared coordinate reference system, including when empty.
func (multi MultiPolygon) CRS() CRS { return multi.crs }

// Len returns the number of polygons.
func (multi MultiPolygon) Len() int { return len(multi.polygons) }

// At returns an immutable polygon and false when index is out of range.
func (multi MultiPolygon) At(index int) (Polygon, bool) {
	if index < 0 || index >= len(multi.polygons) {
		return Polygon{}, false
	}

	return multi.polygons[index], true
}

// Polygons returns owned copies of all polygons.
func (multi MultiPolygon) Polygons() []Polygon { return append([]Polygon(nil), multi.polygons...) }

func (multi MultiPolygon) geometryMarker() {}

func (multi MultiPolygon) pointCount() int {
	count := 0
	for _, polygon := range multi.polygons {
		count += polygon.pointCount()
	}
	return count
}

func (multi MultiPolygon) geometryCount() int { return 1 }

func (multi MultiPolygon) geometryDepth() int { return 1 }

// GeometryCollection is an immutable, possibly empty heterogeneous collection
// in one CRS.
type GeometryCollection struct {
	geometries []Geometry
	crs        CRS
	points     int
	count      int
	depth      int
}

// NewGeometryCollection validates and owns zero or more geometries in one CRS.
func NewGeometryCollection(geometries []Geometry, crs CRS) (GeometryCollection, error) {
	return NewGeometryCollectionWithLimits(geometries, crs, DefaultLimits())
}

// NewGeometryCollectionWithLimits constructs a collection under resource limits.
func NewGeometryCollectionWithLimits(
	geometries []Geometry,
	crs CRS,
	limits Limits,
) (GeometryCollection, error) {
	limits = limits.withDefaults()
	if err := validateDeclaredCRS("geometry collection", crs); err != nil {
		return GeometryCollection{}, err
	}
	owned := make([]Geometry, len(geometries))
	points, count, depth := 0, 1, 1
	for index, geometry := range geometries {
		cloned, err := cloneGeometry(geometry)
		if err != nil {
			return GeometryCollection{}, err
		}
		if !cloned.CRS().Equal(crs) {
			return GeometryCollection{}, incompatibleCRSError(crs, cloned.CRS())
		}
		if exceeds(points, cloned.pointCount(), limits.MaxPoints) {
			return GeometryCollection{}, limitError("geometry collection", "point")
		}
		if exceeds(count, cloned.geometryCount(), limits.MaxGeometries) {
			return GeometryCollection{}, limitError("geometry collection", "geometry")
		}
		points += cloned.pointCount()
		count += cloned.geometryCount()
		depth = max(depth, cloned.geometryDepth()+1)
		owned[index] = cloned
	}
	if depth > limits.MaxCollectionDepth {
		return GeometryCollection{}, limitError("geometry collection", "depth")
	}

	return GeometryCollection{
		geometries: owned,
		crs:        crs,
		points:     points,
		count:      count,
		depth:      depth,
	}, nil
}

// Type returns TypeGeometryCollection.
func (collection GeometryCollection) Type() GeometryType { return TypeGeometryCollection }

// CRS returns the declared coordinate reference system, including when empty.
func (collection GeometryCollection) CRS() CRS { return collection.crs }

// Len returns the number of direct child geometries.
func (collection GeometryCollection) Len() int { return len(collection.geometries) }

// At returns an owned child geometry and false when index is out of range.
func (collection GeometryCollection) At(index int) (Geometry, bool) {
	if index < 0 || index >= len(collection.geometries) {
		return nil, false
	}
	geometry, _ := cloneGeometry(collection.geometries[index])
	return geometry, true
}

// Geometries returns owned copies of all direct child geometries.
func (collection GeometryCollection) Geometries() []Geometry {
	result := make([]Geometry, len(collection.geometries))
	for index, geometry := range collection.geometries {
		result[index], _ = cloneGeometry(geometry)
	}
	return result
}

func (collection GeometryCollection) geometryMarker() {}

func (collection GeometryCollection) pointCount() int { return collection.points }

func (collection GeometryCollection) geometryCount() int { return collection.count }

func (collection GeometryCollection) geometryDepth() int { return collection.depth }

// EqualGeometry compares geometry kind, CRS, coordinates, ring order, and
// collection order exactly. No tolerance or CRS transformation is applied.
func EqualGeometry(left, right Geometry) bool {
	if !supportedGeometry(left) || !supportedGeometry(right) {
		return false
	}
	left, leftErr := cloneGeometry(left)
	right, rightErr := cloneGeometry(right)
	if leftErr != nil || rightErr != nil || left.Type() != right.Type() {
		return false
	}
	equal := false
	switch value := left.(type) {
	case Point:
		equal = value.Equal(right.(Point))
	case LineString:
		equal = equalCoordinates(value.coordinates, right.(LineString).coordinates)
	case Polygon:
		equal = equalPolygon(value, right.(Polygon))
	case MultiPoint:
		equal = value.crs.Equal(right.(MultiPoint).crs) &&
			equalCoordinates(value.coordinates, right.(MultiPoint).coordinates)
	case MultiLineString:
		other := right.(MultiLineString)
		equal = value.crs.Equal(other.crs) && equalLines(value.lines, other.lines)
	case MultiPolygon:
		other := right.(MultiPolygon)
		equal = value.crs.Equal(other.crs) &&
			equalPolygons(value.polygons, other.polygons)
	case GeometryCollection:
		other := right.(GeometryCollection)
		if !value.crs.Equal(other.crs) || len(value.geometries) != len(other.geometries) {
			return false
		}
		equal = true
		for index := range value.geometries {
			if !EqualGeometry(value.geometries[index], other.geometries[index]) {
				equal = false
				break
			}
		}
	}
	return equal
}

func supportedGeometry(geometry Geometry) bool {
	switch geometry.(type) {
	case Point, *Point,
		LineString, *LineString,
		Polygon, *Polygon,
		MultiPoint, *MultiPoint,
		MultiLineString, *MultiLineString,
		MultiPolygon, *MultiPolygon,
		GeometryCollection, *GeometryCollection:
		return true
	default:
		return false
	}
}

func cloneGeometry(geometry Geometry) (Geometry, error) {
	switch value := geometry.(type) {
	case Point:
		return value, nil
	case *Point:
		if value != nil {
			return *value, nil
		}
	case LineString:
		return value, nil
	case *LineString:
		if value != nil {
			return *value, nil
		}
	case Polygon:
		return value, nil
	case *Polygon:
		if value != nil {
			return *value, nil
		}
	case MultiPoint:
		return value, nil
	case *MultiPoint:
		if value != nil {
			return *value, nil
		}
	case MultiLineString:
		return value, nil
	case *MultiLineString:
		if value != nil {
			return *value, nil
		}
	case MultiPolygon:
		return value, nil
	case *MultiPolygon:
		if value != nil {
			return *value, nil
		}
	case GeometryCollection:
		return value, nil
	case *GeometryCollection:
		if value != nil {
			return *value, nil
		}
	}

	return nil, &TopologyError{Geometry: "collection", Problem: "contains nil geometry"}
}

func validateDeclaredCRS(geometry string, crs CRS) error {
	if !crs.valid() {
		return &CRSError{SRID: crs.srid, Problem: geometry + " requires valid CRS metadata"}
	}
	return nil
}

func exceeds(total, addition, limit int) bool {
	return addition > limit || total > limit-addition
}

func limitError(geometry, resource string) *TopologyError {
	return &TopologyError{Geometry: geometry, Problem: resource + " limit exceeded"}
}

func equalCoordinates(left, right []Coordinate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Equal(right[index]) {
			return false
		}
	}
	return true
}

func equalPolygon(left, right Polygon) bool {
	if !left.crs.Equal(right.crs) || !equalCoordinates(left.exterior, right.exterior) || len(left.holes) != len(right.holes) {
		return false
	}
	for index := range left.holes {
		if !equalCoordinates(left.holes[index], right.holes[index]) {
			return false
		}
	}
	return true
}

func equalLines(left, right []LineString) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalCoordinates(left[index].coordinates, right[index].coordinates) {
			return false
		}
	}
	return true
}

func equalPolygons(left, right []Polygon) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !equalPolygon(left[index], right[index]) {
			return false
		}
	}
	return true
}

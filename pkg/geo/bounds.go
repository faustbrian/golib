package geo

// BoundingBox is an immutable, axis-aligned longitude/latitude envelope.
// West greater than east explicitly represents an antimeridian crossing.
// Boundaries are inclusive. Operations are O(1) and allocate no memory.
type BoundingBox struct {
	west  Longitude
	south Latitude
	east  Longitude
	north Latitude
	crs   CRS
}

// NewBoundingBox constructs an envelope in west, south, east, north order.
// Longitude reversal is meaningful and denotes an antimeridian crossing;
// latitude reversal is invalid.
func NewBoundingBox(
	west Longitude,
	south Latitude,
	east Longitude,
	north Latitude,
	crs CRS,
) (BoundingBox, error) {
	if south.degrees > north.degrees {
		return BoundingBox{}, newRangeError(
			"south latitude",
			south.degrees,
			minimumLatitude,
			north.degrees,
		)
	}
	if !crs.valid() {
		return BoundingBox{}, &CRSError{
			SRID:    crs.srid,
			Problem: "bounding box requires valid CRS metadata",
		}
	}

	return BoundingBox{
		west: west, south: south, east: east, north: north, crs: crs,
	}, nil
}

// West returns the inclusive western longitude.
func (bounds BoundingBox) West() Longitude { return bounds.west }

// South returns the inclusive southern latitude.
func (bounds BoundingBox) South() Latitude { return bounds.south }

// East returns the inclusive eastern longitude.
func (bounds BoundingBox) East() Longitude { return bounds.east }

// North returns the inclusive northern latitude.
func (bounds BoundingBox) North() Latitude { return bounds.north }

// CRS returns the bounds' coordinate reference system.
func (bounds BoundingBox) CRS() CRS { return bounds.crs }

// Equal reports exact edge and CRS equality.
func (bounds BoundingBox) Equal(other BoundingBox) bool { return bounds == other }

// CrossesAntimeridian reports whether the west-to-east interval crosses the
// antimeridian. A whole-world [-180, 180] box does not count as crossing.
func (bounds BoundingBox) CrossesAntimeridian() bool {
	return bounds.west.degrees > bounds.east.degrees
}

// Contains reports whether coordinate lies on or inside the bounds. It never
// transforms CRS values implicitly.
func (bounds BoundingBox) Contains(coordinate Coordinate) (bool, error) {
	if !bounds.crs.Equal(coordinate.crs) {
		return false, incompatibleCRSError(bounds.crs, coordinate.crs)
	}

	return coordinate.latitude.degrees >= bounds.south.degrees &&
		coordinate.latitude.degrees <= bounds.north.degrees &&
		bounds.containsLongitude(coordinate.longitude.degrees), nil
}

// Overlaps reports whether two inclusive envelopes share any position. It
// treats -180 and +180 as the same meridian and never transforms CRS values.
func (bounds BoundingBox) Overlaps(other BoundingBox) (bool, error) {
	if !bounds.crs.Equal(other.crs) {
		return false, incompatibleCRSError(bounds.crs, other.crs)
	}
	if bounds.north.degrees < other.south.degrees ||
		other.north.degrees < bounds.south.degrees {
		return false, nil
	}
	if bounds.wholeWorld() || other.wholeWorld() {
		return true, nil
	}

	return bounds.containsLongitude(other.west.degrees) ||
		bounds.containsLongitude(other.east.degrees) ||
		other.containsLongitude(bounds.west.degrees) ||
		other.containsLongitude(bounds.east.degrees), nil
}

func (bounds BoundingBox) wholeWorld() bool {
	return bounds.west.degrees == minimumLongitude &&
		bounds.east.degrees == maximumLongitude
}

func (bounds BoundingBox) containsLongitude(longitude float64) bool {
	if bounds.wholeWorld() {
		return true
	}

	longitude = canonicalDateline(longitude)
	west := canonicalDateline(bounds.west.degrees)
	east := canonicalDateline(bounds.east.degrees)
	if west <= east {
		return longitude >= west && longitude <= east
	}

	return longitude >= west || longitude <= east
}

func canonicalDateline(longitude float64) float64 {
	if longitude == maximumLongitude {
		return minimumLongitude
	}

	return longitude
}

func incompatibleCRSError(expected, actual CRS) *CRSError {
	return &CRSError{
		SRID:    actual.srid,
		Problem: "CRS " + actual.name + " does not match " + expected.name,
	}
}

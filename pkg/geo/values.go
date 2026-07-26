package geo

import (
	"encoding/json"
	"math"
)

const (
	minimumLongitude = -180.0
	maximumLongitude = 180.0
	minimumLatitude  = -90.0
	maximumLatitude  = 90.0
)

// Longitude is an immutable longitude in degrees in the inclusive range
// [-180, 180]. Negative zero is normalized to positive zero.
type Longitude struct{ degrees float64 }

// NewLongitude validates degrees and returns a longitude value.
func NewLongitude(degrees float64) (Longitude, error) {
	if !finite(degrees) || degrees < minimumLongitude || degrees > maximumLongitude {
		return Longitude{}, newRangeError("longitude", degrees, minimumLongitude, maximumLongitude)
	}

	return Longitude{degrees: normalizeZero(degrees)}, nil
}

// Degrees returns the longitude in decimal degrees.
func (longitude Longitude) Degrees() float64 { return longitude.degrees }

// Equal reports exact value equality after constructor normalization.
func (longitude Longitude) Equal(other Longitude) bool { return longitude == other }

// Latitude is an immutable latitude in degrees in the inclusive range
// [-90, 90]. Negative zero is normalized to positive zero.
type Latitude struct{ degrees float64 }

// NewLatitude validates degrees and returns a latitude value.
func NewLatitude(degrees float64) (Latitude, error) {
	if !finite(degrees) || degrees < minimumLatitude || degrees > maximumLatitude {
		return Latitude{}, newRangeError("latitude", degrees, minimumLatitude, maximumLatitude)
	}

	return Latitude{degrees: normalizeZero(degrees)}, nil
}

// Degrees returns the latitude in decimal degrees.
func (latitude Latitude) Degrees() float64 { return latitude.degrees }

// Equal reports exact value equality after constructor normalization.
func (latitude Latitude) Equal(other Latitude) bool { return latitude == other }

// Altitude is an immutable height in metres. It is not tied to a vertical
// datum; callers must carry that domain metadata separately when needed.
type Altitude struct{ metres float64 }

// NewAltitudeMeters accepts any finite height in metres.
func NewAltitudeMeters(metres float64) (Altitude, error) {
	if !finite(metres) {
		return Altitude{}, newRangeError("altitude", metres, -math.MaxFloat64, math.MaxFloat64)
	}

	return Altitude{metres: normalizeZero(metres)}, nil
}

// Meters returns the altitude in metres.
func (altitude Altitude) Meters() float64 { return altitude.metres }

// Equal reports exact value equality after constructor normalization.
func (altitude Altitude) Equal(other Altitude) bool { return altitude == other }

// Bearing is an immutable direction in degrees in the half-open range [0,
// 360). Constructors reject rather than silently normalize a full turn.
type Bearing struct{ degrees float64 }

// NewBearing validates degrees and returns a bearing value.
func NewBearing(degrees float64) (Bearing, error) {
	if !finite(degrees) || degrees < 0 || degrees >= 360 {
		return Bearing{}, newRangeError("bearing", degrees, 0, math.Nextafter(360, 0))
	}

	return Bearing{degrees: normalizeZero(degrees)}, nil
}

// Degrees returns the bearing in decimal degrees.
func (bearing Bearing) Degrees() float64 { return bearing.degrees }

// Equal reports exact value equality after constructor normalization.
func (bearing Bearing) Equal(other Bearing) bool { return bearing == other }

// Distance is an immutable non-negative length in metres.
type Distance struct{ metres float64 }

// NewDistanceMeters validates metres and returns a distance value.
func NewDistanceMeters(metres float64) (Distance, error) {
	if !finite(metres) || metres < 0 {
		return Distance{}, newRangeError("distance", metres, 0, math.MaxFloat64)
	}

	return Distance{metres: normalizeZero(metres)}, nil
}

// Meters returns the distance in metres.
func (distance Distance) Meters() float64 { return distance.metres }

// Kilometers returns the distance in kilometres.
func (distance Distance) Kilometers() float64 { return distance.metres / 1000 }

// Equal reports exact value equality after constructor normalization.
func (distance Distance) Equal(other Distance) bool { return distance == other }

// CRS identifies a coordinate reference system. The package never transforms
// coordinates between CRS values implicitly.
type CRS struct {
	srid int32
	name string
}

// NewCRS returns explicit CRS metadata. SRID must be positive and name must be
// non-empty.
func NewCRS(srid int32, name string) (CRS, error) {
	if srid <= 0 {
		return CRS{}, &CRSError{SRID: srid, Problem: "SRID must be positive"}
	}
	if name == "" {
		return CRS{}, &CRSError{SRID: srid, Problem: "name must not be empty"}
	}

	return CRS{srid: srid, name: name}, nil
}

// WGS84 returns the EPSG:4326 two-dimensional geographic CRS.
func WGS84() CRS { return CRS{srid: 4326, name: "EPSG:4326"} }

// SRID returns the positive spatial reference identifier.
func (crs CRS) SRID() int32 { return crs.srid }

// Name returns the human-readable CRS identity.
func (crs CRS) Name() string { return crs.name }

// Equal reports equality of both SRID and name.
func (crs CRS) Equal(other CRS) bool { return crs == other }

func (crs CRS) valid() bool { return crs.srid > 0 && crs.name != "" }

// MarshalJSON preserves both SRID and human-readable CRS identity.
func (crs CRS) MarshalJSON() ([]byte, error) {
	if !crs.valid() {
		return nil, &CRSError{SRID: crs.srid, Problem: "cannot marshal invalid CRS"}
	}
	return json.Marshal(struct {
		SRID int32  `json:"srid"`
		Name string `json:"name"`
	}{SRID: crs.srid, Name: crs.name})
}

// Coordinate is an immutable horizontal position. Its constructor order is
// always longitude, latitude; the distinct argument types prevent accidental
// interchange. CRS metadata is mandatory.
type Coordinate struct {
	longitude Longitude
	latitude  Latitude
	crs       CRS
}

// NewCoordinate returns a coordinate with explicit longitude-first order and
// CRS metadata.
func NewCoordinate(longitude Longitude, latitude Latitude, crs CRS) (Coordinate, error) {
	if !crs.valid() {
		return Coordinate{}, &CRSError{SRID: crs.srid, Problem: "coordinate requires valid CRS metadata"}
	}

	return Coordinate{longitude: longitude, latitude: latitude, crs: crs}, nil
}

// Longitude returns the coordinate's longitude.
func (coordinate Coordinate) Longitude() Longitude { return coordinate.longitude }

// Latitude returns the coordinate's latitude.
func (coordinate Coordinate) Latitude() Latitude { return coordinate.latitude }

// CRS returns the coordinate reference system metadata.
func (coordinate Coordinate) CRS() CRS { return coordinate.crs }

// Equal reports exact longitude, latitude, and CRS equality.
func (coordinate Coordinate) Equal(other Coordinate) bool { return coordinate == other }

// MarshalJSON uses named longitude and latitude members so root-package JSON
// cannot be mistaken for latitude-first coordinate arrays.
func (coordinate Coordinate) MarshalJSON() ([]byte, error) {
	if !coordinate.crs.valid() {
		return nil, &CRSError{
			SRID:    coordinate.crs.srid,
			Problem: "cannot marshal coordinate without valid CRS metadata",
		}
	}
	return json.Marshal(struct {
		Longitude float64 `json:"longitude"`
		Latitude  float64 `json:"latitude"`
		CRS       CRS     `json:"crs"`
	}{
		Longitude: coordinate.longitude.degrees,
		Latitude:  coordinate.latitude.degrees,
		CRS:       coordinate.crs,
	})
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func normalizeZero(value float64) float64 {
	if value == 0 {
		return 0
	}

	return value
}

func newRangeError(name string, value, minimum, maximum float64) *RangeError {
	return &RangeError{
		ValueName: name,
		Value:     value,
		Minimum:   minimum,
		Maximum:   maximum,
	}
}

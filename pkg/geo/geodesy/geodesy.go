// Package geodesy solves explicit spherical and ellipsoidal geodesic problems.
// All operations require EPSG:4326 coordinates and perform no CRS conversion.
package geodesy

import (
	"math"

	geo "github.com/faustbrian/golib/pkg/geo"
	geographiclib "github.com/pymaxion/geographiclib-go/v2/geodesic"
)

const meanEarthRadiusMeters = 6371008.8

// InverseResult is the immutable solution to an inverse geodesic problem.
// Bearings are undefined for coincident points and may be non-unique for
// antipodal points; callers must inspect BearingsDefined before using them.
type InverseResult struct {
	distance        geo.Distance
	initialBearing  geo.Bearing
	finalBearing    geo.Bearing
	bearingsDefined bool
}

// Distance returns the non-negative path length.
func (result InverseResult) Distance() geo.Distance { return result.distance }

// InitialBearing returns the forward bearing at the start coordinate.
func (result InverseResult) InitialBearing() geo.Bearing { return result.initialBearing }

// FinalBearing returns the forward bearing at the destination coordinate.
func (result InverseResult) FinalBearing() geo.Bearing { return result.finalBearing }

// BearingsDefined reports false for coincident points and exact mathematical
// antipodes. It does not certify uniqueness for near-antipodal geodesics.
func (result InverseResult) BearingsDefined() bool { return result.bearingsDefined }

// Sphere solves great-circle problems on a sphere of an explicit radius.
// Inverse and Destination are O(1), allocate no memory, and use IEEE 754
// double-precision trigonometry.
type Sphere struct{ radius geo.Distance }

// NewSphere constructs a spherical model. Radius must be greater than zero.
func NewSphere(radius geo.Distance) (Sphere, error) {
	if radius.Meters() == 0 {
		return Sphere{}, &geo.RangeError{
			ValueName: "sphere radius",
			Value:     0,
			Minimum:   math.SmallestNonzeroFloat64,
			Maximum:   math.MaxFloat64,
		}
	}

	return Sphere{radius: radius}, nil
}

// MeanEarthSphere returns the IUGG mean-radius sphere (6,371,008.8 metres).
func MeanEarthSphere() Sphere {
	radius, _ := geo.NewDistanceMeters(meanEarthRadiusMeters)
	return Sphere{radius: radius}
}

// Name returns the stable model name.
func (sphere Sphere) Name() string { return "IUGG mean Earth sphere" }

// Radius returns the sphere radius in metres.
func (sphere Sphere) Radius() geo.Distance { return sphere.radius }

// Inverse returns great-circle distance and forward bearings.
func (sphere Sphere) Inverse(from, to geo.Coordinate) (InverseResult, error) {
	if err := requireWGS84(from, to); err != nil {
		return InverseResult{}, err
	}
	if sphere.radius.Meters() == 0 {
		return InverseResult{}, &geo.RangeError{
			ValueName: "sphere radius",
			Value:     0,
			Minimum:   math.SmallestNonzeroFloat64,
			Maximum:   math.MaxFloat64,
		}
	}

	lat1 := radians(from.Latitude().Degrees())
	lat2 := radians(to.Latitude().Degrees())
	deltaLatitude := lat2 - lat1
	deltaLongitude := radians(to.Longitude().Degrees() - from.Longitude().Degrees())
	haversine := math.Sin(deltaLatitude/2)*math.Sin(deltaLatitude/2) +
		math.Cos(lat1)*math.Cos(lat2)*
			math.Sin(deltaLongitude/2)*math.Sin(deltaLongitude/2)
	haversine = math.Max(0, math.Min(1, haversine))
	centralAngle := 2 * math.Atan2(math.Sqrt(haversine), math.Sqrt(1-haversine))
	distance := mustDistance(sphere.radius.Meters() * centralAngle)
	if centralAngle == 0 {
		return InverseResult{distance: distance}, nil
	}

	initial := sphereBearing(lat1, radians(from.Longitude().Degrees()), lat2,
		radians(to.Longitude().Degrees()))
	reverse := sphereBearing(lat2, radians(to.Longitude().Degrees()), lat1,
		radians(from.Longitude().Degrees()))
	return InverseResult{
		distance:        distance,
		initialBearing:  mustBearing(initial),
		finalBearing:    mustBearing(reverse + 180),
		bearingsDefined: math.Abs(math.Pi-centralAngle) > 1e-15,
	}, nil
}

// Destination solves the direct great-circle problem and returns the endpoint
// and forward bearing there.
func (sphere Sphere) Destination(
	from geo.Coordinate,
	initial geo.Bearing,
	distance geo.Distance,
) (geo.Coordinate, geo.Bearing, error) {
	if err := requireWGS84(from); err != nil {
		return geo.Coordinate{}, geo.Bearing{}, err
	}
	if sphere.radius.Meters() == 0 {
		return geo.Coordinate{}, geo.Bearing{}, &geo.RangeError{
			ValueName: "sphere radius",
			Value:     0,
			Minimum:   math.SmallestNonzeroFloat64,
			Maximum:   math.MaxFloat64,
		}
	}
	if distance.Meters() == 0 {
		return from, initial, nil
	}

	lat1 := radians(from.Latitude().Degrees())
	lon1 := radians(from.Longitude().Degrees())
	azimuth := radians(initial.Degrees())
	angularDistance := distance.Meters() / sphere.radius.Meters()
	lat2 := math.Asin(
		math.Sin(lat1)*math.Cos(angularDistance) +
			math.Cos(lat1)*math.Sin(angularDistance)*math.Cos(azimuth),
	)
	lon2 := lon1 + math.Atan2(
		math.Sin(azimuth)*math.Sin(angularDistance)*math.Cos(lat1),
		math.Cos(angularDistance)-math.Sin(lat1)*math.Sin(lat2),
	)
	lon2 = normalizeLongitudeDegrees(degrees(lon2))
	coordinate := coordinate(lon2, clampLatitude(degrees(lat2)))
	reverse := sphereBearing(lat2, radians(lon2), lat1, lon1)

	return coordinate, mustBearing(reverse + 180), nil
}

// Ellipsoid solves geodesics on the WGS84 reference ellipsoid using the
// GeographicLib algorithms of Karney (2013).
type Ellipsoid struct{}

// WGS84Ellipsoid returns the WGS84 ellipsoidal model (a=6378137 m,
// f=1/298.257223563).
func WGS84Ellipsoid() Ellipsoid { return Ellipsoid{} }

// Name returns the stable model name.
func (Ellipsoid) Name() string { return "WGS84 ellipsoid" }

// Inverse solves the ellipsoidal inverse problem using GeographicLib.
func (Ellipsoid) Inverse(from, to geo.Coordinate) (InverseResult, error) {
	if err := requireWGS84(from, to); err != nil {
		return InverseResult{}, err
	}
	if from.Equal(to) {
		return InverseResult{distance: mustDistance(0)}, nil
	}

	result := geographiclib.WGS84.Inverse(
		from.Latitude().Degrees(),
		from.Longitude().Degrees(),
		to.Latitude().Degrees(),
		to.Longitude().Degrees(),
	)

	return InverseResult{
		distance:        mustDistance(result.S12),
		initialBearing:  mustBearing(result.Azi1),
		finalBearing:    mustBearing(result.Azi2),
		bearingsDefined: !antipodal(from, to),
	}, nil
}

// Destination solves the ellipsoidal direct problem using GeographicLib.
func (Ellipsoid) Destination(
	from geo.Coordinate,
	initial geo.Bearing,
	distance geo.Distance,
) (geo.Coordinate, geo.Bearing, error) {
	if err := requireWGS84(from); err != nil {
		return geo.Coordinate{}, geo.Bearing{}, err
	}

	result := geographiclib.WGS84.Direct(
		from.Latitude().Degrees(),
		from.Longitude().Degrees(),
		initial.Degrees(),
		distance.Meters(),
	)
	destination := coordinate(result.Lon2, result.Lat2)

	return destination, mustBearing(result.Azi2), nil
}

func requireWGS84(coordinates ...geo.Coordinate) error {
	for _, coordinate := range coordinates {
		if !coordinate.CRS().Equal(geo.WGS84()) {
			return &geo.CRSError{
				SRID: coordinate.CRS().SRID(),
				Problem: "geodesy requires EPSG:4326; no CRS " +
					"transformation is performed",
			}
		}
	}

	return nil
}

func coordinate(longitude, latitude float64) geo.Coordinate {
	lon, _ := geo.NewLongitude(normalizeLongitudeDegrees(longitude))
	lat, _ := geo.NewLatitude(clampLatitude(latitude))
	result, _ := geo.NewCoordinate(lon, lat, geo.WGS84())
	return result
}

func sphereBearing(lat1, lon1, lat2, lon2 float64) float64 {
	deltaLongitude := lon2 - lon1
	y := math.Sin(deltaLongitude) * math.Cos(lat2)
	x := math.Cos(lat1)*math.Sin(lat2) -
		math.Sin(lat1)*math.Cos(lat2)*math.Cos(deltaLongitude)

	return degrees(math.Atan2(y, x))
}

func mustDistance(metres float64) geo.Distance {
	distance, _ := geo.NewDistanceMeters(metres)
	return distance
}

func mustBearing(value float64) geo.Bearing {
	bearing, _ := geo.NewBearing(normalizeBearingDegrees(value))
	return bearing
}

func normalizeBearingDegrees(value float64) float64 {
	value = math.Mod(value, 360)
	if value < 0 {
		value += 360
	}
	return value
}

func normalizeLongitudeDegrees(value float64) float64 {
	value = math.Mod(value+180, 360)
	if value < 0 {
		value += 360
	}

	return value - 180
}

func antipodal(from, to geo.Coordinate) bool {
	fromLatitude := from.Latitude().Degrees()
	toLatitude := to.Latitude().Degrees()
	if toLatitude != -fromLatitude {
		return false
	}
	if math.Abs(fromLatitude) == 90 {
		return true
	}
	deltaLongitude := normalizeLongitudeDegrees(
		to.Longitude().Degrees() - from.Longitude().Degrees(),
	)
	return math.Abs(deltaLongitude) == 180
}

func clampLatitude(value float64) float64 {
	return math.Max(-90, math.Min(90, value))
}

func radians(degrees float64) float64 { return degrees * math.Pi / 180 }

func degrees(radians float64) float64 { return radians * 180 / math.Pi }

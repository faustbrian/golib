// Package geotest provides reusable numerical tolerances, assertions, and
// authoritative conformance vectors for consumers of geo.
package geotest

import (
	"math"

	geo "github.com/faustbrian/golib/pkg/geo"
)

// Tolerance combines an absolute floor and a scale-relative bound. A value is
// within tolerance when its absolute difference is no greater than the larger
// of Absolute and Relative times the larger magnitude.
type Tolerance struct {
	absolute float64
	relative float64
}

// NewTolerance constructs finite, non-negative numerical tolerances.
func NewTolerance(absolute, relative float64) (Tolerance, error) {
	if math.IsNaN(absolute) || math.IsInf(absolute, 0) || absolute < 0 {
		return Tolerance{}, toleranceError("absolute tolerance", absolute)
	}
	if math.IsNaN(relative) || math.IsInf(relative, 0) || relative < 0 {
		return Tolerance{}, toleranceError("relative tolerance", relative)
	}
	return Tolerance{absolute: absolute, relative: relative}, nil
}

// Absolute returns the absolute tolerance floor.
func (tolerance Tolerance) Absolute() float64 { return tolerance.absolute }

// Relative returns the scale-relative tolerance multiplier.
func (tolerance Tolerance) Relative() float64 { return tolerance.relative }

// Within reports whether two finite numbers satisfy this tolerance.
func (tolerance Tolerance) Within(got, want float64) bool {
	if math.IsNaN(got) || math.IsNaN(want) ||
		math.IsInf(got, 0) || math.IsInf(want, 0) {
		return false
	}
	difference := math.Abs(got - want)
	scale := math.Max(math.Abs(got), math.Abs(want))
	return difference <= math.Max(tolerance.absolute, tolerance.relative*scale)
}

// TestingT is the assertion surface implemented by testing.T and testing.B.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
}

// AssertClose reports a test error when got and want exceed tolerance.
func AssertClose(
	testing TestingT,
	name string,
	got, want float64,
	tolerance Tolerance,
) {
	testing.Helper()
	if !tolerance.Within(got, want) {
		testing.Errorf(
			"%s = %.17g, want %.17g within absolute %g or relative %g",
			name,
			got,
			want,
			tolerance.Absolute(),
			tolerance.Relative(),
		)
	}
}

// InverseVector is an authoritative WGS84 inverse-geodesic case. Coordinates
// are explicitly longitude first; distances are metres and bearings degrees.
type InverseVector struct {
	Name           string
	FromLongitude  float64
	FromLatitude   float64
	ToLongitude    float64
	ToLatitude     float64
	DistanceMeters float64
	InitialBearing float64
	FinalBearing   float64
}

// DistanceVector is an authoritative WGS84 inverse-distance case whose
// bearings are intentionally not part of the assertion. Coordinates are
// longitude first, distances and absolute tolerances are metres.
type DistanceVector struct {
	Name                    string
	Source                  string
	FromLongitude           float64
	FromLatitude            float64
	ToLongitude             float64
	ToLatitude              float64
	DistanceMeters          float64
	AbsoluteToleranceMeters float64
}

// PolygonProbe is a longitude-first point with its expected OGC location.
type PolygonProbe struct {
	Name      string
	Longitude float64
	Latitude  float64
	Location  geo.Location
}

// PolygonLocationVector is an authoritative planar polygon and point-location
// case. Rings contain longitude-first coordinate pairs and are closed.
type PolygonLocationVector struct {
	Name     string
	Exterior [][2]float64
	Holes    [][][2]float64
	Probes   []PolygonProbe
}

var wgs84InverseVectors = []InverseVector{
	{
		Name:           "GeographicLib Wellington to Salamanca",
		FromLongitude:  174.81,
		FromLatitude:   -41.32,
		ToLongitude:    -5.50,
		ToLatitude:     40.96,
		DistanceMeters: 19959679.26735382,
		InitialBearing: 161.06766998616015,
		FinalBearing:   18.825195123248392,
	},
	{
		Name:           "GeographicLib antimeridian regression",
		FromLongitude:  173.34268,
		FromLatitude:   -17.42761,
		ToLongitude:    5.93557,
		ToLatitude:     -15.84784,
		DistanceMeters: 16076603.163118067,
		InitialBearing: 200.96644233880707,
		FinalBearing:   339.212515348463,
	},
	{
		Name:           "GeographicLib near-pole regression",
		FromLongitude:  85.66836,
		FromLatitude:   -87.85331,
		ToLongitude:    16.09921,
		ToLatitude:     66.48646,
		DistanceMeters: 17286615.314714465,
		InitialBearing: 294.87968695975725,
		FinalBearing:   355.1113412807277,
	},
}

var wgs84DistanceVectors = []DistanceVector{
	{
		Name:                    "GeodSolve4 tiny line regression",
		Source:                  "GeographicLib GeodSolve4",
		FromLongitude:           0,
		FromLatitude:            36.493349428792,
		ToLongitude:             0.0000008,
		ToLatitude:              36.49334942879201,
		DistanceMeters:          0.072,
		AbsoluteToleranceMeters: 0.0005,
	},
	{
		Name:                    "GeodSolve6 near-antipodal polar regression",
		Source:                  "GeographicLib GeodSolve6",
		FromLongitude:           0,
		FromLatitude:            88.202499451857,
		ToLongitude:             179.98102203299286,
		ToLatitude:              -88.202499451857,
		DistanceMeters:          20003898.214,
		AbsoluteToleranceMeters: 0.0005,
	},
	{
		Name:                    "GeodSolve9 near-antipodal regression",
		Source:                  "GeographicLib GeodSolve9",
		FromLongitude:           0,
		FromLatitude:            56.320923501171,
		ToLongitude:             179.66474767177288,
		ToLatitude:              -56.320923501171,
		DistanceMeters:          19993558.287,
		AbsoluteToleranceMeters: 0.0005,
	},
	{
		Name:                    "WGS84 equatorial antipodes",
		Source:                  "GeographicLib GeodSolve antipodal regression",
		FromLongitude:           0,
		FromLatitude:            0,
		ToLongitude:             180,
		ToLatitude:              0,
		DistanceMeters:          20003931.458625447,
		AbsoluteToleranceMeters: 0.000001,
	},
	{
		Name:                    "WGS84 pole-to-pole meridian",
		Source:                  "GeographicLib WGS84 meridian",
		FromLongitude:           0,
		FromLatitude:            90,
		ToLongitude:             0,
		ToLatitude:              -90,
		DistanceMeters:          20003931.458625447,
		AbsoluteToleranceMeters: 0.000001,
	},
}

var polygonLocationVectors = []PolygonLocationVector{
	{
		Name: "OGC Simple Features 1.1 polygon text example",
		Exterior: [][2]float64{
			{10, 10},
			{10, 20},
			{20, 20},
			{20, 15},
			{10, 10},
		},
		Probes: []PolygonProbe{
			{Name: "interior", Longitude: 15, Latitude: 17, Location: geo.Inside},
			{Name: "boundary", Longitude: 10, Latitude: 15, Location: geo.Boundary},
			{Name: "exterior", Longitude: 5, Latitude: 15, Location: geo.Outside},
		},
	},
	{
		Name: "OGC Simple Features interior ring semantics",
		Exterior: [][2]float64{
			{0, 0}, {0, 10}, {10, 10}, {10, 0}, {0, 0},
		},
		Holes: [][][2]float64{
			{{3, 3}, {3, 7}, {7, 7}, {7, 3}, {3, 3}},
		},
		Probes: []PolygonProbe{
			{Name: "surface interior", Longitude: 1, Latitude: 1, Location: geo.Inside},
			{Name: "hole interior", Longitude: 5, Latitude: 5, Location: geo.Outside},
			{Name: "hole boundary", Longitude: 3, Latitude: 5, Location: geo.Boundary},
		},
	},
}

// WGS84InverseVectors returns an owned copy of the GeographicLib conformance
// vectors shipped by this package.
func WGS84InverseVectors() []InverseVector {
	return append([]InverseVector(nil), wgs84InverseVectors...)
}

// WGS84DistanceVectors returns an owned copy of edge-case GeographicLib
// conformance vectors with their explicit absolute error budgets.
func WGS84DistanceVectors() []DistanceVector {
	return append([]DistanceVector(nil), wgs84DistanceVectors...)
}

// PolygonLocationVectors returns owned OGC Simple Features conformance cases.
func PolygonLocationVectors() []PolygonLocationVector {
	result := make([]PolygonLocationVector, len(polygonLocationVectors))
	for index, vector := range polygonLocationVectors {
		result[index] = PolygonLocationVector{
			Name:     vector.Name,
			Exterior: append([][2]float64(nil), vector.Exterior...),
			Holes:    cloneRings(vector.Holes),
			Probes:   append([]PolygonProbe(nil), vector.Probes...),
		}
	}
	return result
}

func cloneRings(rings [][][2]float64) [][][2]float64 {
	result := make([][][2]float64, len(rings))
	for index, ring := range rings {
		result[index] = append([][2]float64(nil), ring...)
	}
	return result
}

func toleranceError(name string, value float64) error {
	return &geo.RangeError{
		ValueName: name,
		Value:     value,
		Minimum:   0,
		Maximum:   math.MaxFloat64,
	}
}

package geodesy

import (
	"math"
	"sort"

	geo "github.com/faustbrian/golib/pkg/geo"
)

// Model is the package-owned contract for a geodesic distance model.
type Model interface {
	Name() string
	Inverse(from, to geo.Coordinate) (InverseResult, error)
	Destination(
		from geo.Coordinate,
		initial geo.Bearing,
		distance geo.Distance,
	) (geo.Coordinate, geo.Bearing, error)
}

// RadiusEnvelope returns the smallest axis-aligned spherical envelope around
// center. It is O(1), allocates no heap memory, and is exact for this sphere.
// Envelopes touching a pole span every longitude.
func (sphere Sphere) RadiusEnvelope(
	center geo.Coordinate,
	radius geo.Distance,
) (geo.BoundingBox, error) {
	if err := requireWGS84(center); err != nil {
		return geo.BoundingBox{}, err
	}
	if sphere.radius.Meters() == 0 {
		return geo.BoundingBox{}, &geo.RangeError{
			ValueName: "sphere radius",
			Value:     0,
			Minimum:   math.SmallestNonzeroFloat64,
			Maximum:   math.MaxFloat64,
		}
	}

	angularRadius := radius.Meters() / sphere.radius.Meters()
	if angularRadius >= math.Pi {
		return boundingBox(-180, -90, 180, 90)
	}
	latitude := radians(center.Latitude().Degrees())
	south := math.Max(-math.Pi/2, latitude-angularRadius)
	north := math.Min(math.Pi/2, latitude+angularRadius)
	if south <= -math.Pi/2 || north >= math.Pi/2 {
		return boundingBox(-180, degrees(south), 180, degrees(north))
	}

	deltaLongitude := math.Asin(math.Sin(angularRadius) / math.Cos(latitude))
	deltaLongitude = outward(deltaLongitude, math.Inf(1))
	west := normalizeLongitudeDegrees(
		center.Longitude().Degrees() - degrees(deltaLongitude),
	)
	east := normalizeLongitudeDegrees(
		center.Longitude().Degrees() + degrees(deltaLongitude),
	)
	return boundingBox(
		west,
		outward(degrees(south), math.Inf(-1)),
		east,
		outward(degrees(north), math.Inf(1)),
	)
}

// LineLength sums segment distances using model. It is O(n), allocates no
// memory, and inherits the selected model's numerical behavior.
func LineLength(model Model, line geo.LineString) (geo.Distance, error) {
	if model == nil {
		return geo.Distance{}, missingModelError()
	}
	total := 0.0
	previous, _ := line.At(0)
	for index := 1; index < line.Len(); index++ {
		current, _ := line.At(index)
		result, err := model.Inverse(previous, current)
		if err != nil {
			return geo.Distance{}, err
		}
		total += result.Distance().Meters()
		previous = current
	}
	return geo.NewDistanceMeters(total)
}

// PolygonPerimeter sums exterior and interior ring segment distances. It is
// O(n) in total ring points, allocates O(n) for immutable ring copies, and
// inherits the model's numerical behavior.
func PolygonPerimeter(model Model, polygon geo.Polygon) (geo.Distance, error) {
	if model == nil {
		return geo.Distance{}, missingModelError()
	}
	total, err := ringLength(model, polygon.Exterior())
	if err != nil {
		return geo.Distance{}, err
	}
	for _, hole := range polygon.Holes() {
		length, ringErr := ringLength(model, hole)
		if ringErr != nil {
			return geo.Distance{}, ringErr
		}
		total += length
	}
	return geo.NewDistanceMeters(total)
}

// Neighbor is a coordinate paired with its calculated model distance.
type Neighbor struct {
	coordinate geo.Coordinate
	distance   geo.Distance
}

// Coordinate returns the ranked candidate coordinate.
func (neighbor Neighbor) Coordinate() geo.Coordinate { return neighbor.coordinate }

// Distance returns the model distance from the ranking origin.
func (neighbor Neighbor) Distance() geo.Distance { return neighbor.distance }

// Nearest ranks an in-memory candidate set by model distance and returns at
// most limit entries. It is O(n log n), allocates O(n), is stable for equal
// distances, and does not replace a spatial database index.
func Nearest(
	model Model,
	origin geo.Coordinate,
	candidates []geo.Coordinate,
	limit int,
) ([]Neighbor, error) {
	if model == nil {
		return nil, missingModelError()
	}
	if limit < 0 {
		return nil, &geo.RangeError{
			ValueName: "nearest limit",
			Value:     float64(limit),
			Minimum:   0,
			Maximum:   float64(math.MaxInt),
		}
	}
	ranked := make([]Neighbor, len(candidates))
	for index, candidate := range candidates {
		result, err := model.Inverse(origin, candidate)
		if err != nil {
			return nil, err
		}
		ranked[index] = Neighbor{
			coordinate: candidate,
			distance:   result.Distance(),
		}
	}
	sort.SliceStable(ranked, func(left, right int) bool {
		return ranked[left].distance.Meters() < ranked[right].distance.Meters()
	})
	if limit < len(ranked) {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func ringLength(model Model, ring []geo.Coordinate) (float64, error) {
	total := 0.0
	for index := 1; index < len(ring); index++ {
		result, err := model.Inverse(ring[index-1], ring[index])
		if err != nil {
			return 0, err
		}
		total += result.Distance().Meters()
	}
	return total, nil
}

func boundingBox(west, south, east, north float64) (geo.BoundingBox, error) {
	westLongitude, err := geo.NewLongitude(west)
	if err != nil {
		return geo.BoundingBox{}, err
	}
	southLatitude, err := geo.NewLatitude(south)
	if err != nil {
		return geo.BoundingBox{}, err
	}
	eastLongitude, err := geo.NewLongitude(east)
	if err != nil {
		return geo.BoundingBox{}, err
	}
	northLatitude, err := geo.NewLatitude(north)
	if err != nil {
		return geo.BoundingBox{}, err
	}
	return geo.NewBoundingBox(
		westLongitude,
		southLatitude,
		eastLongitude,
		northLatitude,
		geo.WGS84(),
	)
}

func missingModelError() error {
	return &geo.UnsupportedError{
		Operation: "geodesic operation",
		Reason:    "a model is required",
	}
}

func outward(value, direction float64) float64 {
	for range 8 {
		value = math.Nextafter(value, direction)
	}
	return value
}

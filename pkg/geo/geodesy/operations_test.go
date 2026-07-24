package geodesy_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
)

func TestSphericalRadiusEnvelopeContainsCardinalDestinations(t *testing.T) {
	t.Parallel()

	model := geodesy.MeanEarthSphere()
	center := mustCoordinate(t, 24.9384, 60.1699)
	radius := mustDistance(t, 100_000)
	bounds, err := model.RadiusEnvelope(center, radius)
	if err != nil {
		t.Fatalf("RadiusEnvelope() error = %v", err)
	}
	for _, degrees := range []float64{0, 90, 180, 270} {
		destination, _, destinationErr := model.Destination(
			center,
			mustBearing(t, degrees),
			radius,
		)
		if destinationErr != nil {
			t.Fatalf("Destination(%v) error = %v", degrees, destinationErr)
		}
		inside, containsErr := bounds.Contains(destination)
		if containsErr != nil {
			t.Fatalf("Contains(%v) error = %v", degrees, containsErr)
		}
		if !inside {
			t.Fatalf(
				"envelope [%v,%v,%v,%v] does not contain %v-degree destination [%v,%v]",
				bounds.West().Degrees(),
				bounds.South().Degrees(),
				bounds.East().Degrees(),
				bounds.North().Degrees(),
				degrees,
				destination.Longitude().Degrees(),
				destination.Latitude().Degrees(),
			)
		}
	}
}

func TestSphericalRadiusEnvelopeAtPoleSpansWorld(t *testing.T) {
	t.Parallel()

	model := geodesy.MeanEarthSphere()
	bounds, err := model.RadiusEnvelope(
		mustCoordinate(t, 45, 90),
		mustDistance(t, 1_000),
	)
	if err != nil {
		t.Fatalf("RadiusEnvelope() error = %v", err)
	}
	if bounds.West().Degrees() != -180 || bounds.East().Degrees() != 180 {
		t.Fatalf("polar longitude = [%v, %v], want whole world",
			bounds.West().Degrees(), bounds.East().Degrees())
	}
}

func TestMeasurementsAndNearestCandidatesUseSelectedModel(t *testing.T) {
	t.Parallel()

	model := geodesy.MeanEarthSphere()
	origin := mustCoordinate(t, 0, 0)
	east := mustCoordinate(t, 1, 0)
	north := mustCoordinate(t, 0, 2)
	line, err := geo.NewLineString([]geo.Coordinate{origin, east, north})
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	wantFirst, err := model.Inverse(origin, east)
	if err != nil {
		t.Fatalf("Inverse(first) error = %v", err)
	}
	wantSecond, err := model.Inverse(east, north)
	if err != nil {
		t.Fatalf("Inverse(second) error = %v", err)
	}
	length, err := geodesy.LineLength(model, line)
	if err != nil {
		t.Fatalf("LineLength() error = %v", err)
	}
	wantLength := wantFirst.Distance().Meters() + wantSecond.Distance().Meters()
	if difference := length.Meters() - wantLength; difference < -1e-9 || difference > 1e-9 {
		t.Fatalf("LineLength() = %v, want %v", length.Meters(), wantLength)
	}

	ranked, err := geodesy.Nearest(model, origin, []geo.Coordinate{north, east}, 1)
	if err != nil {
		t.Fatalf("Nearest() error = %v", err)
	}
	if len(ranked) != 1 || !ranked[0].Coordinate().Equal(east) {
		t.Fatal("Nearest() did not return the closest coordinate")
	}
	if ranked[0].Distance().Meters() != wantFirst.Distance().Meters() {
		t.Fatal("Nearest() returned an inconsistent distance")
	}
}

func TestNearestRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	_, err := geodesy.Nearest(
		geodesy.MeanEarthSphere(),
		mustCoordinate(t, 0, 0),
		nil,
		-1,
	)
	if err == nil {
		t.Fatal("Nearest() accepted a negative limit")
	}
}

func TestRadiusEnvelopeRejectsInvalidModelsAndCoversTheWorld(t *testing.T) {
	t.Parallel()

	center := mustCoordinate(t, 0, 0)
	world, err := geodesy.MeanEarthSphere().RadiusEnvelope(
		center,
		mustDistance(t, 40_000_000),
	)
	if err != nil {
		t.Fatalf("RadiusEnvelope(world) error = %v", err)
	}
	if world.West().Degrees() != -180 || world.East().Degrees() != 180 ||
		world.South().Degrees() != -90 || world.North().Degrees() != 90 {
		t.Fatal("world envelope does not span the globe")
	}

	otherCRS, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	foreign := coordinateWithCRS(t, 0, 0, otherCRS)
	if _, err := geodesy.MeanEarthSphere().RadiusEnvelope(foreign, geo.Distance{}); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("RadiusEnvelope(CRS) error = %v, want ErrCRS", err)
	}
	var zero geodesy.Sphere
	if _, err := zero.RadiusEnvelope(center, geo.Distance{}); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("zero RadiusEnvelope() error = %v, want ErrRange", err)
	}
}

func TestPolygonPerimeterIncludesHoles(t *testing.T) {
	t.Parallel()

	exterior := closedRing(t, [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {0, 0}})
	hole := closedRing(t, [][2]float64{{1, 1}, {1, 2}, {2, 2}, {2, 1}, {1, 1}})
	polygon, err := geo.NewPolygon(exterior, [][]geo.Coordinate{hole})
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	model := geodesy.MeanEarthSphere()
	perimeter, err := geodesy.PolygonPerimeter(model, polygon)
	if err != nil {
		t.Fatalf("PolygonPerimeter() error = %v", err)
	}
	exteriorLine, err := geo.NewLineString(exterior)
	if err != nil {
		t.Fatalf("NewLineString(exterior) error = %v", err)
	}
	holeLine, err := geo.NewLineString(hole)
	if err != nil {
		t.Fatalf("NewLineString(hole) error = %v", err)
	}
	exteriorLength, _ := geodesy.LineLength(model, exteriorLine)
	holeLength, _ := geodesy.LineLength(model, holeLine)
	want := exteriorLength.Meters() + holeLength.Meters()
	if difference := perimeter.Meters() - want; difference < -1e-9 || difference > 1e-9 {
		t.Fatalf("PolygonPerimeter() = %v, want %v", perimeter.Meters(), want)
	}
}

func TestOperationsRejectNilModelsAndPropagateModelFailures(t *testing.T) {
	t.Parallel()

	origin := mustCoordinate(t, 0, 0)
	east := mustCoordinate(t, 1, 0)
	line, err := geo.NewLineString([]geo.Coordinate{origin, east})
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	polygon, err := geo.NewPolygon(
		closedRing(t, [][2]float64{{0, 0}, {2, 0}, {2, 2}, {0, 2}, {0, 0}}),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	if _, err := geodesy.LineLength(nil, line); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("LineLength(nil) error = %v, want ErrUnsupported", err)
	}
	if _, err := geodesy.PolygonPerimeter(nil, polygon); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("PolygonPerimeter(nil) error = %v, want ErrUnsupported", err)
	}
	if _, err := geodesy.Nearest(nil, origin, nil, 0); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("Nearest(nil) error = %v, want ErrUnsupported", err)
	}

	wantErr := errors.New("model failure")
	failing := &failingModel{failAt: 1, err: wantErr}
	if _, err := geodesy.LineLength(failing, line); !errors.Is(err, wantErr) {
		t.Fatalf("LineLength(failing) error = %v, want model failure", err)
	}
	failing = &failingModel{failAt: 1, err: wantErr}
	if _, err := geodesy.PolygonPerimeter(failing, polygon); !errors.Is(err, wantErr) {
		t.Fatalf("PolygonPerimeter(failing exterior) error = %v, want model failure", err)
	}
	polygonWithHole, err := geo.NewPolygon(
		closedRing(t, [][2]float64{{0, 0}, {4, 0}, {4, 4}, {0, 4}, {0, 0}}),
		[][]geo.Coordinate{closedRing(t, [][2]float64{{1, 1}, {1, 2}, {2, 2}, {2, 1}, {1, 1}})},
	)
	if err != nil {
		t.Fatalf("NewPolygon(hole) error = %v", err)
	}
	failing = &failingModel{failAt: 5, err: wantErr}
	if _, err := geodesy.PolygonPerimeter(failing, polygonWithHole); !errors.Is(err, wantErr) {
		t.Fatalf("PolygonPerimeter(failing hole) error = %v, want model failure", err)
	}
	failing = &failingModel{failAt: 1, err: wantErr}
	if _, err := geodesy.Nearest(failing, origin, []geo.Coordinate{east}, 1); !errors.Is(err, wantErr) {
		t.Fatalf("Nearest(failing) error = %v, want model failure", err)
	}
}

func TestNearestIsStableAndSupportsZeroLimit(t *testing.T) {
	t.Parallel()

	origin := mustCoordinate(t, 0, 0)
	east := mustCoordinate(t, 1, 0)
	west := mustCoordinate(t, -1, 0)
	ranked, err := geodesy.Nearest(
		geodesy.MeanEarthSphere(),
		origin,
		[]geo.Coordinate{west, east},
		2,
	)
	if err != nil {
		t.Fatalf("Nearest() error = %v", err)
	}
	if !ranked[0].Coordinate().Equal(west) || !ranked[1].Coordinate().Equal(east) {
		t.Fatal("Nearest() did not preserve equal-distance input order")
	}
	empty, err := geodesy.Nearest(geodesy.MeanEarthSphere(), origin, []geo.Coordinate{east}, 0)
	if err != nil {
		t.Fatalf("Nearest(limit zero) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("Nearest(limit zero) length = %d, want 0", len(empty))
	}
}

type failingModel struct {
	calls  int
	failAt int
	err    error
}

func (*failingModel) Name() string { return "failing" }

func (model *failingModel) Inverse(from, to geo.Coordinate) (geodesy.InverseResult, error) {
	model.calls++
	if model.calls == model.failAt {
		return geodesy.InverseResult{}, model.err
	}
	return geodesy.MeanEarthSphere().Inverse(from, to)
}

func (*failingModel) Destination(
	from geo.Coordinate,
	initial geo.Bearing,
	distance geo.Distance,
) (geo.Coordinate, geo.Bearing, error) {
	return geodesy.MeanEarthSphere().Destination(from, initial, distance)
}

func closedRing(t *testing.T, points [][2]float64) []geo.Coordinate {
	t.Helper()
	ring := make([]geo.Coordinate, len(points))
	for index, point := range points {
		ring[index] = mustCoordinate(t, point[0], point[1])
	}
	return ring
}

func mustCoordinate(t *testing.T, longitude, latitude float64) geo.Coordinate {
	t.Helper()

	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}
	return coordinate
}

func coordinateWithCRS(t *testing.T, longitude, latitude float64, crs geo.CRS) geo.Coordinate {
	t.Helper()

	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, crs)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}
	return coordinate
}

func mustDistance(t *testing.T, meters float64) geo.Distance {
	t.Helper()

	distance, err := geo.NewDistanceMeters(meters)
	if err != nil {
		t.Fatalf("NewDistanceMeters() error = %v", err)
	}
	return distance
}

func mustBearing(t *testing.T, degrees float64) geo.Bearing {
	t.Helper()

	bearing, err := geo.NewBearing(degrees)
	if err != nil {
		t.Fatalf("NewBearing() error = %v", err)
	}
	return bearing
}

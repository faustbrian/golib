package geodesy_test

import (
	"errors"
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
	"github.com/faustbrian/golib/pkg/geo/geotest"
)

func TestMeanEarthSphereSolvesQuarterEquator(t *testing.T) {
	t.Parallel()

	model := geodesy.MeanEarthSphere()
	from := coordinate(t, 0, 0, geo.WGS84())
	to := coordinate(t, 90, 0, geo.WGS84())

	result, err := model.Inverse(from, to)
	if err != nil {
		t.Fatalf("Inverse() error = %v", err)
	}
	wantDistance := math.Pi * model.Radius().Meters() / 2
	closeTo(t, "distance", result.Distance().Meters(), wantDistance, 1e-6)
	if !result.BearingsDefined() {
		t.Fatal("BearingsDefined() = false, want true")
	}
	closeTo(t, "initial bearing", result.InitialBearing().Degrees(), 90, 1e-12)
	closeTo(t, "final bearing", result.FinalBearing().Degrees(), 90, 1e-12)

	destination, final, err := model.Destination(
		from,
		result.InitialBearing(),
		result.Distance(),
	)
	if err != nil {
		t.Fatalf("Destination() error = %v", err)
	}
	closeTo(t, "destination longitude", destination.Longitude().Degrees(), 90, 1e-12)
	closeTo(t, "destination latitude", destination.Latitude().Degrees(), 0, 1e-12)
	closeTo(t, "destination final bearing", final.Degrees(), 90, 1e-12)
}

func TestWGS84MatchesGeographicLibAuthoritativeExamples(t *testing.T) {
	t.Parallel()

	for _, vector := range geotest.WGS84InverseVectors() {
		t.Run(vector.Name, func(t *testing.T) {
			from := coordinate(t, vector.FromLongitude, vector.FromLatitude, geo.WGS84())
			to := coordinate(t, vector.ToLongitude, vector.ToLatitude, geo.WGS84())
			result, err := geodesy.WGS84Ellipsoid().Inverse(from, to)
			if err != nil {
				t.Fatalf("Inverse() error = %v", err)
			}

			closeTo(t, "distance", result.Distance().Meters(), vector.DistanceMeters, 1e-6)
			closeTo(t, "initial bearing", result.InitialBearing().Degrees(), vector.InitialBearing, 1e-12)
			closeTo(t, "final bearing", result.FinalBearing().Degrees(), vector.FinalBearing, 5e-12)

			destination, final, err := geodesy.WGS84Ellipsoid().Destination(
				from,
				result.InitialBearing(),
				result.Distance(),
			)
			if err != nil {
				t.Fatalf("Destination() error = %v", err)
			}
			closeTo(t, "destination longitude", destination.Longitude().Degrees(), vector.ToLongitude, 1e-12)
			closeTo(t, "destination latitude", destination.Latitude().Degrees(), vector.ToLatitude, 1e-12)
			closeTo(t, "destination final bearing", final.Degrees(), vector.FinalBearing, 5e-12)
		})
	}
}

func TestWGS84MatchesGeographicLibEdgeDistanceVectors(t *testing.T) {
	t.Parallel()

	for _, vector := range geotest.WGS84DistanceVectors() {
		t.Run(vector.Name, func(t *testing.T) {
			from := coordinate(t, vector.FromLongitude, vector.FromLatitude, geo.WGS84())
			to := coordinate(t, vector.ToLongitude, vector.ToLatitude, geo.WGS84())
			result, err := geodesy.WGS84Ellipsoid().Inverse(from, to)
			if err != nil {
				t.Fatalf("Inverse() error = %v", err)
			}
			closeTo(
				t,
				"distance",
				result.Distance().Meters(),
				vector.DistanceMeters,
				vector.AbsoluteToleranceMeters,
			)
		})
	}
}

func TestGeodesyRejectsCRSMismatchAndMarksCoincidentBearingsUndefined(t *testing.T) {
	t.Parallel()

	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	_, err = geodesy.MeanEarthSphere().Inverse(
		coordinate(t, 0, 0, geo.WGS84()),
		coordinate(t, 0, 0, webMercator),
	)
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Inverse() error = %v, want ErrCRS", err)
	}

	point := coordinate(t, 24.9384, 60.1699, geo.WGS84())
	result, err := geodesy.WGS84Ellipsoid().Inverse(point, point)
	if err != nil {
		t.Fatalf("Inverse() error = %v", err)
	}
	if result.Distance().Meters() != 0 {
		t.Fatalf("Distance() = %v, want 0", result.Distance().Meters())
	}
	if result.BearingsDefined() {
		t.Fatal("BearingsDefined() = true for coincident points")
	}
}

func TestModelsExposeNamesAndValidateSphereRadius(t *testing.T) {
	t.Parallel()

	if geodesy.MeanEarthSphere().Name() != "IUGG mean Earth sphere" {
		t.Fatal("unexpected spherical model name")
	}
	if geodesy.WGS84Ellipsoid().Name() != "WGS84 ellipsoid" {
		t.Fatal("unexpected ellipsoidal model name")
	}
	if _, err := geodesy.NewSphere(geo.Distance{}); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("NewSphere(zero) error = %v, want ErrRange", err)
	}
	radius, err := geo.NewDistanceMeters(1_000)
	if err != nil {
		t.Fatalf("NewDistanceMeters() error = %v", err)
	}
	model, err := geodesy.NewSphere(radius)
	if err != nil {
		t.Fatalf("NewSphere() error = %v", err)
	}
	if model.Radius() != radius {
		t.Fatalf("Radius() = %v, want %v", model.Radius(), radius)
	}
}

func TestSphericalDegenerateAndAntipodalGeodesics(t *testing.T) {
	t.Parallel()

	model := geodesy.MeanEarthSphere()
	point := coordinate(t, 0, 0, geo.WGS84())
	coincident, err := model.Inverse(point, point)
	if err != nil {
		t.Fatalf("Inverse(coincident) error = %v", err)
	}
	if coincident.Distance().Meters() != 0 || coincident.BearingsDefined() {
		t.Fatal("coincident spherical result is not degenerate")
	}
	antipodal, err := model.Inverse(
		point,
		coordinate(t, 180, 0, geo.WGS84()),
	)
	if err != nil {
		t.Fatalf("Inverse(antipodal) error = %v", err)
	}
	if antipodal.BearingsDefined() {
		t.Fatal("antipodal spherical bearings are marked defined")
	}

	initial, err := geo.NewBearing(123)
	if err != nil {
		t.Fatalf("NewBearing() error = %v", err)
	}
	destination, final, err := model.Destination(point, initial, geo.Distance{})
	if err != nil {
		t.Fatalf("Destination(zero) error = %v", err)
	}
	if !destination.Equal(point) || final != initial {
		t.Fatal("zero-distance destination changed the input")
	}
}

func TestEllipsoidalExactAntipodalBearingsAreUndefined(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		fromLongitude float64
		fromLatitude  float64
		toLongitude   float64
		toLatitude    float64
	}{
		{
			name:        "equatorial antipodes",
			toLongitude: 180,
		},
		{
			name:          "opposite poles",
			fromLongitude: 25,
			fromLatitude:  90,
			toLongitude:   -155,
			toLatitude:    -90,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := geodesy.WGS84Ellipsoid().Inverse(
				coordinate(t, test.fromLongitude, test.fromLatitude, geo.WGS84()),
				coordinate(t, test.toLongitude, test.toLatitude, geo.WGS84()),
			)
			if err != nil {
				t.Fatalf("Inverse() error = %v", err)
			}
			if result.Distance().Meters() == 0 {
				t.Fatal("antipodal distance is zero")
			}
			if result.BearingsDefined() {
				t.Fatal("exact antipodal bearings are marked defined")
			}
		})
	}
}

func TestModelsRejectInvalidStateAndCRSForDirectProblems(t *testing.T) {
	t.Parallel()

	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	from := coordinate(t, 0, 0, webMercator)
	bearing, err := geo.NewBearing(0)
	if err != nil {
		t.Fatalf("NewBearing() error = %v", err)
	}
	distance, err := geo.NewDistanceMeters(1)
	if err != nil {
		t.Fatalf("NewDistanceMeters() error = %v", err)
	}
	for name, model := range map[string]geodesy.Model{
		"sphere":    geodesy.MeanEarthSphere(),
		"ellipsoid": geodesy.WGS84Ellipsoid(),
	} {
		if _, _, directErr := model.Destination(from, bearing, distance); !errors.Is(directErr, geo.ErrCRS) {
			t.Fatalf("%s Destination() error = %v, want ErrCRS", name, directErr)
		}
		if _, inverseErr := model.Inverse(from, from); !errors.Is(inverseErr, geo.ErrCRS) {
			t.Fatalf("%s Inverse() error = %v, want ErrCRS", name, inverseErr)
		}
	}

	var zero geodesy.Sphere
	valid := coordinate(t, 0, 0, geo.WGS84())
	if _, err := zero.Inverse(valid, valid); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("zero Sphere.Inverse() error = %v, want ErrRange", err)
	}
	if _, _, err := zero.Destination(valid, bearing, distance); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("zero Sphere.Destination() error = %v, want ErrRange", err)
	}
}

func coordinate(t *testing.T, longitude, latitude float64, crs geo.CRS) geo.Coordinate {
	t.Helper()

	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	value, err := geo.NewCoordinate(lon, lat, crs)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}

	return value
}

func closeTo(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()

	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s = %.15g, want %.15g +/- %g", name, got, want, tolerance)
	}
}

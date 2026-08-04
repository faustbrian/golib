package geo_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestBoundingBoxContainsAcrossAntimeridian(t *testing.T) {
	t.Parallel()

	bounds := mustBounds(t, 170, -10, -170, 10, geo.WGS84())
	if !bounds.CrossesAntimeridian() {
		t.Fatal("CrossesAntimeridian() = false, want true")
	}

	for _, test := range []struct {
		name string
		lon  float64
		lat  float64
		want bool
	}{
		{name: "east side", lon: 175, lat: 0, want: true},
		{name: "west side", lon: -175, lat: 0, want: true},
		{name: "positive dateline", lon: 180, lat: 10, want: true},
		{name: "negative dateline", lon: -180, lat: -10, want: true},
		{name: "outside longitude", lon: 0, lat: 0, want: false},
		{name: "outside latitude", lon: 175, lat: 11, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := bounds.Contains(mustCoordinate(t, test.lon, test.lat, geo.WGS84()))
			if err != nil {
				t.Fatalf("Contains() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Contains() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestBoundingBoxOverlapHandlesTheDatelineAsOneMeridian(t *testing.T) {
	t.Parallel()

	east := mustBounds(t, 170, -5, 180, 5, geo.WGS84())
	west := mustBounds(t, -180, -5, -170, 5, geo.WGS84())
	overlaps, err := east.Overlaps(west)
	if err != nil {
		t.Fatalf("Overlaps() error = %v", err)
	}
	if !overlaps {
		t.Fatal("Overlaps() = false at the antimeridian, want true")
	}
}

func TestBoundingBoxBoundariesAndOverlapOperandsAreIndependent(t *testing.T) {
	t.Parallel()

	pointBounds := mustBounds(t, 10, 5, 10, 5, geo.WGS84())
	if pointBounds.CrossesAntimeridian() {
		t.Fatal("zero-width bounds cross the antimeridian")
	}
	contains, err := pointBounds.Contains(mustCoordinate(t, 10, 5, geo.WGS84()))
	if err != nil || !contains {
		t.Fatalf("Contains(exact point) = %t, %v; want true, nil", contains, err)
	}
	contains, err = pointBounds.Contains(mustCoordinate(t, 11, 5, geo.WGS84()))
	if err != nil || contains {
		t.Fatalf("Contains(outside point bounds) = %t, %v; want false, nil", contains, err)
	}

	touchingNorth := mustBounds(t, -5, 0, 5, 5, geo.WGS84())
	touchingSouth := mustBounds(t, -5, 5, 5, 10, geo.WGS84())
	assertBoundsOverlap(t, touchingNorth, touchingSouth, true)
	assertBoundsOverlap(t, touchingSouth, touchingNorth, true)
	assertBoundsOverlap(t, touchingNorth, mustBounds(t, -5, 6, 5, 10, geo.WGS84()), false)
	assertBoundsOverlap(t, mustBounds(t, -5, -10, 5, -6, geo.WGS84()), touchingNorth, false)

	world := mustBounds(t, -180, -90, 180, 90, geo.WGS84())
	distant := mustBounds(t, 40, 0, 50, 1, geo.WGS84())
	assertBoundsOverlap(t, world, distant, true)
	assertBoundsOverlap(t, distant, world, true)
	if mustBounds(t, -180, 0, 0, 1, geo.WGS84()).CrossesAntimeridian() {
		t.Fatal("half-world western bounds cross the antimeridian")
	}
	if mustBounds(t, 0, 0, 180, 1, geo.WGS84()).CrossesAntimeridian() {
		t.Fatal("half-world eastern bounds cross the antimeridian")
	}
}

func TestBoundingBoxLongitudeOverlapAndDatelineEndpoints(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		left  geo.BoundingBox
		right geo.BoundingBox
		want  bool
	}{
		{name: "left edge enters right", left: mustBounds(t, -10, 0, 10, 1, geo.WGS84()), right: mustBounds(t, 5, 0, 20, 1, geo.WGS84()), want: true},
		{name: "right edge enters left", left: mustBounds(t, 5, 0, 20, 1, geo.WGS84()), right: mustBounds(t, -10, 0, 10, 1, geo.WGS84()), want: true},
		{name: "contained", left: mustBounds(t, -20, 0, 20, 1, geo.WGS84()), right: mustBounds(t, -5, 0, 5, 1, geo.WGS84()), want: true},
		{name: "disjoint", left: mustBounds(t, -20, 0, -10, 1, geo.WGS84()), right: mustBounds(t, 10, 0, 20, 1, geo.WGS84()), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertBoundsOverlap(t, test.left, test.right, test.want)
		})
	}

	easternDateline := mustBounds(t, 170, -1, 180, 1, geo.WGS84())
	for _, longitude := range []float64{170, 180, -180} {
		contains, err := easternDateline.Contains(mustCoordinate(t, longitude, 0, geo.WGS84()))
		if err != nil || !contains {
			t.Fatalf("Contains(%v) = %t, %v; want true, nil", longitude, contains, err)
		}
	}
	contains, err := easternDateline.Contains(mustCoordinate(t, 169, 0, geo.WGS84()))
	if err != nil || contains {
		t.Fatalf("Contains(169) = %t, %v; want false, nil", contains, err)
	}
}

func TestBoundingBoxRejectsInvertedLatitudeAndCRSMismatch(t *testing.T) {
	t.Parallel()

	west := mustLongitude(t, -10)
	east := mustLongitude(t, 10)
	south := mustLatitude(t, 5)
	north := mustLatitude(t, -5)
	_, err := geo.NewBoundingBox(west, south, east, north, geo.WGS84())
	if !errors.Is(err, geo.ErrRange) {
		t.Fatalf("NewBoundingBox() error = %v, want ErrRange", err)
	}

	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	bounds := mustBounds(t, -10, -5, 10, 5, geo.WGS84())
	_, err = bounds.Contains(mustCoordinate(t, 0, 0, webMercator))
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Contains() error = %v, want ErrCRS", err)
	}
	_, err = bounds.Overlaps(mustBounds(t, -10, -5, 10, 5, webMercator))
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Overlaps() error = %v, want ErrCRS", err)
	}
}

func TestWholeWorldBoundingBoxContainsEveryLongitude(t *testing.T) {
	t.Parallel()

	bounds := mustBounds(t, -180, -90, 180, 90, geo.WGS84())
	for _, longitude := range []float64{-180, -179, 0, 179, 180} {
		contains, err := bounds.Contains(mustCoordinate(t, longitude, 0, geo.WGS84()))
		if err != nil {
			t.Fatalf("Contains(%v) error = %v", longitude, err)
		}
		if !contains {
			t.Fatalf("Contains(%v) = false, want true", longitude)
		}
	}
}

func mustBounds(t *testing.T, west, south, east, north float64, crs geo.CRS) geo.BoundingBox {
	t.Helper()

	bounds, err := geo.NewBoundingBox(
		mustLongitude(t, west),
		mustLatitude(t, south),
		mustLongitude(t, east),
		mustLatitude(t, north),
		crs,
	)
	if err != nil {
		t.Fatalf("NewBoundingBox() error = %v", err)
	}

	return bounds
}

func assertBoundsOverlap(t *testing.T, left, right geo.BoundingBox, want bool) {
	t.Helper()

	overlaps, err := left.Overlaps(right)
	if err != nil {
		t.Fatalf("Overlaps() error = %v", err)
	}
	if overlaps != want {
		t.Fatalf("Overlaps() = %t, want %t", overlaps, want)
	}
}

func mustCoordinate(t *testing.T, longitude, latitude float64, crs geo.CRS) geo.Coordinate {
	t.Helper()

	coordinate, err := geo.NewCoordinate(
		mustLongitude(t, longitude),
		mustLatitude(t, latitude),
		crs,
	)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}

	return coordinate
}

func mustLongitude(t *testing.T, degrees float64) geo.Longitude {
	t.Helper()

	longitude, err := geo.NewLongitude(degrees)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}

	return longitude
}

func mustLatitude(t *testing.T, degrees float64) geo.Latitude {
	t.Helper()

	latitude, err := geo.NewLatitude(degrees)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}

	return latitude
}

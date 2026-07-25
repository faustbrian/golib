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

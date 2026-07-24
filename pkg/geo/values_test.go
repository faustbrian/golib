package geo_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestCoordinateUsesExplicitLongitudeLatitudeOrderAndCRS(t *testing.T) {
	t.Parallel()

	lon, err := geo.NewLongitude(24.9384)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(60.1699)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}

	if got := coordinate.Longitude().Degrees(); got != 24.9384 {
		t.Fatalf("Longitude() = %v, want 24.9384", got)
	}
	if got := coordinate.Latitude().Degrees(); got != 60.1699 {
		t.Fatalf("Latitude() = %v, want 60.1699", got)
	}
	if !coordinate.CRS().Equal(geo.WGS84()) {
		t.Fatalf("CRS() = %v, want WGS84", coordinate.CRS())
	}

	want := `{"longitude":24.9384,"latitude":60.1699,"crs":{"srid":4326,"name":"EPSG:4326"}}`
	encoded, err := json.Marshal(coordinate)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got := string(encoded); got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
}

func TestScalarConstructorsRejectNonFiniteAndOutOfRangeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "longitude below range", call: func() error { _, err := geo.NewLongitude(-180.0001); return err }},
		{name: "longitude above range", call: func() error { _, err := geo.NewLongitude(180.0001); return err }},
		{name: "latitude below range", call: func() error { _, err := geo.NewLatitude(-90.0001); return err }},
		{name: "latitude above range", call: func() error { _, err := geo.NewLatitude(90.0001); return err }},
		{name: "longitude NaN", call: func() error { _, err := geo.NewLongitude(math.NaN()); return err }},
		{name: "latitude infinity", call: func() error { _, err := geo.NewLatitude(math.Inf(1)); return err }},
		{name: "altitude infinity", call: func() error { _, err := geo.NewAltitudeMeters(math.Inf(-1)); return err }},
		{name: "bearing negative", call: func() error { _, err := geo.NewBearing(-0.001); return err }},
		{name: "bearing full turn", call: func() error { _, err := geo.NewBearing(360); return err }},
		{name: "distance negative", call: func() error { _, err := geo.NewDistanceMeters(-0.001); return err }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.call()
			if err == nil {
				t.Fatal("constructor accepted invalid value")
			}
			if !errors.Is(err, geo.ErrRange) {
				t.Fatalf("error = %v, want ErrRange", err)
			}
			var rangeError *geo.RangeError
			if !errors.As(err, &rangeError) {
				t.Fatalf("error type = %T, want *RangeError", err)
			}
		})
	}
}

func TestScalarValuesNormalizeSignedZeroAndHaveStableEquality(t *testing.T) {
	t.Parallel()

	lon, err := geo.NewLongitude(math.Copysign(0, -1))
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(0)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}

	if math.Signbit(lon.Degrees()) {
		t.Fatal("longitude retained negative zero")
	}
	if !lon.Equal(geo.Longitude{}) || !lat.Equal(geo.Latitude{}) {
		t.Fatal("normalized zero does not have stable equality")
	}
}

func TestCRSRequiresPositiveSRIDAndAName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		srid    int32
		crsName string
	}{
		{name: "zero SRID", srid: 0, crsName: "unknown"},
		{name: "negative SRID", srid: -1, crsName: "unknown"},
		{name: "empty name", srid: 4326, crsName: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := geo.NewCRS(test.srid, test.crsName)
			if !errors.Is(err, geo.ErrCRS) {
				t.Fatalf("NewCRS() error = %v, want ErrCRS", err)
			}
			var crsError *geo.CRSError
			if !errors.As(err, &crsError) {
				t.Fatalf("error type = %T, want *CRSError", err)
			}
		})
	}
}

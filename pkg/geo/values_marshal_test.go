package geo_test

import (
	"encoding"
	"encoding/json"
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestScalarJSONAndTextRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		text  string
		json  string
		new   func() any
	}{
		{"longitude", mustScalarLongitude(t, 24.9384), "24.9384", "24.9384", func() any { return new(geo.Longitude) }},
		{"latitude", mustScalarLatitude(t, -60.1699), "-60.1699", "-60.1699", func() any { return new(geo.Latitude) }},
		{"altitude", mustAltitude(t, 12.5), "12.5", "12.5", func() any { return new(geo.Altitude) }},
		{"bearing", mustBearingValue(t, 359.5), "359.5", "359.5", func() any { return new(geo.Bearing) }},
		{"distance", mustDistanceValue(t, 1234.5), "1234.5", "1234.5", func() any { return new(geo.Distance) }},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			marshaler, ok := test.value.(encoding.TextMarshaler)
			if !ok {
				t.Fatalf("%T does not implement encoding.TextMarshaler", test.value)
			}
			text, err := marshaler.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			if string(text) != test.text {
				t.Fatalf("MarshalText() = %q, want %q", text, test.text)
			}

			fromText := test.new()
			if err := fromText.(encoding.TextUnmarshaler).UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText() error = %v", err)
			}
			encodedJSON, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(encodedJSON) != test.json {
				t.Fatalf("json.Marshal() = %s, want %s", encodedJSON, test.json)
			}
			fromJSON := test.new()
			if err := json.Unmarshal(encodedJSON, fromJSON); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if fromText != nil && fromJSON != nil &&
				string(mustJSON(t, fromText)) != string(mustJSON(t, fromJSON)) {
				t.Fatal("text and JSON decoding produced different values")
			}
		})
	}
}

func TestCRSAndCoordinateJSONRoundTrip(t *testing.T) {
	t.Parallel()

	coordinate := mustCoordinateValue(t, 24.9384, 60.1699, geo.WGS84())
	encoded, err := json.Marshal(coordinate)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"longitude":24.9384,"latitude":60.1699,"crs":{"srid":4326,"name":"EPSG:4326"}}`
	if string(encoded) != want {
		t.Fatalf("json.Marshal() = %s, want %s", encoded, want)
	}
	var decoded geo.Coordinate
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !decoded.Equal(coordinate) {
		t.Fatal("coordinate JSON round trip changed value")
	}

	text, err := coordinate.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if string(text) != "24.9384,60.1699@4326:EPSG:4326" {
		t.Fatalf("MarshalText() = %q", text)
	}
	var decodedText geo.Coordinate
	if err := decodedText.UnmarshalText(text); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}
	if !decodedText.Equal(coordinate) {
		t.Fatal("coordinate text round trip changed value")
	}
}

func TestValueDecodingRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	var longitude geo.Longitude
	if err := json.Unmarshal([]byte("181"), &longitude); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("longitude error = %v, want ErrRange", err)
	}
	var coordinate geo.Coordinate
	if err := coordinate.UnmarshalText([]byte("60,24")); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("coordinate error = %v, want ErrEncoding", err)
	}
	var crs geo.CRS
	if err := json.Unmarshal([]byte(`{"srid":0,"name":""}`), &crs); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("CRS error = %v, want ErrCRS", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}

func mustScalarLongitude(t *testing.T, value float64) geo.Longitude {
	t.Helper()
	result, err := geo.NewLongitude(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustScalarLatitude(t *testing.T, value float64) geo.Latitude {
	t.Helper()
	result, err := geo.NewLatitude(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustAltitude(t *testing.T, value float64) geo.Altitude {
	t.Helper()
	result, err := geo.NewAltitudeMeters(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustBearingValue(t *testing.T, value float64) geo.Bearing {
	t.Helper()
	result, err := geo.NewBearing(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustDistanceValue(t *testing.T, value float64) geo.Distance {
	t.Helper()
	result, err := geo.NewDistanceMeters(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func mustCoordinateValue(t *testing.T, longitude, latitude float64, crs geo.CRS) geo.Coordinate {
	t.Helper()
	coordinate, err := geo.NewCoordinate(
		mustScalarLongitude(t, longitude),
		mustScalarLatitude(t, latitude),
		crs,
	)
	if err != nil {
		t.Fatal(err)
	}
	return coordinate
}

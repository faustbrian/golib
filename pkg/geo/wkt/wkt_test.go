package wkt_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func TestPointWKTAndEWKTPreserveCoordinateOrderAndSRID(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 24.9384, 60.1699)
	encoded, err := wkt.Marshal(point)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(encoded), "POINT (24.9384 60.1699)"; got != want {
		t.Fatalf("Marshal() = %q, want %q", got, want)
	}
	ewkt, err := wkt.MarshalEWKT(point)
	if err != nil {
		t.Fatalf("MarshalEWKT() error = %v", err)
	}
	if got, want := string(ewkt), "SRID=4326;POINT (24.9384 60.1699)"; got != want {
		t.Fatalf("MarshalEWKT() = %q, want %q", got, want)
	}

	decoded, err := wkt.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, point) {
		t.Fatal("WKT round trip changed point")
	}
	decodedEWKT, err := wkt.UnmarshalEWKT(ewkt, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalEWKT() error = %v", err)
	}
	if !geo.EqualGeometry(decodedEWKT, point) {
		t.Fatal("EWKT round trip changed point")
	}
}

func TestCollectionWithPolygonAndMultiPointRoundTrips(t *testing.T) {
	t.Parallel()

	polygon, err := geo.NewPolygon(
		coords(t, [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 0}}),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	multi, err := geo.NewMultiPoint(
		coords(t, [][2]float64{{1, 2}, {3, 4}}),
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewMultiPoint() error = %v", err)
	}
	collection, err := geo.NewGeometryCollection(
		[]geo.Geometry{polygon, multi},
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}

	encoded, err := wkt.Marshal(collection)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	decoded, err := wkt.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, collection) {
		t.Fatalf("round trip changed collection: %s", encoded)
	}
}

func TestEveryGeometryFamilyAndEmptyCollectionRoundTrips(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"POINT (24.9384 60.1699)",
		"LINESTRING (0 0, 1 1, 2 0)",
		"POLYGON ((0 0, 10 0, 10 10, 0 10, 0 0), " +
			"(2 2, 2 3, 3 3, 3 2, 2 2))",
		"MULTIPOINT ((1 2), (3 4))",
		"MULTILINESTRING ((0 0, 1 1), (2 2, 3 3))",
		"MULTIPOLYGON (((0 0, 2 0, 2 2, 0 0)), " +
			"((10 10, 12 10, 12 12, 10 10)))",
		"GEOMETRYCOLLECTION (POINT (1 2), " +
			"MULTILINESTRING ((0 0, 1 1)))",
		"MULTIPOINT EMPTY",
		"MULTILINESTRING EMPTY",
		"MULTIPOLYGON EMPTY",
		"GEOMETRYCOLLECTION EMPTY",
	}
	for _, input := range inputs {
		input := input
		t.Run(input[:min(len(input), 24)], func(t *testing.T) {
			t.Parallel()

			geometry, err := wkt.Unmarshal(
				[]byte(input),
				geo.WGS84(),
				geo.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("Unmarshal(%q) error = %v", input, err)
			}
			canonical, err := wkt.Marshal(geometry)
			if err != nil {
				t.Fatalf("Marshal(%q) error = %v", input, err)
			}
			roundTrip, err := wkt.Unmarshal(
				canonical,
				geo.WGS84(),
				geo.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("Unmarshal(canonical %q) error = %v", canonical, err)
			}
			if !geo.EqualGeometry(roundTrip, geometry) {
				t.Fatalf("WKT round trip changed %s", geometry.Type())
			}
			ewkt, err := wkt.MarshalEWKT(geometry)
			if err != nil {
				t.Fatalf("MarshalEWKT(%q) error = %v", input, err)
			}
			ewktRoundTrip, err := wkt.UnmarshalEWKT(ewkt, geo.DefaultLimits())
			if err != nil {
				t.Fatalf("UnmarshalEWKT(%q) error = %v", ewkt, err)
			}
			if !geo.EqualGeometry(ewktRoundTrip, geometry) {
				t.Fatalf("EWKT round trip changed %s", geometry.Type())
			}
		})
	}
}

func TestWKTDecoderRejectsLimitsAndTrailingInput(t *testing.T) {
	t.Parallel()

	input := []byte("MULTIPOINT ((0 0), (1 1))")
	_, err := wkt.Unmarshal(input, geo.WGS84(), geo.Limits{MaxPoints: 1})
	if !errors.Is(err, geo.ErrEncoding) || !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("point limit error = %v, want encoding and topology errors", err)
	}
	_, err = wkt.Unmarshal(input, geo.WGS84(), geo.Limits{MaxEncodedBytes: 4})
	if !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("byte limit error = %v, want ErrEncoding", err)
	}
	_, err = wkt.Unmarshal([]byte("POINT (0 0) garbage"), geo.WGS84(), geo.DefaultLimits())
	if !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("trailing input error = %v, want ErrEncoding", err)
	}
}

func TestEWKTRequiresPositiveSRID(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"POINT (0 0)", "SRID=0;POINT (0 0)", "SRID=x;POINT (0 0)"} {
		_, err := wkt.UnmarshalEWKT([]byte(input), geo.DefaultLimits())
		if !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("UnmarshalEWKT(%q) error = %v, want ErrEncoding", input, err)
		}
	}
}

func TestWKTRejectsMalformedDimensionsTopologyAndResourceExhaustion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		limits geo.Limits
	}{
		{"missing type", "", geo.DefaultLimits()},
		{"unsupported type", "CIRCLE (0 0)", geo.DefaultLimits()},
		{"unsupported dimension", "POINT Z (0 0 1)", geo.DefaultLimits()},
		{"unsupported point empty", "POINT EMPTY", geo.DefaultLimits()},
		{"unsupported empty type", "CIRCLE EMPTY", geo.DefaultLimits()},
		{"point missing open", "POINT 0 0)", geo.DefaultLimits()},
		{"point missing number", "POINT ()", geo.DefaultLimits()},
		{"point missing space", "POINT (0)", geo.DefaultLimits()},
		{"point bad latitude", "POINT (0 bad)", geo.DefaultLimits()},
		{"point longitude range", "POINT (181 0)", geo.DefaultLimits()},
		{"point latitude range", "POINT (0 91)", geo.DefaultLimits()},
		{"point missing close", "POINT (0 0", geo.DefaultLimits()},
		{"line missing open", "LINESTRING 0 0)", geo.DefaultLimits()},
		{"line missing close", "LINESTRING (0 0, 1 1", geo.DefaultLimits()},
		{"line invalid second point", "LINESTRING (0 0, bad 1)", geo.DefaultLimits()},
		{"line short", "LINESTRING (0 0)", geo.DefaultLimits()},
		{"line point limit", "LINESTRING (0 0, 1 1)", geo.Limits{MaxPoints: 1}},
		{"polygon missing open", "POLYGON (0 0, 1 0, 0 0)", geo.DefaultLimits()},
		{"polygon body missing open", "POLYGON 0 0", geo.DefaultLimits()},
		{"polygon ring limit", "POLYGON ((0 0, 1 0, 0 0))", geo.Limits{MaxRings: -1}},
		{"polygon missing close", "POLYGON ((0 0, 1 0, 0 0)", geo.DefaultLimits()},
		{"multi point plain form bad", "MULTIPOINT (0 0, bad 1)", geo.DefaultLimits()},
		{"multi point empty body", "MULTIPOINT (", geo.DefaultLimits()},
		{"multi point missing close", "MULTIPOINT ((0 0)", geo.DefaultLimits()},
		{"multi line geometry limit", "MULTILINESTRING ((0 0, 1 1))", geo.Limits{MaxGeometries: -1}},
		{"multi line missing open", "MULTILINESTRING 0", geo.DefaultLimits()},
		{"multi line invalid point", "MULTILINESTRING ((0 0, bad 1))", geo.DefaultLimits()},
		{"multi line invalid line", "MULTILINESTRING ((0 0))", geo.DefaultLimits()},
		{"multi line missing close", "MULTILINESTRING ((0 0, 1 1)", geo.DefaultLimits()},
		{"multi polygon geometry limit", "MULTIPOLYGON (((0 0, 1 0, 0 0)))", geo.Limits{MaxGeometries: -1}},
		{"multi polygon missing open", "MULTIPOLYGON 0", geo.DefaultLimits()},
		{"multi polygon invalid ring", "MULTIPOLYGON (bad)", geo.DefaultLimits()},
		{"multi polygon invalid polygon", "MULTIPOLYGON (((0 0, 1 1, 2 2, 0 0)))", geo.DefaultLimits()},
		{"multi polygon missing close", "MULTIPOLYGON (((0 0, 1 0, 1 1, 0 0))", geo.DefaultLimits()},
		{"collection geometry limit", "GEOMETRYCOLLECTION (POINT (0 0))", geo.Limits{MaxGeometries: -1}},
		{"collection missing open", "GEOMETRYCOLLECTION POINT (0 0)", geo.DefaultLimits()},
		{"collection missing close", "GEOMETRYCOLLECTION (POINT (0 0)", geo.DefaultLimits()},
		{"collection depth", "GEOMETRYCOLLECTION (GEOMETRYCOLLECTION (POINT (0 0)))", geo.Limits{MaxCollectionDepth: 1}},
		{"empty keyword suffix", "MULTIPOINT EMPTYMORE", geo.DefaultLimits()},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := wkt.Unmarshal(
				[]byte(test.input),
				geo.WGS84(),
				test.limits,
			); !errors.Is(err, geo.ErrEncoding) {
				t.Fatalf("Unmarshal(%q) error = %v, want ErrEncoding", test.input, err)
			}
		})
	}
}

func TestWKTRejectsInvalidMetadataAndEWKTPrefixes(t *testing.T) {
	t.Parallel()

	if _, err := wkt.Marshal(nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Marshal(nil) error = %v, want ErrTopology", err)
	}
	if _, err := wkt.MarshalEWKT(nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("MarshalEWKT(nil) error = %v, want ErrTopology", err)
	}
	if _, err := wkt.Unmarshal(
		[]byte("POINT (0 0)"),
		geo.CRS{},
		geo.DefaultLimits(),
	); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Unmarshal(zero CRS) error = %v, want ErrCRS", err)
	}
	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 4
	if _, err := wkt.UnmarshalEWKT(
		[]byte("SRID=4326;POINT (0 0)"),
		limits,
	); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalEWKT(byte limit) error = %v, want ErrEncoding", err)
	}
	for _, input := range []string{
		"nonsense;POINT (0 0)",
		"S=1;POINT (0 0)",
		"SRID=2147483648;POINT (0 0)",
	} {
		if _, err := wkt.UnmarshalEWKT(
			[]byte(input),
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("UnmarshalEWKT(%q) error = %v, want ErrEncoding", input, err)
		}
	}
	geometry, err := wkt.UnmarshalEWKT(
		[]byte("SRID=3857;POINT (0 0)"),
		geo.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("UnmarshalEWKT(EPSG:3857) error = %v", err)
	}
	if geometry.CRS().SRID() != 3857 {
		t.Fatalf("decoded SRID = %d, want 3857", geometry.CRS().SRID())
	}
}

func mustPoint(t *testing.T, longitude, latitude float64) geo.Point {
	t.Helper()

	point, err := geo.NewPoint(coords(t, [][2]float64{{longitude, latitude}})[0])
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	return point
}

func coords(t *testing.T, values [][2]float64) []geo.Coordinate {
	t.Helper()

	result := make([]geo.Coordinate, len(values))
	for index, value := range values {
		lon, err := geo.NewLongitude(value[0])
		if err != nil {
			t.Fatalf("NewLongitude() error = %v", err)
		}
		lat, err := geo.NewLatitude(value[1])
		if err != nil {
			t.Fatalf("NewLatitude() error = %v", err)
		}
		result[index], err = geo.NewCoordinate(lon, lat, geo.WGS84())
		if err != nil {
			t.Fatalf("NewCoordinate() error = %v", err)
		}
	}
	return result
}

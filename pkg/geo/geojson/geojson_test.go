package geojson_test

import (
	"encoding/json"
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geojson"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func TestPointUsesLongitudeLatitudeOrder(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 24.9384, 60.1699)
	encoded, err := geojson.Marshal(point)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	want := `{"type":"Point","coordinates":[24.9384,60.1699]}`
	if string(encoded) != want {
		t.Fatalf("Marshal() = %s, want %s", encoded, want)
	}

	decoded, err := geojson.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, point) {
		t.Fatalf("decoded geometry = %#v, want point", decoded)
	}
}

func TestGeometryCollectionRoundTripsWithPolygonHole(t *testing.T) {
	t.Parallel()

	polygon, err := geo.NewPolygon(
		coordinates(t, [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}}),
		[][]geo.Coordinate{coordinates(t, [][2]float64{{2, 2}, {3, 2}, {2, 3}, {2, 2}})},
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	collection, err := geo.NewGeometryCollection(
		[]geo.Geometry{mustPoint(t, 1, 1), polygon},
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}

	encoded, err := geojson.Marshal(collection)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	decoded, err := geojson.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, collection) {
		t.Fatalf("round trip changed geometry:\n%s", encoded)
	}
}

func TestEveryGeometryFamilyRoundTrips(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"POINT (1 2)",
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
		geometry, err := wkt.Unmarshal(
			[]byte(input),
			geo.WGS84(),
			geo.DefaultLimits(),
		)
		if err != nil {
			t.Fatalf("wkt.Unmarshal(%q) error = %v", input, err)
		}
		encoded, err := geojson.Marshal(geometry)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", geometry.Type(), err)
		}
		decoded, err := geojson.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
		if err != nil {
			t.Fatalf("Unmarshal(%s) error = %v: %s", geometry.Type(), err, encoded)
		}
		if !geo.EqualGeometry(decoded, geometry) {
			t.Fatalf("GeoJSON round trip changed %s", geometry.Type())
		}
	}
}

func TestDecodeIsBoundedAndCRSExplicit(t *testing.T) {
	t.Parallel()

	input := []byte(`{"type":"MultiPoint","coordinates":[[0,0],[1,1]]}`)
	_, err := geojson.Unmarshal(input, geo.WGS84(), geo.Limits{MaxPoints: 1})
	if !errors.Is(err, geo.ErrEncoding) || !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("point limit error = %v, want encoding and topology errors", err)
	}
	_, err = geojson.Unmarshal(input, geo.WGS84(), geo.Limits{MaxEncodedBytes: 8})
	if !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("byte limit error = %v, want ErrEncoding", err)
	}

	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	_, err = geojson.Unmarshal(input, webMercator, geo.DefaultLimits())
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("CRS error = %v, want ErrCRS", err)
	}
}

func TestFeaturePreservesRawPropertiesAndID(t *testing.T) {
	t.Parallel()

	properties := map[string]json.RawMessage{
		"name": json.RawMessage(`"Helsinki"`),
	}
	feature, err := geojson.NewFeature(
		mustPoint(t, 24.9384, 60.1699),
		properties,
		json.RawMessage(`"capital"`),
	)
	if err != nil {
		t.Fatalf("NewFeature() error = %v", err)
	}
	properties["name"] = json.RawMessage(`"changed"`)

	encoded, err := geojson.MarshalFeature(feature)
	if err != nil {
		t.Fatalf("MarshalFeature() error = %v", err)
	}
	want := `{"type":"Feature","id":"capital","geometry":{"type":"Point","coordinates":[24.9384,60.1699]},"properties":{"name":"Helsinki"}}`
	if string(encoded) != want {
		t.Fatalf("MarshalFeature() = %s, want %s", encoded, want)
	}
	decoded, err := geojson.UnmarshalFeature(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalFeature() error = %v", err)
	}
	if !geo.EqualGeometry(decoded.Geometry(), feature.Geometry()) {
		t.Fatal("feature geometry changed")
	}
	decodedProperties := decoded.Properties()
	if string(decodedProperties["name"]) != `"Helsinki"` ||
		string(decoded.ID()) != `"capital"` {
		t.Fatal("feature property or ID changed")
	}
	decodedProperties["name"] = json.RawMessage(`"changed"`)
	if string(decoded.Properties()["name"]) != `"Helsinki"` {
		t.Fatal("Properties() exposed mutable feature state")
	}
}

func TestNullFeatureRoundTrips(t *testing.T) {
	t.Parallel()

	feature, err := geojson.NewFeature(nil, nil, nil)
	if err != nil {
		t.Fatalf("NewFeature() error = %v", err)
	}
	encoded, err := geojson.MarshalFeature(feature)
	if err != nil {
		t.Fatalf("MarshalFeature() error = %v", err)
	}
	decoded, err := geojson.UnmarshalFeature(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalFeature() error = %v", err)
	}
	if decoded.Geometry() != nil || decoded.Properties() != nil || decoded.ID() != nil {
		t.Fatal("null feature did not preserve null members")
	}
}

func TestGeometryDecoderRejectsMalformedCoordinatesAndTopology(t *testing.T) {
	t.Parallel()

	inputs := []string{
		`[]`,
		`{"type":"Unknown","coordinates":[]}`,
		`{"type":"Point"}`,
		`{"type":"Point","coordinates":"bad"}`,
		`{"type":"Point","coordinates":[1,2,3]}`,
		`{"type":"Point","coordinates":[181,0]}`,
		`{"type":"Point","coordinates":[0,91]}`,
		`{"type":"LineString","coordinates":"bad"}`,
		`{"type":"LineString","coordinates":[[0,0],[1]]}`,
		`{"type":"LineString","coordinates":[[0,0]]}`,
		`{"type":"Polygon","coordinates":"bad"}`,
		`{"type":"Polygon","coordinates":[]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1]]]}`,
		`{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}`,
		`{"type":"MultiPoint","coordinates":"bad"}`,
		`{"type":"MultiPoint","coordinates":[[0,0],[181,0]]}`,
		`{"type":"MultiLineString","coordinates":"bad"}`,
		`{"type":"MultiLineString","coordinates":[[[0,0],[1]]]}`,
		`{"type":"MultiLineString","coordinates":[[[0,0]]]}`,
		`{"type":"MultiPolygon","coordinates":"bad"}`,
		`{"type":"MultiPolygon","coordinates":[[]]}`,
		`{"type":"MultiPolygon","coordinates":[[[[0,0],[1]]]]}`,
		`{"type":"MultiPolygon","coordinates":[[[[0,0],[1,1],[2,2],[0,0]]]]}`,
		`{"type":"GeometryCollection","geometries":[{"type":"Point"}]}`,
	}
	for _, input := range inputs {
		if _, err := geojson.Unmarshal(
			[]byte(input),
			geo.WGS84(),
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("Unmarshal(%s) error = %v, want ErrEncoding", input, err)
		}
	}

	deep := []byte(`{"type":"GeometryCollection","geometries":[` +
		`{"type":"GeometryCollection","geometries":[]}]}`)
	limits := geo.DefaultLimits()
	limits.MaxCollectionDepth = 1
	if _, err := geojson.Unmarshal(deep, geo.WGS84(), limits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Unmarshal(depth) error = %v, want ErrTopology", err)
	}
}

func TestFeatureRejectsInvalidShapeIDPropertiesAndLimits(t *testing.T) {
	t.Parallel()

	invalidFeatures := []string{
		`[]`,
		`{"type":"Point","geometry":null,"properties":null}`,
		`{"type":"Feature","properties":null}`,
		`{"type":"Feature","geometry":{"type":"Point"},"properties":null}`,
		`{"type":"Feature","geometry":null}`,
		`{"type":"Feature","geometry":null,"properties":[]}`,
		`{"type":"Feature","id":true,"geometry":null,"properties":null}`,
	}
	for _, input := range invalidFeatures {
		if _, err := geojson.UnmarshalFeature(
			[]byte(input),
			geo.WGS84(),
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("UnmarshalFeature(%s) error = %v, want ErrEncoding", input, err)
		}
	}

	if _, err := geojson.NewFeature(
		nil,
		nil,
		json.RawMessage(`{`),
	); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("NewFeature(invalid ID) error = %v, want ErrEncoding", err)
	}
	if _, err := geojson.NewFeature(
		nil,
		map[string]json.RawMessage{"bad": json.RawMessage(`{`)},
		nil,
	); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("NewFeature(invalid property) error = %v, want ErrEncoding", err)
	}
	var nilPoint *geo.Point
	if _, err := geojson.NewFeature(nilPoint, nil, nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("NewFeature(nil point) error = %v, want ErrTopology", err)
	}

	input := []byte(`{"type":"Feature","geometry":null,"properties":null}`)
	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 4
	if _, err := geojson.UnmarshalFeature(input, geo.WGS84(), limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalFeature(byte limit) error = %v", err)
	}
	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := geojson.UnmarshalFeature(input, webMercator, geo.DefaultLimits()); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("UnmarshalFeature(CRS) error = %v, want ErrCRS", err)
	}
}

func TestMarshalRejectsNilAndNonWGS84Geometry(t *testing.T) {
	t.Parallel()

	if _, err := geojson.Marshal(nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Marshal(nil) error = %v, want ErrTopology", err)
	}
	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	lon, _ := geo.NewLongitude(0)
	lat, _ := geo.NewLatitude(0)
	coordinate, err := geo.NewCoordinate(lon, lat, webMercator)
	if err != nil {
		t.Fatal(err)
	}
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := geojson.Marshal(point); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Marshal(EPSG:3857) error = %v, want ErrCRS", err)
	}
	if _, err := geojson.Unmarshal(
		[]byte(`{`),
		geo.WGS84(),
		geo.DefaultLimits(),
	); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Unmarshal(malformed JSON) error = %v", err)
	}
}

func mustPoint(t *testing.T, longitude, latitude float64) geo.Point {
	t.Helper()

	point, err := geo.NewPoint(mustCoordinate(t, longitude, latitude))
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	return point
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

func coordinates(t *testing.T, values [][2]float64) []geo.Coordinate {
	t.Helper()

	result := make([]geo.Coordinate, len(values))
	for index, value := range values {
		result[index] = mustCoordinate(t, value[0], value[1])
	}
	return result
}

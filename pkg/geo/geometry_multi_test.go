package geo_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestPointAndMultiPointAreImmutableAndExplicitlyTyped(t *testing.T) {
	t.Parallel()

	first := mustCoordinate(t, 24.9384, 60.1699, geo.WGS84())
	point, err := geo.NewPoint(first)
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	if point.Type() != geo.TypePoint || !point.Coordinate().Equal(first) {
		t.Fatalf("point = %#v, want typed coordinate", point)
	}

	input := []geo.Coordinate{first, mustCoordinate(t, 25, 61, geo.WGS84())}
	multi, err := geo.NewMultiPoint(input, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiPoint() error = %v", err)
	}
	input[0] = mustCoordinate(t, 0, 0, geo.WGS84())
	got, ok := multi.At(0)
	if !ok || !got.Equal(first) {
		t.Fatalf("At(0) = %v, %t; want original coordinate", got, ok)
	}
	if !geo.EqualGeometry(multi, multi) {
		t.Fatal("EqualGeometry(multi, multi) = false")
	}
}

func TestEmptyMultiGeometryRetainsExplicitCRS(t *testing.T) {
	t.Parallel()

	multiPoint, err := geo.NewMultiPoint(nil, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiPoint() error = %v", err)
	}
	multiLine, err := geo.NewMultiLineString(nil, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiLineString() error = %v", err)
	}
	multiPolygon, err := geo.NewMultiPolygon(nil, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiPolygon() error = %v", err)
	}
	collection, err := geo.NewGeometryCollection(nil, geo.WGS84())
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}

	for _, geometry := range []geo.Geometry{multiPoint, multiLine, multiPolygon, collection} {
		if !geometry.CRS().Equal(geo.WGS84()) {
			t.Fatalf("%s CRS = %v, want WGS84", geometry.Type(), geometry.CRS())
		}
	}
}

func TestGeometryCollectionEnforcesCRSPointAndDepthLimits(t *testing.T) {
	t.Parallel()

	point, err := geo.NewPoint(mustCoordinate(t, 0, 0, geo.WGS84()))
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	otherPoint, err := geo.NewPoint(mustCoordinate(t, 0, 0, webMercator))
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}

	_, err = geo.NewGeometryCollection([]geo.Geometry{point, otherPoint}, geo.WGS84())
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("mixed CRS error = %v, want ErrCRS", err)
	}
	_, err = geo.NewGeometryCollectionWithLimits(
		[]geo.Geometry{point, point},
		geo.WGS84(),
		geo.Limits{MaxPoints: 1},
	)
	if !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("point limit error = %v, want ErrTopology", err)
	}

	inner, err := geo.NewGeometryCollection([]geo.Geometry{point}, geo.WGS84())
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}
	_, err = geo.NewGeometryCollectionWithLimits(
		[]geo.Geometry{inner},
		geo.WGS84(),
		geo.Limits{MaxCollectionDepth: 1},
	)
	if !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("depth limit error = %v, want ErrTopology", err)
	}
}

func TestMultiLineAndPolygonRejectMismatchedDeclaredCRS(t *testing.T) {
	t.Parallel()

	line, err := geo.NewLineString([]geo.Coordinate{
		mustCoordinate(t, 0, 0, geo.WGS84()),
		mustCoordinate(t, 1, 1, geo.WGS84()),
	})
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	polygon, err := geo.NewPolygon(
		ring(t, [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 0}}),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	other, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}

	if _, err = geo.NewMultiLineString([]geo.LineString{line}, other); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("NewMultiLineString() error = %v, want ErrCRS", err)
	}
	if _, err = geo.NewMultiPolygon([]geo.Polygon{polygon}, other); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("NewMultiPolygon() error = %v, want ErrCRS", err)
	}
}

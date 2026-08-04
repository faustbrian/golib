package wkb

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestGeometryTypeAndSRIDBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		rawType uint32
		problem string
	}{
		{rawType: flagZ | typePoint, problem: "only two-dimensional WKB is supported"},
		{rawType: flagM | typePoint, problem: "only two-dimensional WKB is supported"},
		{rawType: 999, problem: "unsupported geometry type 999"},
		{rawType: 1000, problem: "only two-dimensional WKB is supported"},
	} {
		_, err := Unmarshal(rawGeometry(test.rawType), geo.WGS84(), geo.DefaultLimits())
		if err == nil || !strings.Contains(err.Error(), test.problem) {
			t.Fatalf("Unmarshal(type %#x) error = %v, want problem %q", test.rawType, err, test.problem)
		}
	}

	for _, srid := range []uint32{0, math.MaxInt32, math.MaxInt32 + 1} {
		data := rawGeometry(flagSRID | typePoint)
		data = binary.LittleEndian.AppendUint32(data, srid)
		data = binary.LittleEndian.AppendUint64(data, math.Float64bits(0))
		data = binary.LittleEndian.AppendUint64(data, math.Float64bits(0))
		geometry, err := UnmarshalEWKB(data, geo.DefaultLimits())
		if srid == math.MaxInt32 {
			if err != nil {
				t.Fatalf("UnmarshalEWKB(maximum SRID) error = %v", err)
			}
			if geometry.CRS().SRID() != math.MaxInt32 {
				t.Fatalf("UnmarshalEWKB(maximum SRID) = %d", geometry.CRS().SRID())
			}
			continue
		}
		if !errors.Is(err, geo.ErrCRS) || !strings.Contains(err.Error(), "SRID must be a positive 32-bit integer") {
			t.Fatalf("UnmarshalEWKB(invalid SRID %d) error = %v", srid, err)
		}
	}
}

func TestDepthCountdownRejectsExhaustedAndNegativeLimits(t *testing.T) {
	t.Parallel()

	data := rawGeometry(typePoint)
	data = binary.LittleEndian.AppendUint64(data, math.Float64bits(0))
	data = binary.LittleEndian.AppendUint64(data, math.Float64bits(0))
	for _, remainingDepth := range []int{0, -1} {
		parser := binaryParser{data: data, limits: geo.DefaultLimits()}
		if _, _, err := parser.geometry(remainingDepth, 0, geo.WGS84(), true); !errors.Is(err, geo.ErrTopology) {
			t.Fatalf("geometry(depth %d) error = %v", remainingDepth, err)
		}
	}
	limits := geo.DefaultLimits()
	limits.MaxCollectionDepth = -1
	if _, err := Unmarshal(data, geo.WGS84(), limits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Unmarshal(negative depth limit) error = %v", err)
	}
}

func TestDepthCountdownOwnsEveryNestedGeometryBoundary(t *testing.T) {
	t.Parallel()

	coordinates := []geo.Coordinate{
		internalPoint(t, 0, 0).Coordinate(),
		internalPoint(t, 1, 0).Coordinate(),
		internalPoint(t, 1, 1).Coordinate(),
		internalPoint(t, 0, 0).Coordinate(),
	}
	line, err := geo.NewLineString(coordinates[:2])
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	polygon, err := geo.NewPolygon(coordinates, nil)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	multiPoint, err := geo.NewMultiPoint(coordinates[:1], geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiPoint() error = %v", err)
	}
	multiLine, err := geo.NewMultiLineString([]geo.LineString{line}, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiLineString() error = %v", err)
	}
	multiPolygon, err := geo.NewMultiPolygon([]geo.Polygon{polygon}, geo.WGS84())
	if err != nil {
		t.Fatalf("NewMultiPolygon() error = %v", err)
	}
	collection, err := geo.NewGeometryCollection([]geo.Geometry{internalPoint(t, 0, 0)}, geo.WGS84())
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}

	for _, geometry := range []geo.Geometry{multiPoint, multiLine, multiPolygon, collection} {
		data, err := Marshal(geometry, binary.LittleEndian)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", geometry.Type(), err)
		}
		parser := binaryParser{data: data, limits: geo.DefaultLimits()}
		_, _, err = parser.geometry(1, 0, geo.WGS84(), true)
		if err == nil || !strings.Contains(err.Error(), "collection depth limit exceeded") {
			t.Fatalf("geometry(%s, exhausted child depth) error = %v", geometry.Type(), err)
		}
	}
}

func TestCountUsesTheExactRemainingByteBudget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		payload   int
		limit     int
		wantCount int
		wantError error
	}{
		{name: "exact bytes", payload: 10, limit: 2, wantCount: 2},
		{name: "one byte short", payload: 9, limit: 2, wantError: io.ErrUnexpectedEOF},
		{name: "resource limit", payload: 10, limit: 1, wantError: geo.ErrTopology},
	} {
		data := binary.LittleEndian.AppendUint32(nil, 2)
		data = append(data, make([]byte, test.payload)...)
		parser := binaryParser{data: data, limits: geo.DefaultLimits()}
		count, err := parser.count(binary.LittleEndian, test.limit, 5, "item")
		if test.wantError == nil {
			if err != nil || count != test.wantCount {
				t.Fatalf("%s count = %d, error = %v", test.name, count, err)
			}
			continue
		}
		if test.wantError == geo.ErrTopology {
			if !errors.Is(err, geo.ErrTopology) {
				t.Fatalf("%s error = %v", test.name, err)
			}
			continue
		}
		if !errors.Is(err, test.wantError) {
			t.Fatalf("%s error = %v", test.name, err)
		}
	}
}

func TestInitialCapacityMatchesTheEncodedGeometrySize(t *testing.T) {
	t.Parallel()

	point := internalPoint(t, 0, 0)
	line, err := geo.NewLineString([]geo.Coordinate{
		point.Coordinate(),
		internalPoint(t, 1, 1).Coordinate(),
	})
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	for _, test := range []struct {
		geometry    geo.Geometry
		includeSRID bool
		want        int
	}{
		{geometry: point, includeSRID: false, want: 21},
		{geometry: point, includeSRID: true, want: 25},
		{geometry: line, includeSRID: false, want: 41},
		{geometry: line, includeSRID: true, want: 45},
	} {
		if got := initialCapacity(test.geometry, test.includeSRID); got != test.want {
			t.Fatalf("initialCapacity(%s, %t) = %d, want %d", test.geometry.Type(), test.includeSRID, got, test.want)
		}
	}
}

func TestInputLimitsAndCRSMetadataUseExactBoundaries(t *testing.T) {
	t.Parallel()

	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 2
	if err := validateInput([]byte{0, 1}, geo.WGS84(), limits); err != nil {
		t.Fatalf("validateInput(exact byte limit) error = %v", err)
	}
	if err := validateInput([]byte{0, 1, 2}, geo.WGS84(), limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("validateInput(over byte limit) error = %v", err)
	}
	if !crsValid(geo.WGS84()) {
		t.Fatal("crsValid(WGS84) = false")
	}
	unnamed, err := geo.NewCRS(4326, "EPSG:4326")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	if !crsValid(unnamed) {
		t.Fatal("crsValid(explicit CRS) = false")
	}
	if crsValid(geo.CRS{}) {
		t.Fatal("crsValid(zero CRS) = true")
	}
}

func rawGeometry(rawType uint32) []byte {
	return binary.LittleEndian.AppendUint32([]byte{1}, rawType)
}

func internalPoint(t *testing.T, longitude, latitude float64) geo.Point {
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
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	return point
}

package wkb_test

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkb"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func TestPointMatchesCanonicalLittleEndianWKBAndEWKB(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 1, 2)
	encoded, err := wkb.Marshal(point, binary.LittleEndian)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := hex.EncodeToString(encoded), "0101000000000000000000f03f0000000000000040"; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}
	ewkb, err := wkb.MarshalEWKB(point, binary.LittleEndian)
	if err != nil {
		t.Fatalf("MarshalEWKB() error = %v", err)
	}
	if got, want := hex.EncodeToString(ewkb), "0101000020e6100000000000000000f03f0000000000000040"; got != want {
		t.Fatalf("MarshalEWKB() = %s, want %s", got, want)
	}

	decoded, err := wkb.Unmarshal(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, point) {
		t.Fatal("WKB round trip changed point")
	}
	decodedEWKB, err := wkb.UnmarshalEWKB(ewkb, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalEWKB() error = %v", err)
	}
	if !geo.EqualGeometry(decodedEWKB, point) {
		t.Fatal("EWKB round trip changed point")
	}
}

func TestBigEndianCollectionRoundTrips(t *testing.T) {
	t.Parallel()

	line, err := geo.NewLineString(coords(t, [][2]float64{{0, 0}, {1, 1}}))
	if err != nil {
		t.Fatalf("NewLineString() error = %v", err)
	}
	polygon, err := geo.NewPolygon(
		coords(t, [][2]float64{{0, 0}, {2, 0}, {2, 2}, {0, 0}}),
		nil,
	)
	if err != nil {
		t.Fatalf("NewPolygon() error = %v", err)
	}
	collection, err := geo.NewGeometryCollection(
		[]geo.Geometry{line, polygon},
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewGeometryCollection() error = %v", err)
	}
	encoded, err := wkb.MarshalEWKB(collection, binary.BigEndian)
	if err != nil {
		t.Fatalf("MarshalEWKB() error = %v", err)
	}
	decoded, err := wkb.UnmarshalEWKB(encoded, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalEWKB() error = %v", err)
	}
	if !geo.EqualGeometry(decoded, collection) {
		t.Fatal("big-endian collection round trip changed geometry")
	}
}

func TestEveryGeometryFamilyRoundTripsInBothByteOrders(t *testing.T) {
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
		for _, order := range []binary.ByteOrder{
			binary.LittleEndian,
			binary.BigEndian,
		} {
			encoded, err := wkb.Marshal(geometry, order)
			if err != nil {
				t.Fatalf("Marshal(%s, %s) error = %v", geometry.Type(), order, err)
			}
			decoded, err := wkb.Unmarshal(
				encoded,
				geo.WGS84(),
				geo.DefaultLimits(),
			)
			if err != nil {
				t.Fatalf("Unmarshal(%s, %s) error = %v", geometry.Type(), order, err)
			}
			if !geo.EqualGeometry(decoded, geometry) {
				t.Fatalf("WKB round trip changed %s in %s", geometry.Type(), order)
			}

			ewkb, err := wkb.MarshalEWKB(geometry, order)
			if err != nil {
				t.Fatalf("MarshalEWKB(%s, %s) error = %v", geometry.Type(), order, err)
			}
			decodedEWKB, err := wkb.UnmarshalEWKB(ewkb, geo.DefaultLimits())
			if err != nil {
				t.Fatalf("UnmarshalEWKB(%s, %s) error = %v", geometry.Type(), order, err)
			}
			if !geo.EqualGeometry(decodedEWKB, geometry) {
				t.Fatalf("EWKB round trip changed %s in %s", geometry.Type(), order)
			}
		}
	}
}

func TestDecoderRejectsHostileCountsTruncationAndTrailingBytes(t *testing.T) {
	t.Parallel()

	hostile, err := hex.DecodeString("0104000000ffffffff")
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	_, err = wkb.Unmarshal(hostile, geo.WGS84(), geo.DefaultLimits())
	if !errors.Is(err, geo.ErrEncoding) || !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("hostile count error = %v, want encoding and topology errors", err)
	}

	point, err := wkb.Marshal(mustPoint(t, 1, 2), binary.LittleEndian)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, input := range [][]byte{point[:len(point)-1], append(point, 0)} {
		_, err := wkb.Unmarshal(input, geo.WGS84(), geo.DefaultLimits())
		if !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("Unmarshal(%x) error = %v, want ErrEncoding", input, err)
		}
	}
}

func TestWKBAndEWKBSRIDContractsAreDistinct(t *testing.T) {
	t.Parallel()

	ewkb, err := wkb.MarshalEWKB(mustPoint(t, 1, 2), binary.LittleEndian)
	if err != nil {
		t.Fatalf("MarshalEWKB() error = %v", err)
	}
	_, err = wkb.Unmarshal(ewkb, geo.WGS84(), geo.DefaultLimits())
	if !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("WKB accepted EWKB SRID: %v", err)
	}
	wkbValue, err := wkb.Marshal(mustPoint(t, 1, 2), binary.LittleEndian)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	_, err = wkb.UnmarshalEWKB(wkbValue, geo.DefaultLimits())
	if !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("EWKB accepted missing SRID: %v", err)
	}
}

func TestDecoderRejectsMalformedHeadersFlagsNestingAndCounts(t *testing.T) {
	t.Parallel()

	pointZ := littleGeometry(0x80000001)
	unsupported := littleGeometry(99)
	zeroRingPolygon := appendUint32(littleGeometry(3), 0)
	truncatedPolygonRing := appendUint32(littleGeometry(3), 1)
	truncatedPolygonCoordinates := appendUint32(littleGeometry(3), 1)
	truncatedPolygonCoordinates = appendUint32(truncatedPolygonCoordinates, 1)
	truncatedLineCount := littleGeometry(2)
	lineImpossibleCount := appendUint32(littleGeometry(2), 10)
	lineMissingLatitude := appendUint32(littleGeometry(2), 1)
	lineMissingLatitude = appendUint64(lineMissingLatitude, 0)
	emptyLine := appendUint32(littleGeometry(2), 0)
	multiPointWrongChild := appendUint32(littleGeometry(4), 1)
	multiPointWrongChild = append(multiPointWrongChild, littleGeometry(2)...)
	multiPointWrongChild = appendUint32(multiPointWrongChild, 0)
	nonFinitePoint := littleGeometry(1)
	nonFinitePoint = appendUint64(nonFinitePoint, math.Float64bits(math.NaN()))
	nonFinitePoint = appendUint64(nonFinitePoint, 0)
	nonFiniteLatitude := littleGeometry(1)
	nonFiniteLatitude = appendUint64(nonFiniteLatitude, 0)
	nonFiniteLatitude = appendUint64(nonFiniteLatitude, math.Float64bits(math.NaN()))
	lineNonFiniteCoordinate := appendUint32(littleGeometry(2), 1)
	lineNonFiniteCoordinate = appendUint64(
		lineNonFiniteCoordinate,
		math.Float64bits(math.NaN()),
	)
	lineNonFiniteCoordinate = appendUint64(lineNonFiniteCoordinate, 0)

	inputs := [][]byte{
		{},
		{2},
		{1},
		pointZ,
		unsupported,
		zeroRingPolygon,
		truncatedPolygonRing,
		truncatedPolygonCoordinates,
		truncatedLineCount,
		littleGeometry(5),
		littleGeometry(6),
		lineImpossibleCount,
		lineMissingLatitude,
		emptyLine,
		multiPointWrongChild,
		nonFinitePoint,
		nonFiniteLatitude,
		lineNonFiniteCoordinate,
	}
	for _, input := range inputs {
		if _, err := wkb.Unmarshal(
			input,
			geo.WGS84(),
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("Unmarshal(%x) error = %v, want ErrEncoding", input, err)
		}
	}

	for _, kind := range []uint32{4, 5, 6, 7} {
		truncatedChild := appendUint32(littleGeometry(kind), 1)
		expectedChild := map[uint32]uint32{4: 1, 5: 2, 6: 3, 7: 1}[kind]
		truncatedChild = append(truncatedChild, littleGeometry(expectedChild)...)
		if _, err := wkb.Unmarshal(
			truncatedChild,
			geo.WGS84(),
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("Unmarshal(type %d truncated child) error = %v", kind, err)
		}
	}
}

func TestEWKBRejectsInvalidAndMismatchedSRIDs(t *testing.T) {
	t.Parallel()

	for _, srid := range []uint32{0, math.MaxUint32} {
		input := appendUint32(littleGeometry(0x20000001), srid)
		input = appendUint64(input, 0)
		input = appendUint64(input, 0)
		if _, err := wkb.UnmarshalEWKB(
			input,
			geo.DefaultLimits(),
		); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("UnmarshalEWKB(SRID %d) error = %v", srid, err)
		}
	}
	if _, err := wkb.UnmarshalEWKB(
		littleGeometry(0x20000001),
		geo.DefaultLimits(),
	); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalEWKB(truncated SRID) error = %v", err)
	}

	collection := appendUint32(littleGeometry(0x20000007), 4326)
	collection = appendUint32(collection, 1)
	child := appendUint32(littleGeometry(0x20000001), 3857)
	child = appendUint64(child, 0)
	child = appendUint64(child, 0)
	collection = append(collection, child...)
	if _, err := wkb.UnmarshalEWKB(
		collection,
		geo.DefaultLimits(),
	); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("nested SRID mismatch error = %v, want ErrCRS", err)
	}

	webMercator := appendUint32(littleGeometry(0x20000001), 3857)
	webMercator = appendUint64(webMercator, 0)
	webMercator = appendUint64(webMercator, 0)
	geometry, err := wkb.UnmarshalEWKB(webMercator, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("UnmarshalEWKB(EPSG:3857) error = %v", err)
	}
	if geometry.CRS().SRID() != 3857 {
		t.Fatalf("decoded SRID = %d, want 3857", geometry.CRS().SRID())
	}
}

func TestWKBRejectsInvalidOptionsAndResourceLimits(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 1, 2)
	if _, err := wkb.Marshal(point, fakeByteOrder{}); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("Marshal(fake order) error = %v, want ErrUnsupported", err)
	}
	if _, err := wkb.MarshalEWKB(point, fakeByteOrder{}); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("MarshalEWKB(fake order) error = %v, want ErrUnsupported", err)
	}
	if _, err := wkb.Marshal(nil, binary.LittleEndian); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Marshal(nil) error = %v, want ErrTopology", err)
	}
	if _, err := wkb.MarshalEWKB(nil, binary.LittleEndian); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("MarshalEWKB(nil) error = %v, want ErrTopology", err)
	}
	encoded, err := wkb.Marshal(point, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wkb.Unmarshal(encoded, geo.CRS{}, geo.DefaultLimits()); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Unmarshal(zero CRS) error = %v, want ErrCRS", err)
	}
	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 1
	if _, err := wkb.Unmarshal(encoded, geo.WGS84(), limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Unmarshal(byte limit) error = %v, want ErrEncoding", err)
	}
	ewkb, err := wkb.MarshalEWKB(point, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wkb.UnmarshalEWKB(ewkb, limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalEWKB(byte limit) error = %v, want ErrEncoding", err)
	}
	if _, err := wkb.UnmarshalEWKB(append(ewkb, 0), geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("UnmarshalEWKB(trailing byte) error = %v, want ErrEncoding", err)
	}

	deep := appendUint32(littleGeometry(0x20000007), 4326)
	deep = appendUint32(deep, 1)
	deep = append(deep, littleGeometry(7)...)
	deep = appendUint32(deep, 0)
	depthLimits := geo.DefaultLimits()
	depthLimits.MaxCollectionDepth = 1
	if _, err := wkb.UnmarshalEWKB(deep, depthLimits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("UnmarshalEWKB(depth) error = %v, want ErrTopology", err)
	}
}

func littleGeometry(kind uint32) []byte {
	return appendUint32([]byte{1}, kind)
}

func appendUint32(data []byte, value uint32) []byte {
	return binary.LittleEndian.AppendUint32(data, value)
}

func appendUint64(data []byte, value uint64) []byte {
	return binary.LittleEndian.AppendUint64(data, value)
}

type fakeByteOrder struct{}

func (fakeByteOrder) Uint16([]byte) uint16                   { return 0 }
func (fakeByteOrder) Uint32([]byte) uint32                   { return 0 }
func (fakeByteOrder) Uint64([]byte) uint64                   { return 0 }
func (fakeByteOrder) PutUint16([]byte, uint16)               {}
func (fakeByteOrder) PutUint32([]byte, uint32)               {}
func (fakeByteOrder) PutUint64([]byte, uint64)               {}
func (fakeByteOrder) AppendUint16(b []byte, _ uint16) []byte { return b }
func (fakeByteOrder) AppendUint32(b []byte, _ uint32) []byte { return b }
func (fakeByteOrder) AppendUint64(b []byte, _ uint64) []byte { return b }
func (fakeByteOrder) String() string                         { return "fake" }

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

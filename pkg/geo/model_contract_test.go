package geo

import (
	"encoding"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestResourceAdditionRejectsIntegerOverflow(t *testing.T) {
	t.Parallel()

	if !exceeds(math.MaxInt-1, 2, math.MaxInt) {
		t.Fatal("exceeds() accepted an overflowing aggregate count")
	}
	if !exceeds(0, 2, 1) {
		t.Fatal("exceeds() accepted one addition above the limit")
	}
	if exceeds(math.MaxInt-1, 1, math.MaxInt) {
		t.Fatal("exceeds() rejected an aggregate exactly at the limit")
	}
}

func TestGeometryModelAccessorsCloneAndEquality(t *testing.T) {
	t.Parallel()

	first := contractCoordinate(t, 0, 0)
	second := contractCoordinate(t, 1, 1)
	third := contractCoordinate(t, 2, 0)
	point, err := NewPoint(first)
	if err != nil {
		t.Fatal(err)
	}
	line, err := NewLineString([]Coordinate{first, second})
	if err != nil {
		t.Fatal(err)
	}
	polygon, err := NewPolygon(
		[]Coordinate{first, second, third, first},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	multiPoint, err := NewMultiPoint([]Coordinate{first, second}, WGS84())
	if err != nil {
		t.Fatal(err)
	}
	multiLine, err := NewMultiLineString([]LineString{line}, WGS84())
	if err != nil {
		t.Fatal(err)
	}
	multiPolygon, err := NewMultiPolygon([]Polygon{polygon}, WGS84())
	if err != nil {
		t.Fatal(err)
	}
	collection, err := NewGeometryCollection(
		[]Geometry{point, line, polygon, multiPoint, multiLine, multiPolygon},
		WGS84(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if line.Len() != 2 || line.Type() != TypeLineString || !line.CRS().Equal(WGS84()) ||
		line.pointCount() != 2 || line.geometryCount() != 1 || line.geometryDepth() != 1 {
		t.Fatal("line string accessors returned inconsistent metadata")
	}
	if _, ok := line.At(-1); ok {
		t.Fatal("line At(-1) succeeded")
	}
	if _, ok := line.At(line.Len()); ok {
		t.Fatal("line At(length) succeeded")
	}
	if polygon.Type() != TypePolygon || !polygon.CRS().Equal(WGS84()) ||
		polygon.pointCount() != 4 || polygon.geometryCount() != 1 ||
		polygon.geometryDepth() != 1 || len(polygon.Exterior()) != 4 ||
		len(polygon.Holes()) != 0 {
		t.Fatal("polygon accessors returned inconsistent metadata")
	}

	assertMultiPointContract(t, multiPoint, first)
	assertMultiLineContract(t, multiLine, line)
	assertMultiPolygonContract(t, multiPolygon, polygon)
	if collection.Type() != TypeGeometryCollection ||
		!collection.CRS().Equal(WGS84()) || collection.Len() != 6 ||
		collection.pointCount() != 15 || collection.geometryCount() != 7 ||
		collection.geometryDepth() != 2 {
		t.Fatal("collection accessors returned inconsistent metadata")
	}
	if _, ok := collection.At(-1); ok {
		t.Fatal("collection At(-1) succeeded")
	}
	if _, ok := collection.At(collection.Len()); ok {
		t.Fatal("collection At(length) succeeded")
	}
	if len(collection.Geometries()) != collection.Len() {
		t.Fatal("Geometries() returned the wrong count")
	}
	if got, ok := collection.At(0); !ok || !EqualGeometry(got, point) {
		t.Fatal("collection At(0) returned the wrong geometry")
	}

	geometries := []Geometry{
		point, line, polygon, multiPoint, multiLine, multiPolygon, collection,
	}
	pointers := []Geometry{
		&point, &line, &polygon, &multiPoint, &multiLine, &multiPolygon, &collection,
	}
	for index := range geometries {
		cloned, cloneErr := CloneGeometry(pointers[index])
		if cloneErr != nil {
			t.Fatalf("CloneGeometry(%T) error = %v", pointers[index], cloneErr)
		}
		if !EqualGeometry(geometries[index], cloned) {
			t.Fatalf("EqualGeometry(%T clone) = false", geometries[index])
		}
	}
	if EqualGeometry(point, line) || EqualGeometry(nil, point) {
		t.Fatal("EqualGeometry accepted different or nil geometry")
	}
	if !point.Equal(point) || !point.Coordinate().Equal(first) ||
		point.Type() != TypePoint || point.pointCount() != 1 ||
		point.geometryCount() != 1 || point.geometryDepth() != 1 {
		t.Fatal("point contract is inconsistent")
	}
	point.geometryMarker()
	line.geometryMarker()
	polygon.geometryMarker()
	multiPoint.geometryMarker()
	multiLine.geometryMarker()
	multiPolygon.geometryMarker()
	collection.geometryMarker()
}

func TestGeometryModelRejectsNilPointersAndInvalidMetadata(t *testing.T) {
	t.Parallel()

	var pointers = []Geometry{
		(*Point)(nil),
		(*LineString)(nil),
		(*Polygon)(nil),
		(*MultiPoint)(nil),
		(*MultiLineString)(nil),
		(*MultiPolygon)(nil),
		(*GeometryCollection)(nil),
	}
	for _, geometry := range pointers {
		if _, err := CloneGeometry(geometry); !errors.Is(err, ErrTopology) {
			t.Fatalf("CloneGeometry(%T) error = %v, want ErrTopology", geometry, err)
		}
	}
	if _, err := NewPoint(Coordinate{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewPoint(zero) error = %v, want ErrCRS", err)
	}
	if _, err := NewCoordinate(Longitude{}, Latitude{}, CRS{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewCoordinate(zero CRS) error = %v, want ErrCRS", err)
	}
	if _, err := NewMultiPoint(nil, CRS{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewMultiPoint(zero CRS) error = %v, want ErrCRS", err)
	}
	if _, err := NewMultiLineString(nil, CRS{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewMultiLineString(zero CRS) error = %v, want ErrCRS", err)
	}
	if _, err := NewMultiPolygon(nil, CRS{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewMultiPolygon(zero CRS) error = %v, want ErrCRS", err)
	}
	if _, err := NewGeometryCollection(nil, CRS{}); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewGeometryCollection(zero CRS) error = %v, want ErrCRS", err)
	}
}

func TestGeometryConstructorLimitAndEqualityBranches(t *testing.T) {
	t.Parallel()

	first := contractCoordinate(t, 0, 0)
	second := contractCoordinate(t, 1, 1)
	third := contractCoordinate(t, 2, 0)
	if _, err := NewLineString([]Coordinate{first}); !errors.Is(err, ErrTopology) {
		t.Fatalf("short line error = %v, want ErrTopology", err)
	}
	if _, err := NewLineStringWithLimits(
		[]Coordinate{first, second},
		Limits{MaxPoints: 1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("line point limit error = %v, want ErrTopology", err)
	}
	otherCRS, err := NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatal(err)
	}
	otherCoordinate, err := NewCoordinate(first.longitude, first.latitude, otherCRS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLineString([]Coordinate{first, otherCoordinate}); !errors.Is(err, ErrCRS) {
		t.Fatalf("mixed line CRS error = %v, want ErrCRS", err)
	}
	if _, err := NewLineString([]Coordinate{{}, {}}); !errors.Is(err, ErrCRS) {
		t.Fatalf("zero line CRS error = %v, want ErrCRS", err)
	}
	if _, err := NewMultiPointWithLimits(
		[]Coordinate{first, second},
		WGS84(),
		Limits{MaxPoints: 1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("multi point limit error = %v, want ErrTopology", err)
	}
	if _, err := NewMultiPoint([]Coordinate{otherCoordinate}, WGS84()); !errors.Is(err, ErrCRS) {
		t.Fatalf("multi point CRS error = %v, want ErrCRS", err)
	}

	line, err := NewLineString([]Coordinate{first, second})
	if err != nil {
		t.Fatal(err)
	}
	otherLine, err := NewLineString([]Coordinate{first, third})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMultiLineStringWithLimits(
		[]LineString{line},
		WGS84(),
		Limits{MaxGeometries: -1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("multi line geometry limit error = %v, want ErrTopology", err)
	}
	if _, err := NewMultiLineStringWithLimits(
		[]LineString{line},
		WGS84(),
		Limits{MaxPoints: 1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("multi line point limit error = %v, want ErrTopology", err)
	}

	polygon, err := NewPolygon([]Coordinate{first, second, third, first}, nil)
	if err != nil {
		t.Fatal(err)
	}
	otherPolygon, err := NewPolygon(
		[]Coordinate{first, contractCoordinate(t, 1, 2), third, first},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	exterior := []Coordinate{
		first,
		contractCoordinate(t, 10, 0),
		contractCoordinate(t, 10, 10),
		contractCoordinate(t, 0, 10),
		first,
	}
	hole := []Coordinate{
		contractCoordinate(t, 2, 2),
		contractCoordinate(t, 2, 3),
		contractCoordinate(t, 3, 3),
		contractCoordinate(t, 3, 2),
		contractCoordinate(t, 2, 2),
	}
	polygonWithHole, err := NewPolygon(exterior, [][]Coordinate{hole})
	if err != nil {
		t.Fatal(err)
	}
	if polygonWithHole.pointCount() != 10 || len(polygonWithHole.Holes()) != 1 {
		t.Fatal("polygon hole metadata is inconsistent")
	}
	if location, locateErr := polygonWithHole.Locate(otherCoordinate); location != Outside ||
		!errors.Is(locateErr, ErrCRS) {
		t.Fatalf("Locate(other CRS) = %v, %v", location, locateErr)
	}
	openHole := append([]Coordinate(nil), hole[:len(hole)-1]...)
	if _, err := NewPolygon(exterior, [][]Coordinate{openHole}); !errors.Is(err, ErrTopology) {
		t.Fatalf("open hole error = %v, want ErrTopology", err)
	}
	mixedHole := append([]Coordinate(nil), hole...)
	mixedHole[1] = otherCoordinate
	if _, err := NewPolygon(exterior, [][]Coordinate{mixedHole}); !errors.Is(err, ErrCRS) {
		t.Fatalf("mixed hole CRS error = %v, want ErrCRS", err)
	}
	otherHole := make([]Coordinate, len(hole))
	for index, coordinate := range hole {
		otherHole[index], err = NewCoordinate(
			coordinate.longitude,
			coordinate.latitude,
			otherCRS,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewPolygon(exterior, [][]Coordinate{otherHole}); !errors.Is(err, ErrCRS) {
		t.Fatalf("hole CRS error = %v, want ErrCRS", err)
	}
	if _, err := NewPolygonWithLimits(
		exterior,
		[][]Coordinate{hole},
		Limits{MaxPoints: 9},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("polygon hole point limit error = %v, want ErrTopology", err)
	}
	if _, err := NewPolygonWithLimits(
		polygon.exterior,
		nil,
		Limits{MaxRings: -1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("polygon ring limit error = %v, want ErrTopology", err)
	}
	if _, err := NewPolygonWithLimits(
		polygon.exterior,
		nil,
		Limits{MaxPoints: 3},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("polygon point limit error = %v, want ErrTopology", err)
	}
	if _, err := NewMultiPolygonWithLimits(
		[]Polygon{polygon},
		WGS84(),
		Limits{MaxGeometries: -1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("multi polygon geometry limit error = %v, want ErrTopology", err)
	}
	if _, err := NewMultiPolygonWithLimits(
		[]Polygon{polygon},
		WGS84(),
		Limits{MaxPoints: 3},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("multi polygon point limit error = %v, want ErrTopology", err)
	}

	multiLine, _ := NewMultiLineString([]LineString{line}, WGS84())
	otherMultiLine, _ := NewMultiLineString([]LineString{otherLine}, WGS84())
	twoMultiLine, _ := NewMultiLineString([]LineString{line, otherLine}, WGS84())
	multiPolygon, _ := NewMultiPolygon([]Polygon{polygon}, WGS84())
	otherMultiPolygon, _ := NewMultiPolygon([]Polygon{otherPolygon}, WGS84())
	twoMultiPolygon, _ := NewMultiPolygon([]Polygon{polygon, otherPolygon}, WGS84())
	multiPoint, _ := NewMultiPoint([]Coordinate{first}, WGS84())
	otherMultiPoint, _ := NewMultiPoint([]Coordinate{second}, WGS84())
	collection, _ := NewGeometryCollection([]Geometry{line, polygon}, WGS84())
	otherCollection, _ := NewGeometryCollection([]Geometry{otherLine, polygon}, WGS84())
	shortCollection, _ := NewGeometryCollection([]Geometry{line}, WGS84())
	for _, pair := range [][2]Geometry{
		{line, otherLine},
		{polygon, otherPolygon},
		{multiPoint, otherMultiPoint},
		{multiLine, otherMultiLine},
		{multiLine, twoMultiLine},
		{multiPolygon, otherMultiPolygon},
		{multiPolygon, twoMultiPolygon},
		{polygon, polygonWithHole},
		{collection, otherCollection},
		{collection, shortCollection},
	} {
		if EqualGeometry(pair[0], pair[1]) {
			t.Fatalf("EqualGeometry(%s mismatch) = true", pair[0].Type())
		}
	}
	changedHole := append([]Coordinate(nil), hole...)
	changedHole[1] = contractCoordinate(t, 2.25, 3)
	changedPolygon := polygonWithHole
	changedPolygon.holes = [][]Coordinate{changedHole}
	if EqualGeometry(polygonWithHole, changedPolygon) {
		t.Fatal("EqualGeometry accepted different hole coordinates")
	}
	if EqualGeometry(contractFakeGeometry{}, contractFakeGeometry{}) {
		t.Fatal("EqualGeometry accepted an unsupported internal geometry")
	}
	if _, err := NewGeometryCollectionWithLimits(
		[]Geometry{line},
		WGS84(),
		Limits{MaxGeometries: 1},
	); !errors.Is(err, ErrTopology) {
		t.Fatalf("collection geometry limit error = %v, want ErrTopology", err)
	}
	if _, err := NewGeometryCollection([]Geometry{nil}, WGS84()); !errors.Is(err, ErrTopology) {
		t.Fatalf("collection nil error = %v, want ErrTopology", err)
	}
}

func TestTypedErrorContracts(t *testing.T) {
	t.Parallel()

	errorsUnderTest := []struct {
		error  error
		class  error
		phrase string
	}{
		{&RangeError{ValueName: "x", Value: 2, Minimum: 0, Maximum: 1}, ErrRange, "geo: x 2"},
		{&TopologyError{Geometry: "ring", Problem: "open"}, ErrTopology, "geo: invalid ring topology"},
		{&CRSError{SRID: 0, Problem: "missing"}, ErrCRS, "geo: invalid CRS SRID 0"},
		{&EncodingError{Format: "WKT", Problem: "bad"}, ErrEncoding, "geo: invalid WKT encoding"},
		{&UnsupportedError{Operation: "transform", Reason: "missing"}, ErrUnsupported, "geo: unsupported transform"},
	}
	for _, test := range errorsUnderTest {
		if !errors.Is(test.error, test.class) {
			t.Fatalf("errors.Is(%T) = false", test.error)
		}
		if message := test.error.Error(); len(message) < len(test.phrase) ||
			message[:len(test.phrase)] != test.phrase {
			t.Fatalf("%T message = %q, want prefix %q", test.error, message, test.phrase)
		}
	}
	cause := errors.New("cause")
	encoding := &EncodingError{Format: "binary", Problem: "bad", Cause: cause}
	if !errors.Is(encoding, cause) || encoding.Unwrap() != cause {
		t.Fatal("EncodingError did not preserve its cause")
	}
}

func TestValueAndBoundsAccessorContracts(t *testing.T) {
	t.Parallel()

	altitude, err := NewAltitudeMeters(12.5)
	if err != nil {
		t.Fatal(err)
	}
	bearing, err := NewBearing(45)
	if err != nil {
		t.Fatal(err)
	}
	distance, err := NewDistanceMeters(1_250)
	if err != nil {
		t.Fatal(err)
	}
	if altitude.Meters() != 12.5 || !altitude.Equal(altitude) ||
		bearing.Degrees() != 45 || !bearing.Equal(bearing) ||
		distance.Meters() != 1_250 || distance.Kilometers() != 1.25 ||
		!distance.Equal(distance) || WGS84().SRID() != 4326 ||
		WGS84().Name() != "EPSG:4326" {
		t.Fatal("scalar accessors returned inconsistent values")
	}

	bounds, err := NewBoundingBox(
		contractLongitude(t, -10),
		contractLatitude(t, -5),
		contractLongitude(t, 10),
		contractLatitude(t, 5),
		WGS84(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if bounds.West().Degrees() != -10 || bounds.South().Degrees() != -5 ||
		bounds.East().Degrees() != 10 || bounds.North().Degrees() != 5 ||
		!bounds.CRS().Equal(WGS84()) || !bounds.Equal(bounds) {
		t.Fatal("bounding box accessors returned inconsistent values")
	}
	contains, err := bounds.Contains(contractCoordinate(t, 0, 0))
	if err != nil || !contains {
		t.Fatalf("ordinary bounds Contains() = %t, %v", contains, err)
	}
	disjoint, err := NewBoundingBox(
		contractLongitude(t, 20),
		contractLatitude(t, 20),
		contractLongitude(t, 30),
		contractLatitude(t, 30),
		WGS84(),
	)
	if err != nil {
		t.Fatal(err)
	}
	overlaps, err := bounds.Overlaps(disjoint)
	if err != nil || overlaps {
		t.Fatalf("disjoint Overlaps() = %t, %v", overlaps, err)
	}
	world, err := NewBoundingBox(
		contractLongitude(t, -180),
		contractLatitude(t, -90),
		contractLongitude(t, 180),
		contractLatitude(t, 90),
		WGS84(),
	)
	if err != nil {
		t.Fatal(err)
	}
	overlaps, err = world.Overlaps(disjoint)
	if err != nil || !overlaps {
		t.Fatalf("world Overlaps() = %t, %v", overlaps, err)
	}
	if _, err := NewBoundingBox(
		contractLongitude(t, 0),
		contractLatitude(t, 0),
		contractLongitude(t, 1),
		contractLatitude(t, 1),
		CRS{},
	); !errors.Is(err, ErrCRS) {
		t.Fatalf("NewBoundingBox(zero CRS) error = %v, want ErrCRS", err)
	}
	resolved := ResolveLimits(Limits{})
	if resolved != DefaultLimits() {
		t.Fatalf("ResolveLimits({}) = %#v, want defaults", resolved)
	}
}

func TestValueSerializationErrorContracts(t *testing.T) {
	t.Parallel()

	if _, err := (CRS{}).MarshalJSON(); !errors.Is(err, ErrCRS) {
		t.Fatalf("zero CRS MarshalJSON() error = %v, want ErrCRS", err)
	}
	if _, err := (CRS{}).MarshalText(); !errors.Is(err, ErrCRS) {
		t.Fatalf("zero CRS MarshalText() error = %v, want ErrCRS", err)
	}
	if _, err := (Coordinate{}).MarshalJSON(); !errors.Is(err, ErrCRS) {
		t.Fatalf("zero coordinate MarshalJSON() error = %v, want ErrCRS", err)
	}
	if _, err := (Coordinate{}).MarshalText(); !errors.Is(err, ErrCRS) {
		t.Fatalf("zero coordinate MarshalText() error = %v, want ErrCRS", err)
	}

	textValues := []encoding.TextUnmarshaler{
		new(Longitude),
		new(Latitude),
		new(Altitude),
		new(Bearing),
		new(Distance),
	}
	for _, value := range textValues {
		if err := value.UnmarshalText([]byte("not-a-number")); !errors.Is(err, ErrEncoding) {
			t.Fatalf("%T.UnmarshalText() error = %v, want ErrEncoding", value, err)
		}
	}
	invalidRanges := []struct {
		value encoding.TextUnmarshaler
		text  string
	}{
		{new(Longitude), "181"},
		{new(Latitude), "91"},
		{new(Altitude), "NaN"},
		{new(Bearing), "360"},
		{new(Distance), "-1"},
	}
	for _, test := range invalidRanges {
		if err := test.value.UnmarshalText([]byte(test.text)); !errors.Is(err, ErrRange) {
			t.Fatalf("%T.UnmarshalText(%q) error = %v, want ErrRange",
				test.value, test.text, err)
		}
	}

	var crs CRS
	for _, value := range []string{"missing", "bad:name", "0:name"} {
		if err := crs.UnmarshalText([]byte(value)); err == nil {
			t.Fatalf("CRS.UnmarshalText(%q) succeeded", value)
		}
	}
	if err := json.Unmarshal([]byte("null"), &crs); !errors.Is(err, ErrCRS) {
		t.Fatalf("CRS null JSON error = %v, want ErrCRS", err)
	}
	if err := json.Unmarshal([]byte("[]"), &crs); !errors.Is(err, ErrEncoding) {
		t.Fatalf("CRS malformed JSON error = %v, want ErrEncoding", err)
	}

	var coordinate Coordinate
	for _, value := range []string{
		"missing",
		"1,2,3@4326:EPSG:4326",
		"bad,2@4326:EPSG:4326",
		"1,bad@4326:EPSG:4326",
		"1,2@bad:EPSG:4326",
	} {
		if err := coordinate.UnmarshalText([]byte(value)); err == nil {
			t.Fatalf("Coordinate.UnmarshalText(%q) succeeded", value)
		}
	}
	if err := json.Unmarshal([]byte("[]"), &coordinate); !errors.Is(err, ErrEncoding) {
		t.Fatalf("coordinate malformed JSON error = %v, want ErrEncoding", err)
	}
	if err := json.Unmarshal([]byte(`{}`), &coordinate); !errors.Is(err, ErrCRS) {
		t.Fatalf("coordinate missing CRS error = %v, want ErrCRS", err)
	}
	var longitude Longitude
	if err := json.Unmarshal([]byte(`"bad"`), &longitude); !errors.Is(err, ErrEncoding) {
		t.Fatalf("longitude string JSON error = %v, want ErrEncoding", err)
	}
}

func assertMultiPointContract(t *testing.T, multi MultiPoint, first Coordinate) {
	t.Helper()
	if multi.Type() != TypeMultiPoint || !multi.CRS().Equal(WGS84()) ||
		multi.Len() != 2 || multi.pointCount() != 2 ||
		multi.geometryCount() != 1 || multi.geometryDepth() != 1 ||
		len(multi.Coordinates()) != 2 {
		t.Fatal("multi point accessors returned inconsistent metadata")
	}
	coordinate, ok := multi.At(0)
	if !ok || !coordinate.Equal(first) {
		t.Fatal("multi point At(0) returned the wrong coordinate")
	}
	if _, ok := multi.At(-1); ok {
		t.Fatal("multi point At(-1) succeeded")
	}
	if _, ok := multi.At(multi.Len()); ok {
		t.Fatal("multi point At(length) succeeded")
	}
}

func assertMultiLineContract(t *testing.T, multi MultiLineString, line LineString) {
	t.Helper()
	if multi.Type() != TypeMultiLineString || !multi.CRS().Equal(WGS84()) ||
		multi.Len() != 1 || multi.pointCount() != 2 ||
		multi.geometryCount() != 1 || multi.geometryDepth() != 1 ||
		len(multi.Lines()) != 1 {
		t.Fatal("multi line accessors returned inconsistent metadata")
	}
	got, ok := multi.At(0)
	if !ok || !EqualGeometry(got, line) {
		t.Fatal("multi line At(0) returned the wrong line")
	}
	if _, ok := multi.At(-1); ok {
		t.Fatal("multi line At(-1) succeeded")
	}
	if _, ok := multi.At(multi.Len()); ok {
		t.Fatal("multi line At(length) succeeded")
	}
}

func assertMultiPolygonContract(t *testing.T, multi MultiPolygon, polygon Polygon) {
	t.Helper()
	if multi.Type() != TypeMultiPolygon || !multi.CRS().Equal(WGS84()) ||
		multi.Len() != 1 || multi.pointCount() != 4 ||
		multi.geometryCount() != 1 || multi.geometryDepth() != 1 ||
		len(multi.Polygons()) != 1 {
		t.Fatal("multi polygon accessors returned inconsistent metadata")
	}
	got, ok := multi.At(0)
	if !ok || !EqualGeometry(got, polygon) {
		t.Fatal("multi polygon At(0) returned the wrong polygon")
	}
	if _, ok := multi.At(-1); ok {
		t.Fatal("multi polygon At(-1) succeeded")
	}
	if _, ok := multi.At(multi.Len()); ok {
		t.Fatal("multi polygon At(length) succeeded")
	}
}

func contractCoordinate(t *testing.T, longitude, latitude float64) Coordinate {
	t.Helper()
	lon, err := NewLongitude(longitude)
	if err != nil {
		t.Fatal(err)
	}
	lat, err := NewLatitude(latitude)
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := NewCoordinate(lon, lat, WGS84())
	if err != nil {
		t.Fatal(err)
	}
	return coordinate
}

func contractLongitude(t *testing.T, value float64) Longitude {
	t.Helper()
	longitude, err := NewLongitude(value)
	if err != nil {
		t.Fatal(err)
	}
	return longitude
}

func contractLatitude(t *testing.T, value float64) Latitude {
	t.Helper()
	latitude, err := NewLatitude(value)
	if err != nil {
		t.Fatal(err)
	}
	return latitude
}

type contractFakeGeometry struct{}

func (contractFakeGeometry) Type() GeometryType { return "Fake" }

func (contractFakeGeometry) CRS() CRS { return WGS84() }

func (contractFakeGeometry) geometryMarker() {}

func (contractFakeGeometry) pointCount() int { return 0 }

func (contractFakeGeometry) geometryCount() int { return 1 }

func (contractFakeGeometry) geometryDepth() int { return 1 }

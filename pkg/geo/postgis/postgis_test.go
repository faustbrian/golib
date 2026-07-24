package postgis_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/postgis"
)

const geometryOID = 99999

func TestValueImplementsScannerValuerAndPgxCodec(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 24.9384, 60.1699)
	value, err := postgis.NewValue(point, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("NewValue() error = %v", err)
	}
	var _ driver.Valuer = value
	encodedValue, err := value.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	var scanned postgis.Value
	if err := scanned.Scan(encodedValue); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	scannedGeometry, valid := scanned.Geometry()
	if !valid || !geo.EqualGeometry(scannedGeometry, point) {
		t.Fatal("Scanner/Valuer round trip changed geometry")
	}

	typeMap := pgtype.NewMap()
	postgis.Register(typeMap, geometryOID, geo.DefaultLimits())
	binaryValue, err := typeMap.Encode(
		geometryOID,
		pgtype.BinaryFormatCode,
		value,
		nil,
	)
	if err != nil {
		t.Fatalf("pgx binary Encode() error = %v", err)
	}
	var binaryScanned postgis.Value
	if err := typeMap.Scan(
		geometryOID,
		pgtype.BinaryFormatCode,
		binaryValue,
		&binaryScanned,
	); err != nil {
		t.Fatalf("pgx binary Scan() error = %v", err)
	}
	geometry, valid := binaryScanned.Geometry()
	if !valid || !geo.EqualGeometry(geometry, point) {
		t.Fatal("pgx binary round trip changed geometry")
	}

	textValue, err := typeMap.Encode(
		geometryOID,
		pgtype.TextFormatCode,
		value,
		nil,
	)
	if err != nil {
		t.Fatalf("pgx text Encode() error = %v", err)
	}
	var textScanned postgis.Value
	if err := typeMap.Scan(
		geometryOID,
		pgtype.TextFormatCode,
		textValue,
		&textScanned,
	); err != nil {
		t.Fatalf("pgx text Scan() error = %v", err)
	}
	geometry, valid = textScanned.Geometry()
	if !valid || !geo.EqualGeometry(geometry, point) {
		t.Fatal("pgx text round trip changed geometry")
	}
}

func TestValueHandlesNullAndBoundedMalformedInput(t *testing.T) {
	t.Parallel()

	var value postgis.Value
	if err := value.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) error = %v", err)
	}
	if _, valid := value.Geometry(); valid {
		t.Fatal("Scan(nil) produced a valid geometry")
	}
	if err := value.Scan([]byte{1, 4, 0, 0, 0, 255, 255, 255, 255}); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(hostile) error = %v, want ErrEncoding", err)
	}
	if err := value.Scan(42); err == nil {
		t.Fatal("Scan(unsupported type) succeeded")
	}
}

func TestSQLFragmentsUseValidatedIdentifiersAndBoundValues(t *testing.T) {
	t.Parallel()

	column, err := postgis.NewColumn("locations.coordinate")
	if err != nil {
		t.Fatalf("NewColumn() error = %v", err)
	}
	point := mustPoint(t, 24.9384, 60.1699)
	distance, err := geo.NewDistanceMeters(5_000)
	if err != nil {
		t.Fatalf("NewDistanceMeters() error = %v", err)
	}
	fragment, err := postgis.GeographyDWithin(column, point, distance, 3)
	if err != nil {
		t.Fatalf("GeographyDWithin() error = %v", err)
	}
	want := "ST_DWithin(\"locations\".\"coordinate\"::geography, $3::geography, $4)"
	if fragment.SQL() != want {
		t.Fatalf("SQL() = %q, want %q", fragment.SQL(), want)
	}
	if len(fragment.Args()) != 2 {
		t.Fatalf("Args() length = %d, want 2", len(fragment.Args()))
	}

	intersects, err := postgis.Intersects(column, point, 1)
	if err != nil {
		t.Fatalf("Intersects() error = %v", err)
	}
	wantIntersects := "ST_Intersects(\"locations\".\"coordinate\", $1::geometry)"
	if intersects.SQL() != wantIntersects {
		t.Fatalf("Intersects SQL = %q, want %q", intersects.SQL(), wantIntersects)
	}
	if _, err := postgis.NewColumn("coordinate); DROP TABLE locations;--"); err == nil {
		t.Fatal("NewColumn() accepted SQL injection text")
	}
}

func mustPoint(t *testing.T, longitude, latitude float64) geo.Point {
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

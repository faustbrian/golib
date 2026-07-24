package postgis

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestValueCoversNullOwnershipAndInvalidInternalGeometry(t *testing.T) {
	t.Parallel()

	null, err := NewValue(nil, geo.Limits{})
	if err != nil {
		t.Fatalf("NewValue(nil) error = %v", err)
	}
	if geometry, valid := null.Geometry(); geometry != nil || valid {
		t.Fatal("NULL value returned geometry")
	}
	if encoded, err := null.Value(); err != nil || encoded != nil {
		t.Fatalf("NULL Value() = %v, %v; want nil, nil", encoded, err)
	}

	var nilPoint *geo.Point
	if _, err := NewValue(nilPoint, geo.DefaultLimits()); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("NewValue(typed nil) error = %v, want ErrTopology", err)
	}
	broken := Value{geometry: nilPoint, valid: true}
	if geometry, valid := broken.Geometry(); geometry != nil || valid {
		t.Fatal("Geometry() exposed invalid internal geometry")
	}
	if _, err := broken.Value(); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("broken Value() error = %v, want ErrTopology", err)
	}

	var target *Value
	if err := target.Scan(nil); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("nil Scan() error = %v, want ErrEncoding", err)
	}
}

func TestCodecPlanningAndDatabaseSQLDecoding(t *testing.T) {
	t.Parallel()

	codec := Codec{Limits: geo.DefaultLimits()}
	if codec.FormatSupported(99) {
		t.Fatal("FormatSupported(99) = true")
	}
	if codec.PreferredFormat() != pgtype.BinaryFormatCode {
		t.Fatal("PreferredFormat() is not binary")
	}
	if codec.PlanEncode(nil, 0, 99, Value{}) != nil {
		t.Fatal("PlanEncode() accepted unsupported format")
	}
	if codec.PlanEncode(nil, 0, pgtype.BinaryFormatCode, 42) != nil {
		t.Fatal("PlanEncode() accepted unsupported value")
	}
	if codec.PlanScan(nil, 0, 99, &Value{}) != nil {
		t.Fatal("PlanScan() accepted unsupported format")
	}
	if codec.PlanScan(nil, 0, pgtype.BinaryFormatCode, new(int)) != nil {
		t.Fatal("PlanScan() accepted unsupported target")
	}

	if decoded, err := codec.DecodeDatabaseSQLValue(nil, 0, 99, nil); err != nil || decoded != nil {
		t.Fatalf("DecodeDatabaseSQLValue(nil) = %v, %v", decoded, err)
	}
	if _, err := codec.DecodeDatabaseSQLValue(nil, 0, 99, []byte{1}); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("DecodeDatabaseSQLValue(format) error = %v", err)
	}
	binarySource := []byte{1, 2, 3}
	decoded, err := codec.DecodeDatabaseSQLValue(nil, 0, pgtype.BinaryFormatCode, binarySource)
	if err != nil {
		t.Fatalf("DecodeDatabaseSQLValue(binary) error = %v", err)
	}
	binaryResult := decoded.([]byte)
	binarySource[0] = 9
	if binaryResult[0] != 1 {
		t.Fatal("binary database value aliases source")
	}
	decoded, err = codec.DecodeDatabaseSQLValue(nil, 0, pgtype.TextFormatCode, []byte("abc"))
	if err != nil || decoded != "abc" {
		t.Fatalf("DecodeDatabaseSQLValue(text) = %v, %v", decoded, err)
	}
}

func TestEncodeAndScanPlansCoverEverySupportedValue(t *testing.T) {
	t.Parallel()

	point := testPoint(t, 24.9384, 60.1699, geo.WGS84())
	value, err := NewValue(point, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("NewValue() error = %v", err)
	}
	codec := Codec{Limits: geo.DefaultLimits()}
	binaryPlan := codec.PlanEncode(nil, 0, pgtype.BinaryFormatCode, value)
	textPlan := codec.PlanEncode(nil, 0, pgtype.TextFormatCode, &value)
	geometryPlan := codec.PlanEncode(nil, 0, pgtype.BinaryFormatCode, geo.Geometry(point))
	for name, test := range map[string]struct {
		plan  pgtype.EncodePlan
		value any
	}{
		"value":    {plan: binaryPlan, value: value},
		"pointer":  {plan: textPlan, value: &value},
		"geometry": {plan: geometryPlan, value: geo.Geometry(point)},
	} {
		encoded, encodeErr := test.plan.Encode(test.value, []byte{0xaa})
		if encodeErr != nil {
			t.Fatalf("%s Encode() error = %v", name, encodeErr)
		}
		if len(encoded) <= 1 || encoded[0] != 0xaa {
			t.Fatalf("%s Encode() did not append to buffer", name)
		}
	}
	if encoded, err := binaryPlan.Encode(Value{}, nil); err != nil || encoded != nil {
		t.Fatalf("Encode(NULL Value) = %v, %v", encoded, err)
	}
	if encoded, err := binaryPlan.Encode((*Value)(nil), nil); err != nil || encoded != nil {
		t.Fatalf("Encode(nil *Value) = %v, %v", encoded, err)
	}
	if _, err := binaryPlan.Encode(42, nil); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Encode(unsupported) error = %v", err)
	}
	var nilPoint *geo.Point
	if _, err := geometryPlan.Encode(geo.Geometry(nilPoint), nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Encode(typed nil geometry) error = %v", err)
	}
	broken := Value{geometry: nilPoint, valid: true}
	if _, err := binaryPlan.Encode(broken, nil); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Encode(broken Value) error = %v", err)
	}

	binary, err := value.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	binaryBytes := binary.([]byte)
	textBytes := make([]byte, hex.EncodedLen(len(binaryBytes)))
	hex.Encode(textBytes, binaryBytes)
	for name, test := range map[string]struct {
		format int16
		source []byte
	}{
		"binary": {format: pgtype.BinaryFormatCode, source: binaryBytes},
		"text":   {format: pgtype.TextFormatCode, source: append([]byte(`\x`), textBytes...)},
	} {
		plan := codec.PlanScan(nil, 0, test.format, &Value{})
		var scanned Value
		if scanErr := plan.Scan(test.source, &scanned); scanErr != nil {
			t.Fatalf("%s Scan() error = %v", name, scanErr)
		}
		geometry, valid := scanned.Geometry()
		if !valid || !geo.EqualGeometry(geometry, point) {
			t.Fatalf("%s Scan() changed geometry", name)
		}
	}

	plan := scanPlan{format: pgtype.BinaryFormatCode, limits: geo.DefaultLimits()}
	if err := plan.Scan(nil, new(int)); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(wrong target) error = %v", err)
	}
	var nilTarget *Value
	if err := plan.Scan(nil, nilTarget); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(nil target) error = %v", err)
	}
	var scanned Value
	if err := plan.Scan(nil, &scanned); err != nil {
		t.Fatalf("Scan(NULL) error = %v", err)
	}
	if _, valid := scanned.Geometry(); valid {
		t.Fatal("Scan(NULL) produced geometry")
	}
	if err := (scanPlan{format: pgtype.TextFormatCode}).Scan([]byte("zz"), &scanned); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(invalid hex) error = %v", err)
	}
	if err := (scanPlan{format: 99}).Scan([]byte{1}, &scanned); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(format) error = %v", err)
	}
	if err := plan.Scan([]byte{1}, &scanned); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Scan(EWKB) error = %v", err)
	}
}

func TestCodecDecodeValueCoversNullSuccessAndFailure(t *testing.T) {
	t.Parallel()

	codec := Codec{Limits: geo.DefaultLimits()}
	decoded, err := codec.DecodeValue(nil, 0, pgtype.BinaryFormatCode, nil)
	if err != nil {
		t.Fatalf("DecodeValue(nil) error = %v", err)
	}
	if _, valid := decoded.(Value).Geometry(); valid {
		t.Fatal("DecodeValue(nil) returned valid geometry")
	}
	point := testPoint(t, 1, 2, geo.WGS84())
	value, _ := NewValue(point, geo.DefaultLimits())
	binary, _ := value.Value()
	decoded, err = codec.DecodeValue(nil, 0, pgtype.BinaryFormatCode, binary.([]byte))
	if err != nil {
		t.Fatalf("DecodeValue(binary) error = %v", err)
	}
	geometry, valid := decoded.(Value).Geometry()
	if !valid || !geo.EqualGeometry(geometry, point) {
		t.Fatal("DecodeValue(binary) changed geometry")
	}
	if _, err := codec.DecodeValue(nil, 0, 99, []byte{1}); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("DecodeValue(format) error = %v", err)
	}
}

func TestSQLValidationAndFragmentOwnership(t *testing.T) {
	t.Parallel()

	for _, identifier := range []string{"a.b.c.d", "", "1column", "a.bad-name"} {
		if _, err := NewColumn(identifier); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("NewColumn(%q) error = %v", identifier, err)
		}
	}
	column, err := NewColumn("_schema.Table_2.column3")
	if err != nil {
		t.Fatalf("NewColumn(valid) error = %v", err)
	}
	point := testPoint(t, 1, 2, geo.WGS84())
	distance, _ := geo.NewDistanceMeters(5)
	for name, call := range map[string]func() error{
		"missing geography column": func() error {
			_, err := GeographyDWithin(Column{}, point, distance, 1)
			return err
		},
		"bad geography placeholder": func() error {
			_, err := GeographyDWithin(column, point, distance, 0)
			return err
		},
		"overflow geography placeholder": func() error {
			_, err := GeographyDWithin(column, point, distance, math.MaxInt)
			return err
		},
		"missing geography": func() error {
			_, err := GeographyDWithin(column, nil, distance, 1)
			return err
		},
		"missing intersects column": func() error {
			_, err := Intersects(Column{}, point, 1)
			return err
		},
		"bad intersects placeholder": func() error {
			_, err := Intersects(column, point, 0)
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s succeeded", name)
		}
	}
	foreignCRS, _ := geo.NewCRS(3857, "EPSG:3857")
	foreign := testPoint(t, 1, 2, foreignCRS)
	if _, err := GeographyDWithin(column, foreign, distance, 1); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("GeographyDWithin(CRS) error = %v", err)
	}
	var nilPoint *geo.Point
	if _, err := GeographyDWithin(column, nilPoint, distance, 1); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("GeographyDWithin(typed nil) error = %v", err)
	}
	if _, err := Intersects(column, nilPoint, 1); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("Intersects(typed nil) error = %v", err)
	}
	fragment, err := Intersects(column, point, 1)
	if err != nil {
		t.Fatalf("Intersects() error = %v", err)
	}
	args := fragment.Args()
	args[0] = nil
	if fragment.Args()[0] == nil {
		t.Fatal("Args() aliases fragment storage")
	}

	if err := validatePlaceholder(1, 0); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("validatePlaceholder(count) error = %v", err)
	}
	if geometrySRID(nil) != 0 || geometrySRID(point) != 4326 {
		t.Fatal("geometrySRID() returned unexpected value")
	}
}

func TestDecodeRejectsInvalidHexWithAndWithoutPrefix(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{nil, []byte("zz"), []byte(`\xzz`)} {
		if _, err := decode(data, geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
			t.Fatalf("decode(%q) error = %v", data, err)
		}
	}
}

func testPoint(t *testing.T, longitude, latitude float64, crs geo.CRS) geo.Point {
	t.Helper()
	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, crs)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		t.Fatalf("NewPoint() error = %v", err)
	}
	return point
}

var _ driver.Valuer = Value{}

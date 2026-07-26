// Package postgis connects bounded geo values to PostGIS and pgx.
package postgis

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkb"
)

// Value is a nullable, owned geometry suitable for database/sql and pgx.
type Value struct {
	geometry geo.Geometry
	limits   geo.Limits
	valid    bool
}

// NewValue validates and owns geometry for database use.
func NewValue(geometry geo.Geometry, limits geo.Limits) (Value, error) {
	limits = geo.ResolveLimits(limits)
	if geometry == nil {
		return Value{limits: limits}, nil
	}
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return Value{}, err
	}
	return Value{geometry: owned, limits: limits, valid: true}, nil
}

// Geometry returns an owned copy and reports whether the value is non-NULL.
func (value Value) Geometry() (geo.Geometry, bool) {
	if !value.valid {
		return nil, false
	}
	owned, err := geo.CloneGeometry(value.geometry)
	if err != nil {
		return nil, false
	}
	return owned, true
}

// Value implements database/sql/driver.Valuer using PostGIS EWKB.
func (value Value) Value() (driver.Value, error) {
	if !value.valid {
		return nil, nil
	}
	encoded, err := wkb.MarshalEWKB(value.geometry, binary.LittleEndian)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// Scan implements database/sql.Scanner for binary EWKB and hexadecimal EWKB.
func (value *Value) Scan(source any) error {
	if value == nil {
		return encodingError("cannot scan into a nil Value", nil)
	}
	if source == nil {
		*value = Value{limits: geo.ResolveLimits(value.limits)}
		return nil
	}
	var data []byte
	switch source := source.(type) {
	case []byte:
		data = source
	case string:
		data = []byte(source)
	default:
		return encodingError(fmt.Sprintf("unsupported scan source %T", source), nil)
	}
	geometry, err := decode(data, value.limits)
	if err != nil {
		return err
	}
	*value = Value{
		geometry: geometry,
		limits:   geo.ResolveLimits(value.limits),
		valid:    true,
	}
	return nil
}

// Codec is a pgx codec for the PostGIS geometry type.
type Codec struct {
	Limits geo.Limits
}

// FormatSupported reports the PostGIS wire formats supported by Codec.
func (Codec) FormatSupported(format int16) bool {
	return format == pgtype.BinaryFormatCode || format == pgtype.TextFormatCode
}

// PreferredFormat selects compact binary EWKB.
func (Codec) PreferredFormat() int16 { return pgtype.BinaryFormatCode }

// PlanEncode returns an EWKB encoding plan for supported geometry values.
func (codec Codec) PlanEncode(
	_ *pgtype.Map,
	_ uint32,
	format int16,
	value any,
) pgtype.EncodePlan {
	if !codec.FormatSupported(format) {
		return nil
	}
	switch value.(type) {
	case Value, *Value, geo.Geometry:
		return encodePlan{format: format, limits: codec.Limits}
	default:
		return nil
	}
}

// PlanScan returns an EWKB scanning plan for Value targets.
func (codec Codec) PlanScan(
	_ *pgtype.Map,
	_ uint32,
	format int16,
	target any,
) pgtype.ScanPlan {
	if !codec.FormatSupported(format) {
		return nil
	}
	if _, ok := target.(*Value); !ok {
		return nil
	}
	return scanPlan{format: format, limits: codec.Limits}
}

// DecodeDatabaseSQLValue decodes a wire value for database/sql.
func (codec Codec) DecodeDatabaseSQLValue(
	_ *pgtype.Map,
	_ uint32,
	format int16,
	source []byte,
) (driver.Value, error) {
	if source == nil {
		return nil, nil
	}
	if !codec.FormatSupported(format) {
		return nil, encodingError("unsupported PostGIS format", nil)
	}
	if format == pgtype.BinaryFormatCode {
		return append([]byte(nil), source...), nil
	}
	return string(source), nil
}

// DecodeValue decodes a wire value to a nullable Value.
func (codec Codec) DecodeValue(
	_ *pgtype.Map,
	_ uint32,
	format int16,
	source []byte,
) (any, error) {
	if source == nil {
		return Value{limits: geo.ResolveLimits(codec.Limits)}, nil
	}
	var value Value
	plan := scanPlan{format: format, limits: codec.Limits}
	if err := plan.Scan(source, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// Register registers a PostGIS geometry OID on a pgx type map.
func Register(typeMap *pgtype.Map, oid uint32, limits geo.Limits) {
	typeMap.RegisterType(&pgtype.Type{
		Name:  "geometry",
		OID:   oid,
		Codec: Codec{Limits: geo.ResolveLimits(limits)},
	})
}

type encodePlan struct {
	format int16
	limits geo.Limits
}

func (plan encodePlan) Encode(value any, buffer []byte) ([]byte, error) {
	var databaseValue Value
	switch value := value.(type) {
	case Value:
		databaseValue = value
	case *Value:
		if value == nil {
			return nil, nil
		}
		databaseValue = *value
	case geo.Geometry:
		var err error
		databaseValue, err = NewValue(value, plan.limits)
		if err != nil {
			return nil, err
		}
	default:
		return nil, encodingError(fmt.Sprintf("unsupported encode value %T", value), nil)
	}
	if !databaseValue.valid {
		return nil, nil
	}
	encoded, err := wkb.MarshalEWKB(databaseValue.geometry, binary.LittleEndian)
	if err != nil {
		return nil, err
	}
	if plan.format == pgtype.BinaryFormatCode {
		return append(buffer, encoded...), nil
	}
	return hex.AppendEncode(buffer, encoded), nil
}

type scanPlan struct {
	format int16
	limits geo.Limits
}

func (plan scanPlan) Scan(source []byte, target any) error {
	value, ok := target.(*Value)
	if !ok || value == nil {
		return encodingError(fmt.Sprintf("unsupported scan target %T", target), nil)
	}
	if source == nil {
		*value = Value{limits: geo.ResolveLimits(plan.limits)}
		return nil
	}
	data := source
	if plan.format == pgtype.TextFormatCode {
		decoded := make([]byte, hex.DecodedLen(len(trimHexPrefix(source))))
		count, err := hex.Decode(decoded, trimHexPrefix(source))
		if err != nil {
			return encodingError("invalid hexadecimal EWKB", err)
		}
		data = decoded[:count]
	} else if plan.format != pgtype.BinaryFormatCode {
		return encodingError("unsupported PostGIS format", nil)
	}
	geometry, err := wkb.UnmarshalEWKB(data, plan.limits)
	if err != nil {
		return err
	}
	*value = Value{
		geometry: geometry,
		limits:   geo.ResolveLimits(plan.limits),
		valid:    true,
	}
	return nil
}

// Column is a validated, quoted SQL column identifier.
type Column struct{ sql string }

// NewColumn accepts one to three dot-separated SQL identifier segments.
func NewColumn(identifier string) (Column, error) {
	segments := strings.Split(identifier, ".")
	if len(segments) < 1 || len(segments) > 3 {
		return Column{}, encodingError("column must have one to three segments", nil)
	}
	quoted := make([]string, len(segments))
	for index, segment := range segments {
		if !validIdentifier(segment) {
			return Column{}, encodingError("column contains an invalid identifier", nil)
		}
		quoted[index] = "\"" + segment + "\""
	}
	return Column{sql: strings.Join(quoted, ".")}, nil
}

// Fragment is a SQL expression with separately bound arguments.
type Fragment struct {
	sql  string
	args []any
}

// SQL returns the fixed SQL expression.
func (fragment Fragment) SQL() string { return fragment.sql }

// Args returns a shallow copy of the bound arguments.
func (fragment Fragment) Args() []any {
	return append([]any(nil), fragment.args...)
}

// GeographyDWithin constructs a geography distance predicate.
func GeographyDWithin(
	column Column,
	geometry geo.Geometry,
	distance geo.Distance,
	firstPlaceholder int,
) (Fragment, error) {
	if err := validateColumn(column); err != nil {
		return Fragment{}, err
	}
	if err := validatePlaceholder(firstPlaceholder, 2); err != nil {
		return Fragment{}, err
	}
	if geometry == nil {
		return Fragment{}, &geo.CRSError{
			SRID:    0,
			Problem: "geography requires EPSG:4326",
		}
	}
	value, err := NewValue(geometry, geo.DefaultLimits())
	if err != nil {
		return Fragment{}, err
	}
	if value.geometry.CRS().SRID() != geo.WGS84().SRID() {
		return Fragment{}, &geo.CRSError{
			SRID:    geometrySRID(value.geometry),
			Problem: "geography requires EPSG:4326",
		}
	}
	secondPlaceholder := firstPlaceholder + 1
	return Fragment{
		sql: "ST_DWithin(" + column.sql + "::geography, $" +
			strconv.Itoa(firstPlaceholder) + "::geography, $" +
			strconv.Itoa(secondPlaceholder) + ")",
		args: []any{value, distance.Meters()},
	}, nil
}

// Intersects constructs a PostGIS geometry intersection predicate.
func Intersects(
	column Column,
	geometry geo.Geometry,
	placeholder int,
) (Fragment, error) {
	if err := validateColumn(column); err != nil {
		return Fragment{}, err
	}
	if err := validatePlaceholder(placeholder, 1); err != nil {
		return Fragment{}, err
	}
	value, err := NewValue(geometry, geo.DefaultLimits())
	if err != nil {
		return Fragment{}, err
	}
	return Fragment{
		sql: "ST_Intersects(" + column.sql + ", $" +
			strconv.Itoa(placeholder) + "::geometry)",
		args: []any{value},
	}, nil
}

func decode(data []byte, limits geo.Limits) (geo.Geometry, error) {
	if len(data) == 0 {
		return nil, encodingError("empty EWKB", nil)
	}
	if data[0] == 0 || data[0] == 1 {
		return wkb.UnmarshalEWKB(data, limits)
	}
	encoded := trimHexPrefix(data)
	decoded := make([]byte, hex.DecodedLen(len(encoded)))
	count, err := hex.Decode(decoded, encoded)
	if err != nil {
		return nil, encodingError("invalid hexadecimal EWKB", err)
	}
	return wkb.UnmarshalEWKB(decoded[:count], limits)
}

func trimHexPrefix(data []byte) []byte {
	if len(data) >= 2 && data[0] == '\\' && data[1] == 'x' {
		return data[2:]
	}
	return data
}

func validIdentifier(identifier string) bool {
	if identifier == "" || !asciiLetter(identifier[0]) && identifier[0] != '_' {
		return false
	}
	for index := 1; index < len(identifier); index++ {
		character := identifier[index]
		if !asciiLetter(character) && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func asciiLetter(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z'
}

func validateColumn(column Column) error {
	if column.sql == "" {
		return encodingError("column is required", nil)
	}
	return nil
}

func validatePlaceholder(first, count int) error {
	if first <= 0 || count <= 0 || first > math.MaxInt-count+1 {
		return encodingError("placeholder index is invalid", nil)
	}
	return nil
}

func geometrySRID(geometry geo.Geometry) int32 {
	if geometry == nil {
		return 0
	}
	return geometry.CRS().SRID()
}

func encodingError(message string, cause error) error {
	return &geo.EncodingError{Format: "PostGIS", Problem: message, Cause: cause}
}

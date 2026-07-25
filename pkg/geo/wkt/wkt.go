// Package wkt provides bounded two-dimensional WKT and EWKT codecs.
package wkt

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	geo "github.com/faustbrian/golib/pkg/geo"
)

// Marshal encodes geometry as canonical two-dimensional WKT.
func Marshal(geometry geo.Geometry) ([]byte, error) {
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return nil, err
	}
	var result []byte
	return appendGeometry(result, owned)
}

// MarshalEWKT encodes geometry as WKT prefixed by its explicit SRID.
func MarshalEWKT(geometry geo.Geometry) ([]byte, error) {
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return nil, err
	}
	result := []byte("SRID=")
	result = strconv.AppendInt(result, int64(owned.CRS().SRID()), 10)
	result = append(result, ';')
	return appendGeometry(result, owned)
}

// Unmarshal decodes bounded WKT using caller-supplied CRS metadata.
func Unmarshal(data []byte, crs geo.CRS, limits geo.Limits) (geo.Geometry, error) {
	limits = geo.ResolveLimits(limits)
	if err := validateData(data, crs, limits); err != nil {
		return nil, err
	}
	parser := parser{data: data, crs: crs, limits: limits}
	geometry, err := parser.geometry(1)
	if err != nil {
		return nil, err
	}
	parser.space()
	if parser.position != len(parser.data) {
		return nil, parser.failure("trailing input", nil)
	}
	return geometry, nil
}

// UnmarshalEWKT decodes bounded EWKT and derives explicit EPSG CRS metadata
// from its mandatory positive SRID prefix.
func UnmarshalEWKT(data []byte, limits geo.Limits) (geo.Geometry, error) {
	limits = geo.ResolveLimits(limits)
	if int64(len(data)) > limits.MaxEncodedBytes {
		return nil, encodingError("encoded byte limit exceeded", nil)
	}
	separator := bytes.IndexByte(data, ';')
	if separator < 0 {
		return nil, encodingError("EWKT requires SRID prefix", nil)
	}
	prefix := strings.TrimSpace(string(data[:separator]))
	if len(prefix) < 6 || !strings.EqualFold(prefix[:5], "SRID=") {
		return nil, encodingError("EWKT requires SRID prefix", nil)
	}
	srid, err := strconv.ParseInt(strings.TrimSpace(prefix[5:]), 10, 32)
	if err != nil || srid <= 0 {
		return nil, encodingError("SRID must be a positive 32-bit integer", err)
	}
	var crs geo.CRS
	if srid == 4326 {
		crs = geo.WGS84()
	} else {
		crs, _ = geo.NewCRS(int32(srid), fmt.Sprintf("EPSG:%d", srid))
	}
	return Unmarshal(data[separator+1:], crs, limits)
}

type parser struct {
	data     []byte
	position int
	crs      geo.CRS
	limits   geo.Limits
}

func (parser *parser) geometry(depth int) (geo.Geometry, error) {
	if depth > parser.limits.MaxCollectionDepth {
		return nil, parser.failure("collection depth limit exceeded", geo.ErrTopology)
	}
	kind := strings.ToUpper(parser.identifier())
	if kind == "" {
		return nil, parser.failure("geometry type is required", nil)
	}
	parser.space()
	dimensionPosition := parser.position
	dimension := strings.ToUpper(parser.identifier())
	if dimension == "Z" || dimension == "M" || dimension == "ZM" {
		return nil, parser.failure("only two-dimensional WKT is supported", geo.ErrUnsupported)
	}
	parser.position = dimensionPosition
	if parser.keyword("EMPTY") {
		return parser.empty(kind)
	}

	switch geo.GeometryType(canonicalType(kind)) {
	case geo.TypePoint:
		coordinate, err := parser.parenthesizedPosition()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewPoint(coordinate))
	case geo.TypeLineString:
		coordinates, err := parser.positionList()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewLineStringWithLimits(coordinates, parser.limits))
	case geo.TypePolygon:
		rings, err := parser.ringList()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewPolygonWithLimits(rings[0], rings[1:], parser.limits))
	case geo.TypeMultiPoint:
		coordinates, err := parser.multiPoint()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewMultiPointWithLimits(coordinates, parser.crs, parser.limits))
	case geo.TypeMultiLineString:
		lines, err := parser.multiLineString()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewMultiLineStringWithLimits(lines, parser.crs, parser.limits))
	case geo.TypeMultiPolygon:
		polygons, err := parser.multiPolygon()
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewMultiPolygonWithLimits(polygons, parser.crs, parser.limits))
	case geo.TypeGeometryCollection:
		geometries, err := parser.collection(depth)
		if err != nil {
			return nil, err
		}
		return parser.wrap(geo.NewGeometryCollectionWithLimits(geometries, parser.crs, parser.limits))
	default:
		return nil, parser.failure("unsupported geometry type "+kind, geo.ErrUnsupported)
	}
}

func (parser *parser) empty(kind string) (geo.Geometry, error) {
	switch geo.GeometryType(canonicalType(kind)) {
	case geo.TypeMultiPoint:
		return parser.wrap(geo.NewMultiPointWithLimits(nil, parser.crs, parser.limits))
	case geo.TypeMultiLineString:
		return parser.wrap(geo.NewMultiLineStringWithLimits(nil, parser.crs, parser.limits))
	case geo.TypeMultiPolygon:
		return parser.wrap(geo.NewMultiPolygonWithLimits(nil, parser.crs, parser.limits))
	case geo.TypeGeometryCollection:
		return parser.wrap(geo.NewGeometryCollectionWithLimits(nil, parser.crs, parser.limits))
	case geo.TypePoint, geo.TypeLineString, geo.TypePolygon:
		return nil, parser.failure(kind+" EMPTY has no root-model representation", geo.ErrUnsupported)
	default:
		return nil, parser.failure("unsupported geometry type "+kind, geo.ErrUnsupported)
	}
}

func (parser *parser) parenthesizedPosition() (geo.Coordinate, error) {
	if err := parser.expect('('); err != nil {
		return geo.Coordinate{}, err
	}
	coordinate, err := parser.coordinate()
	if err != nil {
		return geo.Coordinate{}, err
	}
	if err := parser.expect(')'); err != nil {
		return geo.Coordinate{}, err
	}
	return coordinate, nil
}

func (parser *parser) positionList() ([]geo.Coordinate, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	coordinates, err := parser.coordinates()
	if err != nil {
		return nil, err
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return coordinates, nil
}

func (parser *parser) coordinates() ([]geo.Coordinate, error) {
	var coordinates []geo.Coordinate
	for {
		if len(coordinates) >= parser.limits.MaxPoints {
			return nil, parser.failure("point limit exceeded", geo.ErrTopology)
		}
		coordinate, err := parser.coordinate()
		if err != nil {
			return nil, err
		}
		coordinates = append(coordinates, coordinate)
		if !parser.consume(',') {
			return coordinates, nil
		}
	}
}

func (parser *parser) coordinate() (geo.Coordinate, error) {
	longitude, err := parser.number()
	if err != nil {
		return geo.Coordinate{}, err
	}
	if !parser.requiredSpace() {
		return geo.Coordinate{}, parser.failure("position requires longitude and latitude", nil)
	}
	latitude, err := parser.number()
	if err != nil {
		return geo.Coordinate{}, err
	}
	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		return geo.Coordinate{}, parser.failure("invalid longitude", err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		return geo.Coordinate{}, parser.failure("invalid latitude", err)
	}
	coordinate, _ := geo.NewCoordinate(lon, lat, parser.crs)
	return coordinate, nil
}

func (parser *parser) ringList() ([][]geo.Coordinate, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	var rings [][]geo.Coordinate
	for {
		if len(rings) >= parser.limits.MaxRings {
			return nil, parser.failure("ring limit exceeded", geo.ErrTopology)
		}
		ring, err := parser.positionList()
		if err != nil {
			return nil, err
		}
		rings = append(rings, ring)
		if !parser.consume(',') {
			break
		}
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return rings, nil
}

func (parser *parser) multiPoint() ([]geo.Coordinate, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	var coordinates []geo.Coordinate
	for {
		if len(coordinates) >= parser.limits.MaxPoints {
			return nil, parser.failure("point limit exceeded", geo.ErrTopology)
		}
		parser.space()
		var coordinate geo.Coordinate
		var err error
		if parser.peek() == '(' {
			coordinate, err = parser.parenthesizedPosition()
		} else {
			coordinate, err = parser.coordinate()
		}
		if err != nil {
			return nil, err
		}
		coordinates = append(coordinates, coordinate)
		if !parser.consume(',') {
			break
		}
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return coordinates, nil
}

func (parser *parser) multiLineString() ([]geo.LineString, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	var lines []geo.LineString
	for {
		if len(lines) >= parser.limits.MaxGeometries {
			return nil, parser.failure("geometry limit exceeded", geo.ErrTopology)
		}
		coordinates, err := parser.positionList()
		if err != nil {
			return nil, err
		}
		line, err := geo.NewLineStringWithLimits(coordinates, parser.limits)
		if err != nil {
			return nil, parser.failure("invalid line string", err)
		}
		lines = append(lines, line)
		if !parser.consume(',') {
			break
		}
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return lines, nil
}

func (parser *parser) multiPolygon() ([]geo.Polygon, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	var polygons []geo.Polygon
	for {
		if len(polygons) >= parser.limits.MaxGeometries {
			return nil, parser.failure("geometry limit exceeded", geo.ErrTopology)
		}
		rings, err := parser.ringList()
		if err != nil {
			return nil, err
		}
		polygon, err := geo.NewPolygonWithLimits(rings[0], rings[1:], parser.limits)
		if err != nil {
			return nil, parser.failure("invalid polygon", err)
		}
		polygons = append(polygons, polygon)
		if !parser.consume(',') {
			break
		}
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return polygons, nil
}

func (parser *parser) collection(depth int) ([]geo.Geometry, error) {
	if err := parser.expect('('); err != nil {
		return nil, err
	}
	var geometries []geo.Geometry
	for {
		if len(geometries) >= parser.limits.MaxGeometries {
			return nil, parser.failure("geometry limit exceeded", geo.ErrTopology)
		}
		geometry, err := parser.geometry(depth + 1)
		if err != nil {
			return nil, err
		}
		geometries = append(geometries, geometry)
		if !parser.consume(',') {
			break
		}
	}
	if err := parser.expect(')'); err != nil {
		return nil, err
	}
	return geometries, nil
}

func (parser *parser) wrap(geometry geo.Geometry, err error) (geo.Geometry, error) {
	if err != nil {
		return nil, parser.failure("invalid geometry", err)
	}
	return geometry, nil
}

func (parser *parser) number() (float64, error) {
	parser.space()
	start := parser.position
	for parser.position < len(parser.data) {
		value := parser.data[parser.position]
		if value == ',' || value == ')' || isSpace(value) {
			break
		}
		parser.position++
	}
	if start == parser.position {
		return 0, parser.failure("number is required", nil)
	}
	value, err := strconv.ParseFloat(string(parser.data[start:parser.position]), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, parser.failure("invalid finite number", err)
	}
	return value, nil
}

func (parser *parser) identifier() string {
	parser.space()
	start := parser.position
	for parser.position < len(parser.data) {
		value := parser.data[parser.position]
		if (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') {
			parser.position++
			continue
		}
		break
	}
	return string(parser.data[start:parser.position])
}

func (parser *parser) keyword(keyword string) bool {
	parser.space()
	if len(parser.data)-parser.position < len(keyword) ||
		!strings.EqualFold(string(parser.data[parser.position:parser.position+len(keyword)]), keyword) {
		return false
	}
	end := parser.position + len(keyword)
	if end < len(parser.data) {
		value := parser.data[end]
		if (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') {
			return false
		}
	}
	parser.position = end
	return true
}

func (parser *parser) expect(expected byte) error {
	parser.space()
	if parser.position >= len(parser.data) || parser.data[parser.position] != expected {
		return parser.failure(fmt.Sprintf("expected %q", expected), nil)
	}
	parser.position++
	return nil
}

func (parser *parser) consume(expected byte) bool {
	parser.space()
	if parser.position < len(parser.data) && parser.data[parser.position] == expected {
		parser.position++
		return true
	}
	return false
}

func (parser *parser) peek() byte {
	parser.space()
	if parser.position >= len(parser.data) {
		return 0
	}
	return parser.data[parser.position]
}

func (parser *parser) space() {
	for parser.position < len(parser.data) && isSpace(parser.data[parser.position]) {
		parser.position++
	}
}

func (parser *parser) requiredSpace() bool {
	start := parser.position
	parser.space()
	return parser.position > start
}

func (parser *parser) failure(problem string, cause error) *geo.EncodingError {
	return encodingError(fmt.Sprintf("%s at byte %d", problem, parser.position), cause)
}

func appendGeometry(result []byte, geometry geo.Geometry) ([]byte, error) {
	switch value := geometry.(type) {
	case geo.Point:
		result = append(result, "POINT "...)
		result = appendCoordinateGroup(result, []geo.Coordinate{value.Coordinate()})
	case geo.LineString:
		result = append(result, "LINESTRING "...)
		result = appendCoordinateGroup(result, value.Coordinates())
	case geo.Polygon:
		result = append(result, "POLYGON "...)
		result = appendPolygonBody(result, value)
	case geo.MultiPoint:
		result = append(result, "MULTIPOINT "...)
		if value.Len() == 0 {
			result = append(result, "EMPTY"...)
		} else {
			result = append(result, '(')
			for index, coordinate := range value.Coordinates() {
				if index > 0 {
					result = append(result, ", "...)
				}
				result = appendCoordinateGroup(result, []geo.Coordinate{coordinate})
			}
			result = append(result, ')')
		}
	case geo.MultiLineString:
		result = append(result, "MULTILINESTRING "...)
		if value.Len() == 0 {
			result = append(result, "EMPTY"...)
		} else {
			result = append(result, '(')
			for index, line := range value.Lines() {
				if index > 0 {
					result = append(result, ", "...)
				}
				result = appendCoordinateGroup(result, line.Coordinates())
			}
			result = append(result, ')')
		}
	case geo.MultiPolygon:
		result = append(result, "MULTIPOLYGON "...)
		if value.Len() == 0 {
			result = append(result, "EMPTY"...)
		} else {
			result = append(result, '(')
			for index, polygon := range value.Polygons() {
				if index > 0 {
					result = append(result, ", "...)
				}
				result = appendPolygonBody(result, polygon)
			}
			result = append(result, ')')
		}
	case geo.GeometryCollection:
		result = append(result, "GEOMETRYCOLLECTION "...)
		if value.Len() == 0 {
			result = append(result, "EMPTY"...)
		} else {
			result = append(result, '(')
			for index, child := range value.Geometries() {
				if index > 0 {
					result = append(result, ", "...)
				}
				result, _ = appendGeometry(result, child)
			}
			result = append(result, ')')
		}
	}
	return result, nil
}

func appendPolygonBody(result []byte, polygon geo.Polygon) []byte {
	result = append(result, '(')
	result = appendCoordinateGroup(result, polygon.Exterior())
	for _, hole := range polygon.Holes() {
		result = append(result, ", "...)
		result = appendCoordinateGroup(result, hole)
	}
	return append(result, ')')
}

func appendCoordinateGroup(result []byte, coordinates []geo.Coordinate) []byte {
	result = append(result, '(')
	for index, coordinate := range coordinates {
		if index > 0 {
			result = append(result, ", "...)
		}
		result = strconv.AppendFloat(result, coordinate.Longitude().Degrees(), 'g', -1, 64)
		result = append(result, ' ')
		result = strconv.AppendFloat(result, coordinate.Latitude().Degrees(), 'g', -1, 64)
	}
	return append(result, ')')
}

func canonicalType(value string) string {
	switch value {
	case "POINT":
		return string(geo.TypePoint)
	case "LINESTRING":
		return string(geo.TypeLineString)
	case "POLYGON":
		return string(geo.TypePolygon)
	case "MULTIPOINT":
		return string(geo.TypeMultiPoint)
	case "MULTILINESTRING":
		return string(geo.TypeMultiLineString)
	case "MULTIPOLYGON":
		return string(geo.TypeMultiPolygon)
	case "GEOMETRYCOLLECTION":
		return string(geo.TypeGeometryCollection)
	default:
		return value
	}
}

func validateData(data []byte, crs geo.CRS, limits geo.Limits) error {
	if crs.SRID() <= 0 || crs.Name() == "" {
		return &geo.CRSError{SRID: crs.SRID(), Problem: "WKT requires explicit CRS metadata"}
	}
	if int64(len(data)) > limits.MaxEncodedBytes {
		return encodingError("encoded byte limit exceeded", nil)
	}
	return nil
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r'
}

func encodingError(problem string, cause error) *geo.EncodingError {
	return &geo.EncodingError{Format: "WKT", Problem: problem, Cause: cause}
}

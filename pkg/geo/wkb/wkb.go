// Package wkb provides bounded two-dimensional WKB and PostGIS EWKB codecs.
package wkb

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	geo "github.com/faustbrian/golib/pkg/geo"
)

const (
	typePoint              uint32 = 1
	typeLineString         uint32 = 2
	typePolygon            uint32 = 3
	typeMultiPoint         uint32 = 4
	typeMultiLineString    uint32 = 5
	typeMultiPolygon       uint32 = 6
	typeGeometryCollection uint32 = 7

	flagZ    uint32 = 0x80000000
	flagM    uint32 = 0x40000000
	flagSRID uint32 = 0x20000000
	typeMask uint32 = 0x0fffffff
)

// Marshal encodes geometry as two-dimensional OGC WKB.
func Marshal(geometry geo.Geometry, order binary.ByteOrder) ([]byte, error) {
	if err := validateOrder(order); err != nil {
		return nil, err
	}
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, initialCapacity(owned, false))
	return appendGeometry(result, owned, order.(binary.AppendByteOrder), false), nil
}

// MarshalEWKB encodes geometry as PostGIS EWKB with an SRID on the top-level
// geometry. Child geometries inherit that SRID.
func MarshalEWKB(geometry geo.Geometry, order binary.ByteOrder) ([]byte, error) {
	if err := validateOrder(order); err != nil {
		return nil, err
	}
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, initialCapacity(owned, true))
	return appendGeometry(result, owned, order.(binary.AppendByteOrder), true), nil
}

// Unmarshal decodes bounded OGC WKB using caller-supplied CRS metadata.
func Unmarshal(data []byte, crs geo.CRS, limits geo.Limits) (geo.Geometry, error) {
	limits = geo.ResolveLimits(limits)
	if err := validateInput(data, crs, limits); err != nil {
		return nil, err
	}
	parser := binaryParser{data: data, limits: limits, ewkb: false}
	geometry, _, err := parser.geometry(1, 0, crs, true)
	if err != nil {
		return nil, err
	}
	if parser.position != len(data) {
		return nil, parser.failure("trailing bytes", nil)
	}
	return geometry, nil
}

// UnmarshalEWKB decodes bounded PostGIS EWKB. A positive top-level SRID is
// mandatory; nested SRIDs, when present, must match it.
func UnmarshalEWKB(data []byte, limits geo.Limits) (geo.Geometry, error) {
	limits = geo.ResolveLimits(limits)
	if int64(len(data)) > limits.MaxEncodedBytes {
		return nil, encodingError("encoded byte limit exceeded", nil)
	}
	parser := binaryParser{data: data, limits: limits, ewkb: true}
	geometry, _, err := parser.geometry(1, 0, geo.CRS{}, true)
	if err != nil {
		return nil, err
	}
	if parser.position != len(data) {
		return nil, parser.failure("trailing bytes", nil)
	}
	return geometry, nil
}

type binaryParser struct {
	data     []byte
	position int
	limits   geo.Limits
	ewkb     bool
}

func (parser *binaryParser) geometry(
	depth int,
	expected uint32,
	inherited geo.CRS,
	top bool,
) (geo.Geometry, geo.CRS, error) {
	if depth > parser.limits.MaxCollectionDepth {
		return nil, geo.CRS{}, parser.failure("collection depth limit exceeded", geo.ErrTopology)
	}
	order, err := parser.byteOrder()
	if err != nil {
		return nil, geo.CRS{}, err
	}
	rawType, err := parser.uint32(order)
	if err != nil {
		return nil, geo.CRS{}, err
	}
	if rawType&(flagZ|flagM) != 0 || rawType&typeMask >= 1000 {
		return nil, geo.CRS{}, parser.failure("only two-dimensional WKB is supported", geo.ErrUnsupported)
	}
	hasSRID := rawType&flagSRID != 0
	if !parser.ewkb && hasSRID {
		return nil, geo.CRS{}, parser.failure("WKB must not contain an EWKB SRID", nil)
	}
	if parser.ewkb && top && !hasSRID {
		return nil, geo.CRS{}, parser.failure("EWKB requires a top-level SRID", nil)
	}
	kind := rawType & typeMask
	if expected != 0 && kind != expected {
		return nil, geo.CRS{}, parser.failure("nested geometry has unexpected type", geo.ErrTopology)
	}

	crs := inherited
	if hasSRID {
		srid, readErr := parser.uint32(order)
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		if srid == 0 || srid > math.MaxInt32 {
			return nil, geo.CRS{}, parser.failure("SRID must be a positive 32-bit integer", geo.ErrCRS)
		}
		crs = crsForSRID(int32(srid))
		if inherited.SRID() != 0 && !crs.Equal(inherited) {
			return nil, geo.CRS{}, parser.failure("nested SRID does not match parent", geo.ErrCRS)
		}
	}
	switch kind {
	case typePoint:
		coordinate, readErr := parser.coordinate(order, crs)
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		point, buildErr := geo.NewPoint(coordinate)
		return parser.built(point, crs, buildErr)
	case typeLineString:
		coordinates, readErr := parser.coordinateSequence(order, crs)
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		line, buildErr := geo.NewLineStringWithLimits(coordinates, parser.limits)
		return parser.built(line, crs, buildErr)
	case typePolygon:
		rings, readErr := parser.rings(order, crs)
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		if len(rings) == 0 {
			return nil, geo.CRS{}, parser.failure("empty polygon is unsupported", geo.ErrUnsupported)
		}
		polygon, buildErr := geo.NewPolygonWithLimits(rings[0], rings[1:], parser.limits)
		return parser.built(polygon, crs, buildErr)
	case typeMultiPoint:
		count, readErr := parser.count(order, parser.limits.MaxPoints, 5, "point")
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		coordinates := make([]geo.Coordinate, count)
		for index := range coordinates {
			child, _, childErr := parser.geometry(depth+1, typePoint, crs, false)
			if childErr != nil {
				return nil, geo.CRS{}, childErr
			}
			coordinates[index] = child.(geo.Point).Coordinate()
		}
		multi, buildErr := geo.NewMultiPointWithLimits(coordinates, crs, parser.limits)
		return parser.built(multi, crs, buildErr)
	case typeMultiLineString:
		count, readErr := parser.count(order, parser.limits.MaxGeometries, 5, "geometry")
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		lines := make([]geo.LineString, count)
		for index := range lines {
			child, _, childErr := parser.geometry(depth+1, typeLineString, crs, false)
			if childErr != nil {
				return nil, geo.CRS{}, childErr
			}
			lines[index] = child.(geo.LineString)
		}
		multi, buildErr := geo.NewMultiLineStringWithLimits(lines, crs, parser.limits)
		return parser.built(multi, crs, buildErr)
	case typeMultiPolygon:
		count, readErr := parser.count(order, parser.limits.MaxGeometries, 5, "geometry")
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		polygons := make([]geo.Polygon, count)
		for index := range polygons {
			child, _, childErr := parser.geometry(depth+1, typePolygon, crs, false)
			if childErr != nil {
				return nil, geo.CRS{}, childErr
			}
			polygons[index] = child.(geo.Polygon)
		}
		multi, buildErr := geo.NewMultiPolygonWithLimits(polygons, crs, parser.limits)
		return parser.built(multi, crs, buildErr)
	case typeGeometryCollection:
		count, readErr := parser.count(order, parser.limits.MaxGeometries, 5, "geometry")
		if readErr != nil {
			return nil, geo.CRS{}, readErr
		}
		geometries := make([]geo.Geometry, count)
		for index := range geometries {
			child, _, childErr := parser.geometry(depth+1, 0, crs, false)
			if childErr != nil {
				return nil, geo.CRS{}, childErr
			}
			geometries[index] = child
		}
		collection, buildErr := geo.NewGeometryCollectionWithLimits(geometries, crs, parser.limits)
		return parser.built(collection, crs, buildErr)
	default:
		return nil, geo.CRS{}, parser.failure(fmt.Sprintf("unsupported geometry type %d", kind), geo.ErrUnsupported)
	}
}

func (parser *binaryParser) rings(order binary.ByteOrder, crs geo.CRS) ([][]geo.Coordinate, error) {
	count, err := parser.count(order, parser.limits.MaxRings, 4, "ring")
	if err != nil {
		return nil, err
	}
	rings := make([][]geo.Coordinate, count)
	for index := range rings {
		ring, readErr := parser.coordinateSequence(order, crs)
		if readErr != nil {
			return nil, readErr
		}
		rings[index] = ring
	}
	return rings, nil
}

func (parser *binaryParser) coordinateSequence(
	order binary.ByteOrder,
	crs geo.CRS,
) ([]geo.Coordinate, error) {
	count, err := parser.count(order, parser.limits.MaxPoints, 16, "point")
	if err != nil {
		return nil, err
	}
	coordinates := make([]geo.Coordinate, count)
	for index := range coordinates {
		coordinate, readErr := parser.coordinate(order, crs)
		if readErr != nil {
			return nil, readErr
		}
		coordinates[index] = coordinate
	}
	return coordinates, nil
}

func (parser *binaryParser) coordinate(
	order binary.ByteOrder,
	crs geo.CRS,
) (geo.Coordinate, error) {
	longitude, err := parser.float64(order)
	if err != nil {
		return geo.Coordinate{}, err
	}
	latitude, err := parser.float64(order)
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
	coordinate, _ := geo.NewCoordinate(lon, lat, crs)
	return coordinate, nil
}

func (parser *binaryParser) count(
	order binary.ByteOrder,
	limit int,
	minimumBytes int,
	resource string,
) (int, error) {
	raw, err := parser.uint32(order)
	if err != nil {
		return 0, err
	}
	if uint64(raw) > uint64(limit) {
		return 0, parser.failure(resource+" limit exceeded", geo.ErrTopology)
	}
	remaining := len(parser.data) - parser.position
	if uint64(raw) > uint64(remaining/minimumBytes) {
		return 0, parser.failure(resource+" count exceeds remaining bytes", io.ErrUnexpectedEOF)
	}
	return int(raw), nil
}

func (parser *binaryParser) byteOrder() (binary.ByteOrder, error) {
	value, err := parser.byte()
	if err != nil {
		return nil, err
	}
	switch value {
	case 0:
		return binary.BigEndian, nil
	case 1:
		return binary.LittleEndian, nil
	default:
		return nil, parser.failure("invalid byte order", nil)
	}
}

func (parser *binaryParser) byte() (byte, error) {
	if parser.position >= len(parser.data) {
		return 0, parser.failure("unexpected end of input", io.ErrUnexpectedEOF)
	}
	value := parser.data[parser.position]
	parser.position++
	return value, nil
}

func (parser *binaryParser) uint32(order binary.ByteOrder) (uint32, error) {
	if len(parser.data)-parser.position < 4 {
		return 0, parser.failure("unexpected end of input", io.ErrUnexpectedEOF)
	}
	value := order.Uint32(parser.data[parser.position : parser.position+4])
	parser.position += 4
	return value, nil
}

func (parser *binaryParser) float64(order binary.ByteOrder) (float64, error) {
	if len(parser.data)-parser.position < 8 {
		return 0, parser.failure("unexpected end of input", io.ErrUnexpectedEOF)
	}
	bits := order.Uint64(parser.data[parser.position : parser.position+8])
	parser.position += 8
	return math.Float64frombits(bits), nil
}

func (parser *binaryParser) built(
	geometry geo.Geometry,
	crs geo.CRS,
	err error,
) (geo.Geometry, geo.CRS, error) {
	if err != nil {
		return nil, geo.CRS{}, parser.failure("invalid geometry", err)
	}
	return geometry, crs, nil
}

func (parser *binaryParser) failure(problem string, cause error) *geo.EncodingError {
	return encodingError(fmt.Sprintf("%s at byte %d", problem, parser.position), cause)
}

func appendGeometry(
	result []byte,
	geometry geo.Geometry,
	order binary.AppendByteOrder,
	includeSRID bool,
) []byte {
	result = appendByteOrder(result, order)
	kind := geometryCode(geometry.Type())
	rawType := kind
	if includeSRID {
		rawType |= flagSRID
	}
	result = appendUint32(result, order, rawType)
	if includeSRID {
		result = appendUint32(result, order, uint32(geometry.CRS().SRID()))
	}

	switch value := geometry.(type) {
	case geo.Point:
		result = appendCoordinate(result, order, value.Coordinate())
	case geo.LineString:
		result = appendLineString(result, order, value)
	case geo.Polygon:
		result = appendPolygon(result, order, value)
	case geo.MultiPoint:
		result = appendUint32(result, order, uint32(value.Len()))
		for index := 0; index < value.Len(); index++ {
			coordinate, _ := value.At(index)
			point, _ := geo.NewPoint(coordinate)
			result = appendGeometry(result, point, order, false)
		}
	case geo.MultiLineString:
		result = appendUint32(result, order, uint32(value.Len()))
		for _, line := range value.Lines() {
			result = appendGeometry(result, line, order, false)
		}
	case geo.MultiPolygon:
		result = appendUint32(result, order, uint32(value.Len()))
		for _, polygon := range value.Polygons() {
			result = appendGeometry(result, polygon, order, false)
		}
	case geo.GeometryCollection:
		result = appendUint32(result, order, uint32(value.Len()))
		for _, child := range value.Geometries() {
			result = appendGeometry(result, child, order, false)
		}
	}
	return result
}

func appendLineString(
	result []byte,
	order binary.AppendByteOrder,
	line geo.LineString,
) []byte {
	result = appendUint32(result, order, uint32(line.Len()))
	for index := 0; index < line.Len(); index++ {
		coordinate, _ := line.At(index)
		result = appendCoordinate(result, order, coordinate)
	}
	return result
}

func appendPolygon(result []byte, order binary.AppendByteOrder, polygon geo.Polygon) []byte {
	holes := polygon.Holes()
	result = appendUint32(result, order, uint32(1+len(holes)))
	result = appendCoordinates(result, order, polygon.Exterior())
	for _, hole := range holes {
		result = appendCoordinates(result, order, hole)
	}
	return result
}

func appendCoordinates(
	result []byte,
	order binary.AppendByteOrder,
	coordinates []geo.Coordinate,
) []byte {
	result = appendUint32(result, order, uint32(len(coordinates)))
	for _, coordinate := range coordinates {
		result = appendCoordinate(result, order, coordinate)
	}
	return result
}

func appendCoordinate(
	result []byte,
	order binary.AppendByteOrder,
	coordinate geo.Coordinate,
) []byte {
	result = appendFloat64(result, order, coordinate.Longitude().Degrees())
	return appendFloat64(result, order, coordinate.Latitude().Degrees())
}

func appendByteOrder(result []byte, order binary.AppendByteOrder) []byte {
	if order == binary.LittleEndian {
		return append(result, 1)
	}
	return append(result, 0)
}

func appendUint32(result []byte, order binary.AppendByteOrder, value uint32) []byte {
	return order.AppendUint32(result, value)
}

func appendFloat64(result []byte, order binary.AppendByteOrder, value float64) []byte {
	return order.AppendUint64(result, math.Float64bits(value))
}

func initialCapacity(geometry geo.Geometry, includeSRID bool) int {
	header := 5
	if includeSRID {
		header += 4
	}
	switch value := geometry.(type) {
	case geo.Point:
		return header + 16
	case geo.LineString:
		if value.Len() <= (math.MaxInt-header-4)/16 {
			return header + 4 + value.Len()*16
		}
	}
	return 0
}

func geometryCode(kind geo.GeometryType) uint32 {
	code := uint32(0)
	switch kind {
	case geo.TypePoint:
		code = typePoint
	case geo.TypeLineString:
		code = typeLineString
	case geo.TypePolygon:
		code = typePolygon
	case geo.TypeMultiPoint:
		code = typeMultiPoint
	case geo.TypeMultiLineString:
		code = typeMultiLineString
	case geo.TypeMultiPolygon:
		code = typeMultiPolygon
	case geo.TypeGeometryCollection:
		code = typeGeometryCollection
	}
	return code
}

func validateOrder(order binary.ByteOrder) error {
	if order != binary.LittleEndian && order != binary.BigEndian {
		return &geo.UnsupportedError{
			Operation: "WKB byte order",
			Reason:    "only binary.LittleEndian and binary.BigEndian are supported",
		}
	}
	return nil
}

func validateInput(data []byte, crs geo.CRS, limits geo.Limits) error {
	if !crsValid(crs) {
		return &geo.CRSError{SRID: crs.SRID(), Problem: "WKB requires explicit CRS metadata"}
	}
	if int64(len(data)) > limits.MaxEncodedBytes {
		return encodingError("encoded byte limit exceeded", nil)
	}
	return nil
}

func crsForSRID(srid int32) geo.CRS {
	if srid == 4326 {
		return geo.WGS84()
	}
	crs, _ := geo.NewCRS(srid, fmt.Sprintf("EPSG:%d", srid))
	return crs
}

func crsValid(crs geo.CRS) bool { return crs.SRID() > 0 && crs.Name() != "" }

func encodingError(problem string, cause error) *geo.EncodingError {
	return &geo.EncodingError{Format: "WKB", Problem: problem, Cause: cause}
}

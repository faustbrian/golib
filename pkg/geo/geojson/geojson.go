// Package geojson provides bounded RFC 7946 geometry and Feature codecs.
// Callers must pass EPSG:4326 explicitly; no CRS transformation is performed.
package geojson

import (
	"bytes"
	"encoding/json"

	geo "github.com/faustbrian/golib/pkg/geo"
)

type geometryWire struct {
	Type        string            `json:"type"`
	Coordinates json.RawMessage   `json:"coordinates,omitempty"`
	Geometries  []json.RawMessage `json:"geometries,omitempty"`
}

// Feature is an immutable GeoJSON Feature. Properties retain their JSON
// representation without coercing numbers through float64.
type Feature struct {
	geometry   geo.Geometry
	properties map[string]json.RawMessage
	id         json.RawMessage
}

// NewFeature validates and owns an optional geometry, properties, and JSON ID.
func NewFeature(
	geometry geo.Geometry,
	properties map[string]json.RawMessage,
	id json.RawMessage,
) (Feature, error) {
	owned, err := cloneOptionalGeometry(geometry)
	if err != nil {
		return Feature{}, err
	}
	if err := validateID(id); err != nil {
		return Feature{}, err
	}
	for name, value := range properties {
		if !json.Valid(value) {
			return Feature{}, encodingError("property "+name+" is not valid JSON", nil)
		}
	}

	return Feature{
		geometry:   owned,
		properties: cloneProperties(properties),
		id:         bytes.Clone(id),
	}, nil
}

// Geometry returns an owned geometry, or nil for a null geometry.
func (feature Feature) Geometry() geo.Geometry {
	geometry, _ := cloneOptionalGeometry(feature.geometry)
	return geometry
}

// Properties returns an owned map with owned raw JSON values.
func (feature Feature) Properties() map[string]json.RawMessage {
	return cloneProperties(feature.properties)
}

// ID returns an owned raw JSON ID, or nil when absent.
func (feature Feature) ID() json.RawMessage { return bytes.Clone(feature.id) }

// Marshal encodes geometry with stable longitude-latitude coordinate order.
func Marshal(geometry geo.Geometry) ([]byte, error) {
	owned, err := geo.CloneGeometry(geometry)
	if err != nil {
		return nil, err
	}
	if !owned.CRS().Equal(geo.WGS84()) {
		return nil, &geo.CRSError{
			SRID:    owned.CRS().SRID(),
			Problem: "GeoJSON requires EPSG:4326",
		}
	}

	return json.Marshal(marshalValue(owned))
}

// Unmarshal decodes one bounded GeoJSON geometry using an explicit CRS.
func Unmarshal(data []byte, crs geo.CRS, limits geo.Limits) (geo.Geometry, error) {
	limits = geo.ResolveLimits(limits)
	if err := validateInput(data, crs, limits); err != nil {
		return nil, err
	}

	return unmarshalGeometry(data, crs, limits, 1)
}

// MarshalFeature encodes a Feature with deterministic member order.
func MarshalFeature(feature Feature) ([]byte, error) {
	geometry := json.RawMessage("null")
	if feature.geometry != nil {
		encoded, _ := Marshal(feature.geometry)
		geometry = encoded
	}

	return json.Marshal(struct {
		Type       string                     `json:"type"`
		ID         json.RawMessage            `json:"id,omitempty"`
		Geometry   json.RawMessage            `json:"geometry"`
		Properties map[string]json.RawMessage `json:"properties"`
	}{
		Type:       "Feature",
		ID:         feature.id,
		Geometry:   geometry,
		Properties: feature.properties,
	})
}

// UnmarshalFeature decodes one bounded GeoJSON Feature.
func UnmarshalFeature(data []byte, crs geo.CRS, limits geo.Limits) (Feature, error) {
	limits = geo.ResolveLimits(limits)
	if err := validateInput(data, crs, limits); err != nil {
		return Feature{}, err
	}
	var wire struct {
		Type       string          `json:"type"`
		ID         json.RawMessage `json:"id"`
		Geometry   json.RawMessage `json:"geometry"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return Feature{}, encodingError("malformed JSON", err)
	}
	if wire.Type != "Feature" {
		return Feature{}, encodingError("type must be Feature", nil)
	}
	var geometry geo.Geometry
	var err error
	if len(wire.Geometry) == 0 {
		return Feature{}, encodingError("geometry member is required", nil)
	}
	if !bytes.Equal(bytes.TrimSpace(wire.Geometry), []byte("null")) {
		geometry, err = unmarshalGeometry(wire.Geometry, crs, limits, 1)
		if err != nil {
			return Feature{}, err
		}
	}
	var properties map[string]json.RawMessage
	if len(wire.Properties) == 0 {
		return Feature{}, encodingError("properties member is required", nil)
	}
	if !bytes.Equal(bytes.TrimSpace(wire.Properties), []byte("null")) {
		if err := json.Unmarshal(wire.Properties, &properties); err != nil {
			return Feature{}, encodingError("properties must be an object or null", err)
		}
	}

	return NewFeature(geometry, properties, wire.ID)
}

func unmarshalGeometry(data []byte, crs geo.CRS, limits geo.Limits, depth int) (geo.Geometry, error) {
	if depth > limits.MaxCollectionDepth {
		return nil, encodingError("collection depth limit exceeded", geo.ErrTopology)
	}
	var wire geometryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, encodingError("malformed geometry JSON", err)
	}
	switch geo.GeometryType(wire.Type) {
	case geo.TypePoint:
		position, err := decodePosition(wire.Coordinates, crs)
		if err != nil {
			return nil, err
		}
		return wrapGeometry(geo.NewPoint(position))
	case geo.TypeLineString:
		var positions [][]float64
		if err := decodeCoordinates(wire.Coordinates, &positions); err != nil {
			return nil, err
		}
		coordinates, err := makeCoordinates(positions, crs)
		if err != nil {
			return nil, err
		}
		return wrapGeometry(geo.NewLineStringWithLimits(coordinates, limits))
	case geo.TypePolygon:
		var positions [][][]float64
		if err := decodeCoordinates(wire.Coordinates, &positions); err != nil {
			return nil, err
		}
		if len(positions) == 0 {
			return nil, encodingError("polygon requires an exterior ring", geo.ErrTopology)
		}
		rings, err := makeRings(positions, crs)
		if err != nil {
			return nil, err
		}
		return wrapGeometry(geo.NewPolygonWithLimits(rings[0], rings[1:], limits))
	case geo.TypeMultiPoint:
		var positions [][]float64
		if err := decodeCoordinates(wire.Coordinates, &positions); err != nil {
			return nil, err
		}
		coordinates, err := makeCoordinates(positions, crs)
		if err != nil {
			return nil, err
		}
		return wrapGeometry(geo.NewMultiPointWithLimits(coordinates, crs, limits))
	case geo.TypeMultiLineString:
		var positions [][][]float64
		if err := decodeCoordinates(wire.Coordinates, &positions); err != nil {
			return nil, err
		}
		lines := make([]geo.LineString, len(positions))
		for index, rawLine := range positions {
			coordinates, err := makeCoordinates(rawLine, crs)
			if err != nil {
				return nil, err
			}
			line, err := geo.NewLineStringWithLimits(coordinates, limits)
			if err != nil {
				return nil, encodingError("invalid line string", err)
			}
			lines[index] = line
		}
		return wrapGeometry(geo.NewMultiLineStringWithLimits(lines, crs, limits))
	case geo.TypeMultiPolygon:
		var positions [][][][]float64
		if err := decodeCoordinates(wire.Coordinates, &positions); err != nil {
			return nil, err
		}
		polygons := make([]geo.Polygon, len(positions))
		for index, rawPolygon := range positions {
			if len(rawPolygon) == 0 {
				return nil, encodingError("polygon requires an exterior ring", geo.ErrTopology)
			}
			rings, err := makeRings(rawPolygon, crs)
			if err != nil {
				return nil, err
			}
			polygon, err := geo.NewPolygonWithLimits(rings[0], rings[1:], limits)
			if err != nil {
				return nil, encodingError("invalid polygon", err)
			}
			polygons[index] = polygon
		}
		return wrapGeometry(geo.NewMultiPolygonWithLimits(polygons, crs, limits))
	case geo.TypeGeometryCollection:
		geometries := make([]geo.Geometry, len(wire.Geometries))
		for index, rawGeometry := range wire.Geometries {
			geometry, err := unmarshalGeometry(rawGeometry, crs, limits, depth+1)
			if err != nil {
				return nil, err
			}
			geometries[index] = geometry
		}
		return wrapGeometry(geo.NewGeometryCollectionWithLimits(geometries, crs, limits))
	default:
		return nil, encodingError("unsupported geometry type "+wire.Type, geo.ErrUnsupported)
	}
}

func marshalValue(geometry geo.Geometry) any {
	var result any
	switch value := geometry.(type) {
	case geo.Point:
		result = coordinateWire(geo.TypePoint, position(value.Coordinate()))
	case geo.LineString:
		result = coordinateWire(geo.TypeLineString, positions(value.Coordinates()))
	case geo.Polygon:
		result = coordinateWire(geo.TypePolygon, polygonPositions(value))
	case geo.MultiPoint:
		result = coordinateWire(geo.TypeMultiPoint, positions(value.Coordinates()))
	case geo.MultiLineString:
		lines := make([][][]float64, value.Len())
		for index, line := range value.Lines() {
			lines[index] = positions(line.Coordinates())
		}
		result = coordinateWire(geo.TypeMultiLineString, lines)
	case geo.MultiPolygon:
		polygons := make([][][][]float64, value.Len())
		for index, polygon := range value.Polygons() {
			polygons[index] = polygonPositions(polygon)
		}
		result = coordinateWire(geo.TypeMultiPolygon, polygons)
	case geo.GeometryCollection:
		geometries := make([]any, value.Len())
		for index, child := range value.Geometries() {
			geometries[index] = marshalValue(child)
		}
		result = struct {
			Type       string `json:"type"`
			Geometries []any  `json:"geometries"`
		}{Type: string(geo.TypeGeometryCollection), Geometries: geometries}
	}
	return result
}

func coordinateWire(kind geo.GeometryType, coordinates any) any {
	return struct {
		Type        string `json:"type"`
		Coordinates any    `json:"coordinates"`
	}{Type: string(kind), Coordinates: coordinates}
}

func position(coordinate geo.Coordinate) []float64 {
	return []float64{coordinate.Longitude().Degrees(), coordinate.Latitude().Degrees()}
}

func positions(coordinates []geo.Coordinate) [][]float64 {
	result := make([][]float64, len(coordinates))
	for index, coordinate := range coordinates {
		result[index] = position(coordinate)
	}
	return result
}

func polygonPositions(polygon geo.Polygon) [][][]float64 {
	result := make([][][]float64, 1+len(polygon.Holes()))
	result[0] = positions(polygon.Exterior())
	for index, hole := range polygon.Holes() {
		result[index+1] = positions(hole)
	}
	return result
}

func decodePosition(raw json.RawMessage, crs geo.CRS) (geo.Coordinate, error) {
	var values []float64
	if err := decodeCoordinates(raw, &values); err != nil {
		return geo.Coordinate{}, err
	}
	if len(values) != 2 {
		return geo.Coordinate{}, encodingError("position must contain exactly longitude and latitude", geo.ErrUnsupported)
	}
	return makeCoordinate(values, crs)
}

func decodeCoordinates(raw json.RawMessage, destination any) error {
	if len(raw) == 0 {
		return encodingError("coordinates member is required", nil)
	}
	if err := json.Unmarshal(raw, destination); err != nil {
		return encodingError("malformed coordinates", err)
	}
	return nil
}

func makeCoordinate(values []float64, crs geo.CRS) (geo.Coordinate, error) {
	if len(values) != 2 {
		return geo.Coordinate{}, encodingError("position must contain exactly longitude and latitude", geo.ErrUnsupported)
	}
	lon, err := geo.NewLongitude(values[0])
	if err != nil {
		return geo.Coordinate{}, encodingError("invalid longitude", err)
	}
	lat, err := geo.NewLatitude(values[1])
	if err != nil {
		return geo.Coordinate{}, encodingError("invalid latitude", err)
	}
	coordinate, _ := geo.NewCoordinate(lon, lat, crs)
	return coordinate, nil
}

func makeCoordinates(values [][]float64, crs geo.CRS) ([]geo.Coordinate, error) {
	result := make([]geo.Coordinate, len(values))
	for index, value := range values {
		coordinate, err := makeCoordinate(value, crs)
		if err != nil {
			return nil, err
		}
		result[index] = coordinate
	}
	return result, nil
}

func makeRings(values [][][]float64, crs geo.CRS) ([][]geo.Coordinate, error) {
	result := make([][]geo.Coordinate, len(values))
	for index, value := range values {
		ring, err := makeCoordinates(value, crs)
		if err != nil {
			return nil, err
		}
		result[index] = ring
	}
	return result, nil
}

func wrapGeometry[T geo.Geometry](geometry T, err error) (geo.Geometry, error) {
	if err != nil {
		return nil, encodingError("invalid geometry", err)
	}
	return geometry, nil
}

func validateInput(data []byte, crs geo.CRS, limits geo.Limits) error {
	if !crs.Equal(geo.WGS84()) {
		return &geo.CRSError{SRID: crs.SRID(), Problem: "GeoJSON requires explicit EPSG:4326"}
	}
	if int64(len(data)) > limits.MaxEncodedBytes {
		return encodingError("encoded byte limit exceeded", nil)
	}
	if !json.Valid(data) {
		return encodingError("malformed JSON", nil)
	}
	return nil
}

func validateID(id json.RawMessage) error {
	if len(id) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(id))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return encodingError("feature id is not valid JSON", err)
	}
	switch value.(type) {
	case string, json.Number:
		return nil
	default:
		return encodingError("feature id must be a string or number", nil)
	}
}

func cloneOptionalGeometry(geometry geo.Geometry) (geo.Geometry, error) {
	if geometry == nil {
		return nil, nil
	}
	return geo.CloneGeometry(geometry)
}

func cloneProperties(properties map[string]json.RawMessage) map[string]json.RawMessage {
	if properties == nil {
		return nil
	}
	result := make(map[string]json.RawMessage, len(properties))
	for name, value := range properties {
		result[name] = bytes.Clone(value)
	}
	return result
}

func encodingError(problem string, cause error) *geo.EncodingError {
	return &geo.EncodingError{Format: "GeoJSON", Problem: problem, Cause: cause}
}

// Package gogeom converts between geo and github.com/twpayne/go-geom.
// Conversion preserves two-dimensional SRID metadata and performs no CRS
// transformation.
package gogeom

import (
	"encoding/binary"
	"reflect"

	"github.com/twpayne/go-geom"
	geomwkb "github.com/twpayne/go-geom/encoding/ewkb"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkb"
)

// ToGoGeom converts an owned geo geometry through canonical EWKB. It is
// O(n) in geometry size and allocates O(n).
func ToGoGeom(geometry geo.Geometry) (geom.T, error) {
	if geometry == nil {
		return nil, adapterError("cannot convert nil geometry", nil)
	}
	encoded, err := wkb.MarshalEWKB(geometry, binary.LittleEndian)
	if err != nil {
		return nil, err
	}
	converted, _ := geomwkb.Unmarshal(encoded)
	return converted, nil
}

// FromGoGeom converts a two-dimensional geom value with a positive SRID.
// The supplied limits bound coordinates and the canonical EWKB result. It is
// O(n) in geometry size and allocates O(n).
func FromGoGeom(value geom.T, limits geo.Limits) (geo.Geometry, error) {
	if nilGeometry(value) {
		return nil, adapterError("cannot convert nil geometry", nil)
	}
	if value.Layout() != geom.XY {
		return nil, &geo.UnsupportedError{
			Operation: "geom conversion",
			Reason:    "only the two-dimensional XY layout is supported",
		}
	}
	if value.SRID() <= 0 {
		return nil, &geo.CRSError{
			SRID:    int32(value.SRID()),
			Problem: "geom conversion requires a positive SRID",
		}
	}
	limits = geo.ResolveLimits(limits)
	coordinates := value.FlatCoords()
	if len(coordinates)%2 != 0 {
		return nil, adapterError("geom has malformed XY coordinates", nil)
	}
	if len(coordinates)/2 > limits.MaxPoints {
		return nil, &geo.TopologyError{
			Geometry: "geom",
			Problem:  "point limit exceeded",
		}
	}

	encoded, err := geomwkb.Marshal(value, binary.LittleEndian)
	if err != nil {
		return nil, adapterError("cannot encode geom EWKB", err)
	}
	if int64(len(encoded)) > limits.MaxEncodedBytes {
		return nil, adapterError("encoded byte limit exceeded", nil)
	}
	return wkb.UnmarshalEWKB(encoded, limits)
}

func nilGeometry(value geom.T) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

func adapterError(problem string, cause error) error {
	return &geo.EncodingError{
		Format:  "geom adapter",
		Problem: problem,
		Cause:   cause,
	}
}

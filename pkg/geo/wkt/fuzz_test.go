package wkt_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func FuzzDecode(f *testing.F) {
	f.Add([]byte("POINT (24.9384 60.1699)"))
	f.Add([]byte("SRID=4326;POINT (24.9384 60.1699)"))
	f.Add([]byte("GEOMETRYCOLLECTION ("))

	limits := geo.DefaultLimits()
	limits.MaxPoints = 64
	limits.MaxRings = 16
	limits.MaxGeometries = 32
	limits.MaxCollectionDepth = 8
	limits.MaxEncodedBytes = 4096
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = wkt.Unmarshal(data, geo.WGS84(), limits)
		_, _ = wkt.UnmarshalEWKT(data, limits)
	})
}

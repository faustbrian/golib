package wkb_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkb"
)

func FuzzDecode(f *testing.F) {
	f.Add([]byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{1, 1, 0, 0, 32, 230, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{1, 7, 0, 0, 0, 255, 255, 255, 255})

	limits := geo.DefaultLimits()
	limits.MaxPoints = 64
	limits.MaxRings = 16
	limits.MaxGeometries = 32
	limits.MaxCollectionDepth = 8
	limits.MaxEncodedBytes = 4096
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = wkb.Unmarshal(data, geo.WGS84(), limits)
		_, _ = wkb.UnmarshalEWKB(data, limits)
	})
}

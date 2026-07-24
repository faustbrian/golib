package postgis_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/postgis"
)

func FuzzValueScan(f *testing.F) {
	f.Add([]byte{1, 1, 0, 0, 32, 230, 16, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte("0101000020e610000000000000000000000000000000000000"))
	f.Add([]byte{})

	f.Fuzz(func(_ *testing.T, data []byte) {
		limits := geo.DefaultLimits()
		limits.MaxPoints = 64
		limits.MaxRings = 16
		limits.MaxGeometries = 32
		limits.MaxCollectionDepth = 8
		limits.MaxEncodedBytes = 4096
		value, _ := postgis.NewValue(nil, limits)
		_ = value.Scan(data)
		_ = value.Scan(string(data))
	})
}

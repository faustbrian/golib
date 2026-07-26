package gogeom_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/twpayne/go-geom"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/adapter/gogeom"
)

func FuzzFromGoGeom(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8})

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 256 {
			data = data[:256]
		}
		flat := make([]float64, 0, len(data)/8)
		for len(data) >= 8 {
			flat = append(flat, math.Float64frombits(binary.LittleEndian.Uint64(data[:8])))
			data = data[8:]
		}
		line := geom.NewLineStringFlat(geom.XY, flat).SetSRID(4326)
		converted, err := gogeom.FromGoGeom(line, fuzzAdapterLimits())
		if err == nil {
			_, _ = gogeom.ToGoGeom(converted)
		}
	})
}

func fuzzAdapterLimits() geo.Limits {
	limits := geo.DefaultLimits()
	limits.MaxPoints = 32
	limits.MaxEncodedBytes = 4096
	return limits
}

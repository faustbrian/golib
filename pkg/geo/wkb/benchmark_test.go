package wkb_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/wkb"
)

func BenchmarkLineStringEWKB(b *testing.B) {
	for _, points := range []int{10, 1_000, 100_000} {
		b.Run(fmt.Sprintf("points=%d", points), func(b *testing.B) {
			line := benchmarkLine(b, points)
			encoded, err := wkb.MarshalEWKB(line, binary.LittleEndian)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("marshal", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(encoded)))
				for b.Loop() {
					if _, marshalErr := wkb.MarshalEWKB(
						line,
						binary.LittleEndian,
					); marshalErr != nil {
						b.Fatal(marshalErr)
					}
				}
			})
			b.Run("unmarshal", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(encoded)))
				for b.Loop() {
					if _, unmarshalErr := wkb.UnmarshalEWKB(
						encoded,
						geo.DefaultLimits(),
					); unmarshalErr != nil {
						b.Fatal(unmarshalErr)
					}
				}
			})
		})
	}
}

func TestLineStringEWKBMarshalAllocationBudget(t *testing.T) {
	line := benchmarkLine(t, 1_000)
	allocations := testing.AllocsPerRun(20, func() {
		if _, err := wkb.MarshalEWKB(line, binary.LittleEndian); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 3 {
		t.Fatalf("MarshalEWKB() allocations = %.1f, budget is 3", allocations)
	}
}

func benchmarkLine(test testing.TB, points int) geo.LineString {
	test.Helper()
	coordinates := make([]geo.Coordinate, points)
	for index := range coordinates {
		longitude, err := geo.NewLongitude(float64(index%360) - 180)
		if err != nil {
			test.Fatal(err)
		}
		latitude, err := geo.NewLatitude(float64(index%180) - 90)
		if err != nil {
			test.Fatal(err)
		}
		coordinates[index], err = geo.NewCoordinate(
			longitude,
			latitude,
			geo.WGS84(),
		)
		if err != nil {
			test.Fatal(err)
		}
	}
	line, err := geo.NewLineString(coordinates)
	if err != nil {
		test.Fatal(err)
	}
	return line
}

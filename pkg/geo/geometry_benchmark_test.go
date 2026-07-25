package geo_test

import (
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func BenchmarkPolygonValidation(b *testing.B) {
	exterior := benchmarkRing(1_000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := geo.NewPolygon(exterior, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPolygonValidationAllocationBudget(t *testing.T) {
	exterior := benchmarkRing(1_000, 10)
	allocations := testing.AllocsPerRun(20, func() {
		if _, err := geo.NewPolygon(exterior, nil); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 400 {
		t.Fatalf("NewPolygon() allocations = %.1f, budget is 400", allocations)
	}
}

func benchmarkRing(points int, radius float64) []geo.Coordinate {
	ring := make([]geo.Coordinate, points+1)
	for index := range points {
		angle := float64(index) * 2 * 3.141592653589793 / float64(points)
		longitude := radius * math.Cos(angle)
		latitude := radius * math.Sin(angle)
		ring[index] = propertyCoordinateDegrees(longitude, latitude)
	}
	ring[points] = ring[0]
	return ring
}

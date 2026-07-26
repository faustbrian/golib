package gogeom_test

import (
	"testing"

	"github.com/twpayne/go-geom"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/adapter/gogeom"
)

func BenchmarkLineStringConversion(b *testing.B) {
	flat := make([]float64, 2_000)
	for index := 0; index < len(flat); index += 2 {
		flat[index] = float64(index/2) / 10
		flat[index+1] = float64(index/2) / 20
	}
	value := geom.NewLineStringFlat(geom.XY, flat).SetSRID(4326)
	limits := geo.DefaultLimits()

	b.Run("from geom", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := gogeom.FromGoGeom(value, limits); err != nil {
				b.Fatal(err)
			}
		}
	})

	converted, err := gogeom.FromGoGeom(value, limits)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("to geom", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := gogeom.ToGoGeom(converted); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestLineStringConversionAllocationBudgets(t *testing.T) {
	flat := make([]float64, 2_000)
	for index := 0; index < len(flat); index += 2 {
		flat[index] = float64(index/2) / 10
		flat[index+1] = float64(index/2) / 20
	}
	value := geom.NewLineStringFlat(geom.XY, flat).SetSRID(4326)
	limits := geo.DefaultLimits()
	var converted geo.Geometry
	fromAllocations := testing.AllocsPerRun(20, func() {
		var err error
		converted, err = gogeom.FromGoGeom(value, limits)
		if err != nil {
			t.Fatal(err)
		}
	})
	if fromAllocations > 16 {
		t.Fatalf(
			"FromGoGeom() allocations = %.1f, budget is 16",
			fromAllocations,
		)
	}
	toAllocations := testing.AllocsPerRun(20, func() {
		if _, err := gogeom.ToGoGeom(converted); err != nil {
			t.Fatal(err)
		}
	})
	if toAllocations > 16 {
		t.Fatalf("ToGoGeom() allocations = %.1f, budget is 16", toAllocations)
	}
}

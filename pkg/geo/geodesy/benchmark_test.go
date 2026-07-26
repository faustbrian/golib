package geodesy_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
)

func BenchmarkInverse(b *testing.B) {
	from := benchmarkCoordinate(b, 24.9384, 60.1699)
	to := benchmarkCoordinate(b, -5.5, 40.96)
	models := []geodesy.Model{
		geodesy.MeanEarthSphere(),
		geodesy.WGS84Ellipsoid(),
	}
	for _, model := range models {
		b.Run(model.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := model.Inverse(from, to); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkCoordinate(b *testing.B, longitude, latitude float64) geo.Coordinate {
	b.Helper()
	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		b.Fatal(err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		b.Fatal(err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		b.Fatal(err)
	}
	return coordinate
}

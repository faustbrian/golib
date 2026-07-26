package geodesy_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
)

func FuzzModels(f *testing.F) {
	f.Add(uint16(0), uint16(0), uint16(65535), uint16(65535))
	f.Add(uint16(32768), uint16(32768), uint16(49152), uint16(16384))

	f.Fuzz(func(t *testing.T, lonA, latA, lonB, latB uint16) {
		first := fuzzCoordinate(lonA, latA)
		second := fuzzCoordinate(lonB, latB)
		for _, model := range []geodesy.Model{
			geodesy.MeanEarthSphere(),
			geodesy.WGS84Ellipsoid(),
		} {
			inverse, err := model.Inverse(first, second)
			if err != nil || !inverse.BearingsDefined() {
				continue
			}
			_, _, _ = model.Destination(
				first,
				inverse.InitialBearing(),
				inverse.Distance(),
			)
		}
	})
}

func fuzzCoordinate(longitude, latitude uint16) geo.Coordinate {
	lon, _ := geo.NewLongitude(float64(longitude)*360/65535 - 180)
	lat, _ := geo.NewLatitude(float64(latitude)*180/65535 - 90)
	coordinate, _ := geo.NewCoordinate(lon, lat, geo.WGS84())
	return coordinate
}

package geo_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func FuzzGeometryConstructors(f *testing.F) {
	f.Add([]byte{0, 0, 127, 127, 255, 255})
	f.Add([]byte{128, 64, 32, 16})

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1_024 {
			data = data[:1_024]
		}
		limits := geo.DefaultLimits()
		limits.MaxPoints = 32
		limits.MaxRings = 4
		limits.MaxGeometries = 8
		limits.MaxCollectionDepth = 4
		limits.MaxEncodedBytes = 4_096
		coordinates := make([]geo.Coordinate, 0, len(data)/2+1)
		for index := 0; index+1 < len(data); index += 2 {
			longitude, _ := geo.NewLongitude(float64(data[index])*360/255 - 180)
			latitude, _ := geo.NewLatitude(float64(data[index+1])*180/255 - 90)
			coordinate, _ := geo.NewCoordinate(longitude, latitude, geo.WGS84())
			coordinates = append(coordinates, coordinate)
		}
		if len(coordinates) == 0 {
			longitude, _ := geo.NewLongitude(0)
			latitude, _ := geo.NewLatitude(0)
			coordinate, _ := geo.NewCoordinate(longitude, latitude, geo.WGS84())
			coordinates = append(coordinates, coordinate)
		}

		point, _ := geo.NewPoint(coordinates[0])
		line, lineErr := geo.NewLineStringWithLimits(coordinates, limits)
		ring := append(append([]geo.Coordinate(nil), coordinates...), coordinates[0])
		polygon, polygonErr := geo.NewPolygonWithLimits(ring, nil, limits)
		reversed := append([]geo.Coordinate(nil), ring...)
		for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
			reversed[left], reversed[right] = reversed[right], reversed[left]
		}
		_, _ = geo.NewPolygonWithLimits(reversed, [][]geo.Coordinate{ring}, limits)
		_, _ = geo.NewMultiPointWithLimits(coordinates, geo.WGS84(), limits)
		if lineErr == nil {
			_, _ = geo.NewMultiLineStringWithLimits(
				[]geo.LineString{line},
				geo.WGS84(),
				limits,
			)
		}
		if polygonErr == nil {
			_, _ = geo.NewMultiPolygonWithLimits(
				[]geo.Polygon{polygon},
				geo.WGS84(),
				limits,
			)
		}

		var nested geo.Geometry = point
		depth := 0
		if len(data) > 0 {
			depth = int(data[0]%8) + 1
		}
		for range depth {
			collection, err := geo.NewGeometryCollectionWithLimits(
				[]geo.Geometry{nested},
				geo.WGS84(),
				limits,
			)
			if err != nil {
				break
			}
			nested = collection
		}
	})
}

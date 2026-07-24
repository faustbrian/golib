package main

import (
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
)

func main() {
	origin := coordinate(24.9384, 60.1699)
	east := coordinate(25.0, 60.1699)
	north := coordinate(24.9384, 60.25)
	line, err := geo.NewLineString([]geo.Coordinate{origin, east, north})
	if err != nil {
		panic(err)
	}
	model := geodesy.WGS84Ellipsoid()
	length, err := geodesy.LineLength(model, line)
	if err != nil {
		panic(err)
	}
	ranked, err := geodesy.Nearest(model, origin, []geo.Coordinate{north, east}, 1)
	if err != nil {
		panic(err)
	}
	inverse, err := model.Inverse(origin, ranked[0].Coordinate())
	if err != nil {
		panic(err)
	}
	destination, final, err := model.Destination(
		origin,
		inverse.InitialBearing(),
		inverse.Distance(),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("line: %.0f m, nearest: %.0f m, destination: %v, final: %.2f\n",
		length.Meters(), ranked[0].Distance().Meters(), destination, final.Degrees())
}

func coordinate(longitude, latitude float64) geo.Coordinate {
	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		panic(err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		panic(err)
	}
	value, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		panic(err)
	}
	return value
}

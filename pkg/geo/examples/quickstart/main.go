package main

import (
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
)

func main() {
	helsinki := coordinate(24.9384, 60.1699)
	newYork := coordinate(-73.9857, 40.7484)

	result, err := geodesy.WGS84Ellipsoid().Inverse(helsinki, newYork)
	if err != nil {
		panic(err)
	}
	fmt.Printf("distance: %.0f m, initial bearing: %.2f degrees\n",
		result.Distance().Meters(), result.InitialBearing().Degrees())

	radius, err := geo.NewDistanceMeters(10_000)
	if err != nil {
		panic(err)
	}
	bounds, err := geodesy.MeanEarthSphere().RadiusEnvelope(helsinki, radius)
	if err != nil {
		panic(err)
	}
	fmt.Printf("crosses antimeridian: %t\n", bounds.CrossesAntimeridian())
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
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		panic(err)
	}
	return coordinate
}

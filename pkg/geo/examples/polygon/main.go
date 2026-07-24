package main

import (
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func main() {
	exterior := ring([][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}, {0, 0}})
	hole := ring([][2]float64{{3, 3}, {3, 7}, {7, 7}, {7, 3}, {3, 3}})
	polygon, err := geo.NewPolygon(exterior, [][]geo.Coordinate{hole})
	if err != nil {
		panic(err)
	}

	for name, point := range map[string]geo.Coordinate{
		"surface":  coordinate(1, 1),
		"hole":     coordinate(5, 5),
		"boundary": coordinate(3, 5),
	} {
		location, err := polygon.Locate(point)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%s: %v\n", name, location)
	}
}

func ring(points [][2]float64) []geo.Coordinate {
	result := make([]geo.Coordinate, len(points))
	for index, point := range points {
		result[index] = coordinate(point[0], point[1])
	}
	return result
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

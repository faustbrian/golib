package main

import (
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geohash"
)

func main() {
	coordinate := mustCoordinate(24.9384, 60.1699)
	hash, err := geohash.Encode(coordinate, 7)
	if err != nil {
		panic(err)
	}
	cell, err := geohash.Decode(hash)
	if err != nil {
		panic(err)
	}
	neighbors, err := geohash.Neighbors(hash)
	if err != nil {
		panic(err)
	}
	fmt.Printf("hash: %s, center: %v, neighbors: %v\n",
		cell.Hash(), cell.Center(), neighbors.All())
}

func mustCoordinate(longitude, latitude float64) geo.Coordinate {
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

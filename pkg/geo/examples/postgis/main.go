package main

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/postgis"
)

func main() {
	// Query these OIDs from each live PostGIS connection before registering.
	typeMap := pgtype.NewMap()
	postgis.Register(typeMap, 1234, geo.DefaultLimits()) // geometry
	postgis.Register(typeMap, 1235, geo.DefaultLimits()) // geography

	column, err := postgis.NewColumn("locations.coordinate")
	if err != nil {
		panic(err)
	}
	point := mustPoint(24.9384, 60.1699)
	radius, err := geo.NewDistanceMeters(5_000)
	if err != nil {
		panic(err)
	}
	fragment, err := postgis.GeographyDWithin(column, point, radius, 1)
	if err != nil {
		panic(err)
	}

	query := "SELECT id FROM locations WHERE " + fragment.SQL()
	fmt.Printf("query: %s\nbound arguments: %d\n", query, len(fragment.Args()))
}

func mustPoint(longitude, latitude float64) geo.Point {
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
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		panic(err)
	}
	return point
}

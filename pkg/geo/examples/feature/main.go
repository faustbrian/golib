package main

import (
	"encoding/json"
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geojson"
)

func main() {
	point, err := geo.NewPoint(coordinate(24.9384, 60.1699))
	if err != nil {
		panic(err)
	}
	feature, err := geojson.NewFeature(
		point,
		map[string]json.RawMessage{
			"name": json.RawMessage(`"Helsinki"`),
		},
		json.RawMessage(`"capital"`),
	)
	if err != nil {
		panic(err)
	}
	encoded, err := geojson.MarshalFeature(feature)
	if err != nil {
		panic(err)
	}
	decoded, err := geojson.UnmarshalFeature(encoded, geo.WGS84(), geo.DefaultLimits())
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s\nproperty count: %d\n", encoded, len(decoded.Properties()))
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

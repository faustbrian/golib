package main

import (
	"encoding/binary"
	"fmt"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geojson"
	"github.com/faustbrian/golib/pkg/geo/wkb"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func main() {
	point := mustPoint(24.9384, 60.1699)

	jsonValue, err := geojson.Marshal(point)
	if err != nil {
		panic(err)
	}
	textValue, err := wkt.MarshalEWKT(point)
	if err != nil {
		panic(err)
	}
	binaryValue, err := wkb.MarshalEWKB(point, binary.LittleEndian)
	if err != nil {
		panic(err)
	}

	decoded, err := geojson.Unmarshal(jsonValue, geo.WGS84(), requestLimits())
	if err != nil {
		panic(err)
	}
	fmt.Printf("GeoJSON: %s\nEWKT: %s\nEWKB bytes: %d\nround trip: %t\n",
		jsonValue, textValue, len(binaryValue), geo.EqualGeometry(decoded, point))
}

func requestLimits() geo.Limits {
	return geo.Limits{
		MaxPoints:          10_000,
		MaxRings:           100,
		MaxGeometries:      1_000,
		MaxCollectionDepth: 8,
		MaxEncodedBytes:    1 << 20,
	}
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

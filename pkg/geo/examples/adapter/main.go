package main

import (
	"fmt"

	"github.com/twpayne/go-geom"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/adapter/gogeom"
)

func main() {
	external := geom.NewPointFlat(geom.XY, []float64{24.9384, 60.1699}).SetSRID(4326)
	owned, err := gogeom.FromGoGeom(external, geo.DefaultLimits())
	if err != nil {
		panic(err)
	}
	roundTrip, err := gogeom.ToGoGeom(owned)
	if err != nil {
		panic(err)
	}
	fmt.Printf("type: %s, SRID: %d, layout: %s\n",
		owned.Type(), roundTrip.SRID(), roundTrip.Layout())
}

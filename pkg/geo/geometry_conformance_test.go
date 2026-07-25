package geo_test

import (
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geotest"
)

func TestPolygonLocationMatchesOGCSimpleFeaturesVectors(t *testing.T) {
	t.Parallel()

	for _, vector := range geotest.PolygonLocationVectors() {
		vector := vector
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()

			exterior := conformanceRing(vector.Exterior)
			holes := make([][]geo.Coordinate, len(vector.Holes))
			for index, hole := range vector.Holes {
				holes[index] = conformanceRing(hole)
			}
			polygon, err := geo.NewPolygon(exterior, holes)
			if err != nil {
				t.Fatalf("NewPolygon() error = %v", err)
			}
			for _, probe := range vector.Probes {
				got, locateErr := polygon.Locate(
					propertyCoordinateDegrees(probe.Longitude, probe.Latitude),
				)
				if locateErr != nil {
					t.Fatalf("Locate(%s) error = %v", probe.Name, locateErr)
				}
				if got != probe.Location {
					t.Fatalf("Locate(%s) = %v, want %v", probe.Name, got, probe.Location)
				}
			}
		})
	}
}

func conformanceRing(points [][2]float64) []geo.Coordinate {
	ring := make([]geo.Coordinate, len(points))
	for index, point := range points {
		ring[index] = propertyCoordinateDegrees(point[0], point[1])
	}
	return ring
}

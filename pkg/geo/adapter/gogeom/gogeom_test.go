package gogeom_test

import (
	"errors"
	"testing"

	"github.com/twpayne/go-geom"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/adapter/gogeom"
)

func TestRoundTripPreservesGeometryAndSRID(t *testing.T) {
	t.Parallel()

	point := mustPoint(t, 24.9384, 60.1699)
	adapted, err := gogeom.ToGoGeom(point)
	if err != nil {
		t.Fatalf("ToGoGeom() error = %v", err)
	}
	if adapted.SRID() != 4326 {
		t.Fatalf("ToGoGeom() SRID = %d, want 4326", adapted.SRID())
	}
	roundTrip, err := gogeom.FromGoGeom(adapted, geo.DefaultLimits())
	if err != nil {
		t.Fatalf("FromGoGeom() error = %v", err)
	}
	if !geo.EqualGeometry(roundTrip, point) {
		t.Fatal("adapter round trip changed geometry")
	}
}

func TestFromGoGeomEnforcesDimensionsAndResourceLimits(t *testing.T) {
	t.Parallel()

	threeDimensional := geom.NewPointFlat(
		geom.XYZ,
		[]float64{24.9384, 60.1699, 10},
	).SetSRID(4326)
	if _, err := gogeom.FromGoGeom(
		threeDimensional,
		geo.DefaultLimits(),
	); !errors.Is(err, geo.ErrUnsupported) {
		t.Fatalf("XYZ error = %v, want ErrUnsupported", err)
	}

	line := geom.NewLineStringFlat(
		geom.XY,
		[]float64{0, 0, 1, 1, 2, 2},
	).SetSRID(4326)
	limits := geo.DefaultLimits()
	limits.MaxPoints = 2
	if _, err := gogeom.FromGoGeom(line, limits); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("point limit error = %v, want ErrTopology", err)
	}
}

func TestAdaptersRejectNilMissingSRIDMalformedAndOversizedValues(t *testing.T) {
	t.Parallel()

	if _, err := gogeom.ToGoGeom(nil); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("ToGoGeom(nil) error = %v, want ErrEncoding", err)
	}
	var nilPoint *geo.Point
	if _, err := gogeom.ToGoGeom(nilPoint); !errors.Is(err, geo.ErrTopology) {
		t.Fatalf("ToGoGeom(nil point) error = %v, want ErrTopology", err)
	}
	if _, err := gogeom.FromGoGeom(nil, geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("FromGoGeom(nil) error = %v, want ErrEncoding", err)
	}
	var nilGoGeomPoint *geom.Point
	if _, err := gogeom.FromGoGeom(nilGoGeomPoint, geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("FromGoGeom(typed nil) error = %v, want ErrEncoding", err)
	}
	withoutSRID := geom.NewPointFlat(geom.XY, []float64{0, 0})
	if _, err := gogeom.FromGoGeom(withoutSRID, geo.DefaultLimits()); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("FromGoGeom(no SRID) error = %v, want ErrCRS", err)
	}
	malformed := geom.NewLineStringFlat(geom.XY, []float64{0}).SetSRID(4326)
	if _, err := gogeom.FromGoGeom(malformed, geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("FromGoGeom(odd coordinates) error = %v, want ErrEncoding", err)
	}
	limits := geo.DefaultLimits()
	limits.MaxEncodedBytes = 1
	valid := geom.NewPointFlat(geom.XY, []float64{0, 0}).SetSRID(4326)
	if _, err := gogeom.FromGoGeom(valid, limits); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("FromGoGeom(byte limit) error = %v, want ErrEncoding", err)
	}
	if _, err := gogeom.FromGoGeom(fakeGeometry{}, geo.DefaultLimits()); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("FromGoGeom(unsupported type) error = %v, want ErrEncoding", err)
	}
}

type fakeGeometry struct{}

func (fakeGeometry) Layout() geom.Layout { return geom.XY }

func (fakeGeometry) Stride() int { return 2 }

func (fakeGeometry) Bounds() *geom.Bounds { return geom.NewBounds(geom.XY) }

func (fakeGeometry) FlatCoords() []float64 { return []float64{0, 0} }

func (fakeGeometry) Ends() []int { return nil }

func (fakeGeometry) Endss() [][]int { return nil }

func (fakeGeometry) SRID() int { return 4326 }

func (fakeGeometry) Empty() bool { return false }

func mustPoint(t *testing.T, longitude, latitude float64) geo.Point {
	t.Helper()

	lon, err := geo.NewLongitude(longitude)
	if err != nil {
		t.Fatal(err)
	}
	lat, err := geo.NewLatitude(latitude)
	if err != nil {
		t.Fatal(err)
	}
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	if err != nil {
		t.Fatal(err)
	}
	point, err := geo.NewPoint(coordinate)
	if err != nil {
		t.Fatal(err)
	}
	return point
}

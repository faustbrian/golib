package geodesy

import (
	"errors"
	"math"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
)

func TestInternalNormalizationAndBoundingValidation(t *testing.T) {
	t.Parallel()

	if got := normalizeBearingDegrees(-1); got != 359 {
		t.Fatalf("normalizeBearingDegrees(-1) = %v, want 359", got)
	}
	if got := normalizeLongitudeDegrees(-541); got != 179 {
		t.Fatalf("normalizeLongitudeDegrees(-541) = %v, want 179", got)
	}
	for _, test := range []struct {
		value float64
		want  float64
	}{
		{value: 0, want: 0},
		{value: -360, want: 0},
		{value: 360, want: 0},
		{value: -1, want: 359},
		{value: 361, want: 1},
	} {
		if got := normalizeBearingDegrees(test.value); got != test.want {
			t.Fatalf("normalizeBearingDegrees(%v) = %v, want %v", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		value float64
		want  float64
	}{
		{value: 0, want: 180},
		{value: 90, want: 270},
		{value: 180, want: 0},
		{value: 270, want: 90},
		{value: 359, want: 179},
	} {
		if got := oppositeBearingDegrees(test.value); got != test.want {
			t.Fatalf("oppositeBearingDegrees(%v) = %v, want %v", test.value, got, test.want)
		}
	}
	for _, test := range []struct {
		value float64
		want  float64
	}{
		{value: -180, want: -180},
		{value: 180, want: -180},
		{value: 0, want: 0},
		{value: -181, want: 179},
		{value: 181, want: -179},
		{value: 540, want: -180},
	} {
		if got := normalizeLongitudeDegrees(test.value); got != test.want {
			t.Fatalf("normalizeLongitudeDegrees(%v) = %v, want %v", test.value, got, test.want)
		}
	}

	tests := []struct {
		name              string
		west, south, east float64
		north             float64
	}{
		{name: "west", west: math.NaN()},
		{name: "south", west: 0, south: math.NaN()},
		{name: "east", west: 0, south: 0, east: math.NaN()},
		{name: "north", west: 0, south: 0, east: 0, north: math.NaN()},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := boundingBox(test.west, test.south, test.east, test.north); !errors.Is(err, geo.ErrRange) {
				t.Fatalf("boundingBox() error = %v, want ErrRange", err)
			}
		})
	}
}

func TestInternalSphericalAndAntipodalHelpers(t *testing.T) {
	t.Parallel()

	if got := sphereBearing(0, 0, 0, math.Pi/2); math.Abs(got-90) > 1e-12 {
		t.Fatalf("eastward equatorial bearing = %v, want 90", got)
	}
	lat1, lat2 := radians(10), radians(40)
	lon1, lon2 := radians(20), radians(50)
	want := sphereBearing(lat1, lon1, lat2, lon2)
	if got := sphereBearing(lat1, lon1+radians(73), lat2, lon2+radians(73)); math.Abs(got-want) > 1e-12 {
		t.Fatalf("longitude-shifted bearing = %v, want %v", got, want)
	}

	for _, test := range []struct {
		from geo.Coordinate
		to   geo.Coordinate
		want bool
	}{
		{from: coordinate(25, 10), to: coordinate(-155, -10), want: true},
		{from: coordinate(25, 10), to: coordinate(-154, -10), want: false},
		{from: coordinate(25, 10), to: coordinate(25, -10), want: false},
		{from: coordinate(25, 90), to: coordinate(25, -90), want: true},
	} {
		if got := antipodal(test.from, test.to); got != test.want {
			t.Fatalf("antipodal(%v, %v) = %t, want %t", test.from, test.to, got, test.want)
		}
	}
}

func TestRadiusEnvelopeHasExactAngularAndPolarBoundaries(t *testing.T) {
	t.Parallel()

	sphere := MeanEarthSphere()
	tenDegrees := mustDistance(sphere.radius.Meters() * radians(10))
	bounds, err := sphere.RadiusEnvelope(coordinate(0, 0), tenDegrees)
	if err != nil {
		t.Fatal(err)
	}
	for name, edge := range map[string]struct {
		got  float64
		want float64
	}{
		"west":  {got: bounds.West().Degrees(), want: -10},
		"south": {got: bounds.South().Degrees(), want: -10},
		"east":  {got: bounds.East().Degrees(), want: 10},
		"north": {got: bounds.North().Degrees(), want: 10},
	} {
		if math.Abs(edge.got-edge.want) > 1e-12 {
			t.Fatalf("%s = %.15g, want %.15g", name, edge.got, edge.want)
		}
	}

	halfWorld := mustDistance(sphere.radius.Meters() * math.Pi)
	world, err := sphere.RadiusEnvelope(coordinate(37, 0), halfWorld)
	if err != nil {
		t.Fatal(err)
	}
	if world.West().Degrees() != -180 || world.East().Degrees() != 180 ||
		world.South().Degrees() != -90 || world.North().Degrees() != 90 {
		t.Fatalf("half-world envelope = [%v,%v,%v,%v]",
			world.West().Degrees(), world.South().Degrees(),
			world.East().Degrees(), world.North().Degrees())
	}

	tenDegreeRadius := mustDistance(sphere.radius.Meters() * radians(10))
	for _, latitude := range []float64{-80, 80} {
		polar, polarErr := sphere.RadiusEnvelope(coordinate(25, latitude), tenDegreeRadius)
		if polarErr != nil {
			t.Fatal(polarErr)
		}
		if polar.West().Degrees() != -180 || polar.East().Degrees() != 180 {
			t.Fatalf("latitude %v exact-pole envelope does not span all longitudes", latitude)
		}
		if latitude < 0 && (math.Abs(polar.South().Degrees()+90) > 1e-12 || polar.North().Degrees() >= 0) {
			t.Fatalf("south-pole envelope = [%v, %v]", polar.South().Degrees(), polar.North().Degrees())
		}
		if latitude > 0 && (math.Abs(polar.North().Degrees()-90) > 1e-12 || polar.South().Degrees() <= 0) {
			t.Fatalf("north-pole envelope = [%v, %v]", polar.South().Degrees(), polar.North().Degrees())
		}
	}
}

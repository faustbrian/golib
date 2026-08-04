package geohash_test

import (
	"errors"
	"testing"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geohash"
)

func TestEncodeMatchesCanonicalGeohashVector(t *testing.T) {
	t.Parallel()

	coordinate := mustCoordinate(t, -5.6, 42.6)
	hash, err := geohash.Encode(coordinate, 5)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if hash != "ezs42" {
		t.Fatalf("Encode() = %q, want ezs42", hash)
	}
	cell, err := geohash.Decode(hash)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	contains, err := cell.Bounds().Contains(coordinate)
	if err != nil {
		t.Fatalf("Contains() error = %v", err)
	}
	if !contains {
		t.Fatalf("decoded bounds do not contain source: %#v", cell.Bounds())
	}
	if cell.Hash() != hash {
		t.Fatalf("Hash() = %q, want %q", cell.Hash(), hash)
	}
	centerInside, err := cell.Bounds().Contains(cell.Center())
	if err != nil {
		t.Fatalf("Contains(center) error = %v", err)
	}
	if !centerInside {
		t.Fatal("decoded bounds do not contain their center")
	}
}

func TestEncodeAssignsMidpointCoordinatesToUpperHalves(t *testing.T) {
	t.Parallel()

	hash, err := geohash.Encode(mustCoordinate(t, 0, 0), 1)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if hash != "s" {
		t.Fatalf("Encode(0, 0, 1) = %q, want s", hash)
	}
}

func TestNeighborsAreDistinctAndReciprocal(t *testing.T) {
	t.Parallel()

	hash, err := geohash.Encode(mustCoordinate(t, 24.9384, 60.1699), 7)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	neighbors, err := geohash.Neighbors(hash)
	if err != nil {
		t.Fatalf("Neighbors() error = %v", err)
	}
	seen := map[geohash.Hash]bool{hash: true}
	for _, neighbor := range neighbors.All() {
		if seen[neighbor] {
			t.Fatalf("duplicate neighbor %q", neighbor)
		}
		seen[neighbor] = true
	}
	northNeighbors, err := geohash.Neighbors(neighbors.North)
	if err != nil {
		t.Fatalf("Neighbors(north) error = %v", err)
	}
	if northNeighbors.South != hash {
		t.Fatalf("north then south = %q, want %q", northNeighbors.South, hash)
	}
	cell, err := geohash.Decode(hash)
	if err != nil {
		t.Fatal(err)
	}
	for name, neighbor := range map[string]struct {
		hash      geohash.Hash
		wantWest  float64
		wantSouth float64
		wantEast  float64
		wantNorth float64
	}{
		"north": {hash: neighbors.North, wantWest: cell.Bounds().West().Degrees(), wantSouth: cell.Bounds().North().Degrees()},
		"east":  {hash: neighbors.East, wantWest: cell.Bounds().East().Degrees(), wantSouth: cell.Bounds().South().Degrees()},
		"south": {hash: neighbors.South, wantWest: cell.Bounds().West().Degrees(), wantNorth: cell.Bounds().South().Degrees()},
		"west":  {hash: neighbors.West, wantSouth: cell.Bounds().South().Degrees(), wantEast: cell.Bounds().West().Degrees()},
	} {
		adjacent, decodeErr := geohash.Decode(neighbor.hash)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		bounds := adjacent.Bounds()
		switch name {
		case "north":
			if bounds.West().Degrees() != neighbor.wantWest || bounds.South().Degrees() != neighbor.wantSouth {
				t.Fatalf("north bounds are not adjacent")
			}
		case "east":
			if bounds.West().Degrees() != neighbor.wantWest || bounds.South().Degrees() != neighbor.wantSouth {
				t.Fatalf("east bounds are not adjacent")
			}
		case "south":
			if bounds.West().Degrees() != neighbor.wantWest || bounds.North().Degrees() != neighbor.wantNorth {
				t.Fatalf("south bounds are not adjacent")
			}
		case "west":
			if bounds.South().Degrees() != neighbor.wantSouth || bounds.East().Degrees() != neighbor.wantEast {
				t.Fatalf("west bounds are not adjacent")
			}
		}
	}
}

func TestCoverIsBoundedAndHandlesAntimeridian(t *testing.T) {
	t.Parallel()

	bounds, err := geo.NewBoundingBox(
		mustLongitude(t, 179.9),
		mustLatitude(t, -0.1),
		mustLongitude(t, -179.9),
		mustLatitude(t, 0.1),
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewBoundingBox() error = %v", err)
	}
	hashes, err := geohash.Cover(bounds, 4, 100)
	if err != nil {
		t.Fatalf("Cover() error = %v", err)
	}
	if len(hashes) == 0 {
		t.Fatal("Cover() returned no cells")
	}
	exactCount := len(hashes)
	if _, err := geohash.Cover(bounds, 4, exactCount); err != nil {
		t.Fatalf("Cover(exact antimeridian limit %d) error = %v", exactCount, err)
	}
	_, err = geohash.Cover(bounds, 4, exactCount-1)
	var rangeError *geo.RangeError
	if !errors.As(err, &rangeError) || rangeError.Value != float64(exactCount) {
		t.Fatalf("Cover(antimeridian one below limit) error = %#v, want required count %d", err, exactCount)
	}
	for _, longitude := range []float64{179.95, -179.95} {
		target := mustCoordinate(t, longitude, 0)
		found := false
		for _, hash := range hashes {
			cell, decodeErr := geohash.Decode(hash)
			if decodeErr != nil {
				t.Fatalf("Decode(%q) error = %v", hash, decodeErr)
			}
			contains, containsErr := cell.Bounds().Contains(target)
			if containsErr != nil {
				t.Fatalf("Contains() error = %v", containsErr)
			}
			found = found || contains
		}
		if !found {
			t.Fatalf("cover does not contain longitude %v", longitude)
		}
	}
	_, err = geohash.Cover(bounds, 8, 1)
	if !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Cover(limit) error = %v, want ErrRange", err)
	}
}

func TestGeohashRejectsInvalidPrecisionHashAndCRS(t *testing.T) {
	t.Parallel()

	_, err := geohash.Encode(mustCoordinate(t, 0, 0), 0)
	if !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Encode(precision) error = %v, want ErrRange", err)
	}
	if _, err = geohash.Decode("EZS42"); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Decode() error = %v, want ErrEncoding", err)
	}
	for _, hash := range []geohash.Hash{"", "0123456789bcd", "a"} {
		if _, decodeErr := geohash.Decode(hash); !errors.Is(decodeErr, geo.ErrEncoding) {
			t.Fatalf("Decode(%q) error = %v, want ErrEncoding", hash, decodeErr)
		}
	}
	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	coordinate, err := geo.NewCoordinate(mustLongitude(t, 0), mustLatitude(t, 0), webMercator)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}
	_, err = geohash.Encode(coordinate, 5)
	if !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Encode(CRS) error = %v, want ErrCRS", err)
	}
	if _, err = geohash.Encode(mustCoordinate(t, 0, 0), 13); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Encode(precision 13) error = %v, want ErrRange", err)
	}
}

func TestNeighborsClampPolesAndWrapTheAntimeridian(t *testing.T) {
	t.Parallel()

	for _, point := range [][2]float64{{179.999, 0}, {-179.999, 0}, {0, 90}, {0, -90}} {
		hash, err := geohash.Encode(mustCoordinate(t, point[0], point[1]), 7)
		if err != nil {
			t.Fatalf("Encode(%v) error = %v", point, err)
		}
		neighbors, err := geohash.Neighbors(hash)
		if err != nil {
			t.Fatalf("Neighbors(%q) error = %v", hash, err)
		}
		for _, neighbor := range neighbors.All() {
			if _, err := geohash.Decode(neighbor); err != nil {
				t.Fatalf("Decode(neighbor %q) error = %v", neighbor, err)
			}
		}
		if point[1] == 90 && neighbors.North != hash {
			t.Fatalf("north-pole neighbor = %q, want same polar row %q", neighbors.North, hash)
		}
		if point[1] == -90 && neighbors.South != hash {
			t.Fatalf("south-pole neighbor = %q, want same polar row %q", neighbors.South, hash)
		}
	}
	if _, err := geohash.Neighbors("!"); !errors.Is(err, geo.ErrEncoding) {
		t.Fatalf("Neighbors(invalid) error = %v, want ErrEncoding", err)
	}
}

func TestCoverValidatesInputsAndIncludesPolarRows(t *testing.T) {
	t.Parallel()

	world := mustBounds(t, -180, -90, 180, 90, geo.WGS84())
	if _, err := geohash.Cover(world, 0, 100); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Cover(precision) error = %v, want ErrRange", err)
	}
	if _, err := geohash.Cover(world, 1, 0); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Cover(limit) error = %v, want ErrRange", err)
	}
	_, err := geohash.Cover(world, 1, 0)
	var limitError *geo.RangeError
	if !errors.As(err, &limitError) || limitError.ValueName != "geohash cover cell limit" || limitError.Minimum != 1 {
		t.Fatalf("Cover(zero limit) error = %#v, want cell-limit validation", err)
	}
	webMercator, err := geo.NewCRS(3857, "EPSG:3857")
	if err != nil {
		t.Fatalf("NewCRS() error = %v", err)
	}
	foreign := mustBounds(t, -1, -1, 1, 1, webMercator)
	if _, err := geohash.Cover(foreign, 1, 100); !errors.Is(err, geo.ErrCRS) {
		t.Fatalf("Cover(CRS) error = %v, want ErrCRS", err)
	}
	hashes, err := geohash.Cover(world, 1, 100)
	if err != nil {
		t.Fatalf("Cover(world) error = %v", err)
	}
	if len(hashes) != 32 {
		t.Fatalf("Cover(world) count = %d, want 32", len(hashes))
	}
}

func TestCoverAcceptsAnExactCellLimitAndReportsTheExactRequiredCount(t *testing.T) {
	t.Parallel()

	cell, err := geohash.Decode("s")
	if err != nil {
		t.Fatal(err)
	}
	center := cell.Center()
	pointBounds := mustBounds(
		t,
		center.Longitude().Degrees(),
		center.Latitude().Degrees(),
		center.Longitude().Degrees(),
		center.Latitude().Degrees(),
		geo.WGS84(),
	)
	hashes, err := geohash.Cover(pointBounds, 1, 1)
	if err != nil {
		t.Fatalf("Cover(exact one-cell limit) error = %v", err)
	}
	if len(hashes) != 1 || hashes[0] != "s" {
		t.Fatalf("Cover(exact one-cell limit) = %v, want [s]", hashes)
	}

	bounds := mustBounds(t, -45, 0, 45, 45, geo.WGS84())
	hashes, err = geohash.Cover(bounds, 1, 6)
	if err != nil {
		t.Fatalf("Cover(exact six-cell limit) error = %v", err)
	}
	if len(hashes) != 6 {
		t.Fatalf("Cover(exact six-cell limit) count = %d, want 6", len(hashes))
	}
	want := []geohash.Hash{"e", "g", "s", "t", "u", "v"}
	for index := range want {
		if hashes[index] != want[index] {
			t.Fatalf("Cover(exact six-cell limit) = %v, want %v", hashes, want)
		}
	}
	if _, err := geohash.Cover(bounds, 1, 5); !errors.Is(err, geo.ErrRange) {
		t.Fatalf("Cover(one below required limit) error = %v, want ErrRange", err)
	}
}

func mustBounds(t *testing.T, west, south, east, north float64, crs geo.CRS) geo.BoundingBox {
	t.Helper()
	bounds, err := geo.NewBoundingBox(
		mustLongitude(t, west),
		mustLatitude(t, south),
		mustLongitude(t, east),
		mustLatitude(t, north),
		crs,
	)
	if err != nil {
		t.Fatalf("NewBoundingBox() error = %v", err)
	}
	return bounds
}

func mustCoordinate(t *testing.T, longitude, latitude float64) geo.Coordinate {
	t.Helper()

	coordinate, err := geo.NewCoordinate(
		mustLongitude(t, longitude),
		mustLatitude(t, latitude),
		geo.WGS84(),
	)
	if err != nil {
		t.Fatalf("NewCoordinate() error = %v", err)
	}
	return coordinate
}

func mustLongitude(t *testing.T, degrees float64) geo.Longitude {
	t.Helper()

	value, err := geo.NewLongitude(degrees)
	if err != nil {
		t.Fatalf("NewLongitude() error = %v", err)
	}
	return value
}

func mustLatitude(t *testing.T, degrees float64) geo.Latitude {
	t.Helper()

	value, err := geo.NewLatitude(degrees)
	if err != nil {
		t.Fatalf("NewLatitude() error = %v", err)
	}
	return value
}

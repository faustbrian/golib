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

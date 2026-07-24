package geo_test

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
	"testing/quick"

	geo "github.com/faustbrian/golib/pkg/geo"
	"github.com/faustbrian/golib/pkg/geo/geodesy"
	"github.com/faustbrian/golib/pkg/geo/geohash"
	"github.com/faustbrian/golib/pkg/geo/geojson"
	"github.com/faustbrian/golib/pkg/geo/wkb"
	"github.com/faustbrian/golib/pkg/geo/wkt"
)

func TestGeodesicDistanceSymmetryProperty(t *testing.T) {
	t.Parallel()

	property := func(lonA, latA, lonB, latB uint16) bool {
		first, firstOK := propertyCoordinate(lonA, latA)
		second, secondOK := propertyCoordinate(lonB, latB)
		if !firstOK || !secondOK {
			return false
		}
		for _, model := range []geodesy.Model{
			geodesy.MeanEarthSphere(),
			geodesy.WGS84Ellipsoid(),
		} {
			forward, forwardErr := model.Inverse(first, second)
			reverse, reverseErr := model.Inverse(second, first)
			if forwardErr != nil || reverseErr != nil {
				return false
			}
			if math.Abs(forward.Distance().Meters()-reverse.Distance().Meters()) > 1e-8 {
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, propertyConfig(1_000, 1)); err != nil {
		t.Fatal(err)
	}
}

func TestBoundingBoxContainmentAndOverlapProperties(t *testing.T) {
	t.Parallel()

	property := func(westRaw, eastRaw, southRaw, northRaw uint16) bool {
		west := propertyLongitude(westRaw)
		east := propertyLongitude(eastRaw)
		south := propertyLatitude(min(southRaw, northRaw))
		north := propertyLatitude(max(southRaw, northRaw))
		bounds, err := geo.NewBoundingBox(west, south, east, north, geo.WGS84())
		if err != nil {
			return false
		}
		for _, coordinate := range []geo.Coordinate{
			propertyCoordinateValues(west, south),
			propertyCoordinateValues(west, north),
			propertyCoordinateValues(east, south),
			propertyCoordinateValues(east, north),
		} {
			inside, containsErr := bounds.Contains(coordinate)
			if containsErr != nil || !inside {
				return false
			}
		}
		reverse, err := geo.NewBoundingBox(east, south, west, north, geo.WGS84())
		if err != nil {
			return false
		}
		left, leftErr := bounds.Overlaps(reverse)
		right, rightErr := reverse.Overlaps(bounds)
		return leftErr == nil && rightErr == nil && left == right
	}
	if err := quick.Check(property, propertyConfig(1_000, 2)); err != nil {
		t.Fatal(err)
	}
}

func TestPointCodecRoundTripProperty(t *testing.T) {
	t.Parallel()

	property := func(longitude, latitude uint16) bool {
		coordinate, ok := propertyCoordinate(longitude, latitude)
		if !ok {
			return false
		}
		point, err := geo.NewPoint(coordinate)
		if err != nil {
			return false
		}

		jsonData, err := geojson.Marshal(point)
		if err != nil {
			return false
		}
		jsonGeometry, err := geojson.Unmarshal(jsonData, geo.WGS84(), geo.DefaultLimits())
		if err != nil || !geo.EqualGeometry(jsonGeometry, point) {
			return false
		}

		textData, err := wkt.MarshalEWKT(point)
		if err != nil {
			return false
		}
		textGeometry, err := wkt.UnmarshalEWKT(textData, geo.DefaultLimits())
		if err != nil || !geo.EqualGeometry(textGeometry, point) {
			return false
		}

		binaryData, err := wkb.MarshalEWKB(point, binary.LittleEndian)
		if err != nil {
			return false
		}
		binaryGeometry, err := wkb.UnmarshalEWKB(binaryData, geo.DefaultLimits())
		return err == nil && geo.EqualGeometry(binaryGeometry, point)
	}
	if err := quick.Check(property, propertyConfig(500, 3)); err != nil {
		t.Fatal(err)
	}
}

func TestGeohashCellContainsEncodedCoordinateProperty(t *testing.T) {
	t.Parallel()

	property := func(longitude, latitude uint16, precisionRaw uint8) bool {
		coordinate, ok := propertyCoordinate(longitude, latitude)
		if !ok {
			return false
		}
		precision := int(precisionRaw%12) + 1
		hash, err := geohash.Encode(coordinate, precision)
		if err != nil {
			return false
		}
		cell, err := geohash.Decode(hash)
		if err != nil || cell.Hash() != hash {
			return false
		}
		inside, err := cell.Bounds().Contains(coordinate)
		return err == nil && inside
	}
	if err := quick.Check(property, propertyConfig(1_000, 4)); err != nil {
		t.Fatal(err)
	}
}

func TestAxisAlignedPolygonContainmentProperty(t *testing.T) {
	t.Parallel()

	property := func(westRaw, southRaw, widthRaw, heightRaw uint8) bool {
		west := -170 + float64(westRaw)*300/255
		south := -80 + float64(southRaw)*140/255
		width := 0.1 + float64(widthRaw)*min(20, 170-west)/255
		height := 0.1 + float64(heightRaw)*min(10, 90-south)/255
		east := west + width
		north := south + height
		ring := []geo.Coordinate{
			propertyCoordinateDegrees(west, south),
			propertyCoordinateDegrees(east, south),
			propertyCoordinateDegrees(east, north),
			propertyCoordinateDegrees(west, north),
			propertyCoordinateDegrees(west, south),
		}
		polygon, err := geo.NewPolygon(ring, nil)
		if err != nil {
			return false
		}
		inside, err := polygon.Locate(propertyCoordinateDegrees(
			(west+east)/2,
			(south+north)/2,
		))
		if err != nil || inside != geo.Inside {
			return false
		}
		boundary, err := polygon.Locate(propertyCoordinateDegrees(west, south))
		return err == nil && boundary == geo.Boundary
	}
	if err := quick.Check(property, propertyConfig(500, 5)); err != nil {
		t.Fatal(err)
	}
}

func propertyConfig(count int, seed int64) *quick.Config {
	return &quick.Config{
		MaxCount: count,
		Rand:     rand.New(rand.NewSource(seed)),
	}
}

func propertyCoordinate(longitude, latitude uint16) (geo.Coordinate, bool) {
	lon := propertyLongitude(longitude)
	lat := propertyLatitude(latitude)
	coordinate, err := geo.NewCoordinate(lon, lat, geo.WGS84())
	return coordinate, err == nil
}

func propertyLongitude(value uint16) geo.Longitude {
	longitude, _ := geo.NewLongitude(float64(value)*360/65535 - 180)
	return longitude
}

func propertyLatitude(value uint16) geo.Latitude {
	latitude, _ := geo.NewLatitude(float64(value)*180/65535 - 90)
	return latitude
}

func propertyCoordinateValues(longitude geo.Longitude, latitude geo.Latitude) geo.Coordinate {
	coordinate, _ := geo.NewCoordinate(longitude, latitude, geo.WGS84())
	return coordinate
}

func propertyCoordinateDegrees(longitude, latitude float64) geo.Coordinate {
	lon, _ := geo.NewLongitude(longitude)
	lat, _ := geo.NewLatitude(latitude)
	return propertyCoordinateValues(lon, lat)
}

// Package geohash provides bounded geohash indexing helpers for EPSG:4326.
package geohash

import (
	"math"
	"sort"
	"strings"

	geo "github.com/faustbrian/golib/pkg/geo"
)

const (
	alphabet     = "0123456789bcdefghjkmnpqrstuvwxyz"
	maxPrecision = 12
)

// Hash is a canonical lowercase geohash.
type Hash string

// Cell is the exact angular cell represented by a geohash.
type Cell struct {
	hash   Hash
	bounds geo.BoundingBox
	center geo.Coordinate
}

// Hash returns the canonical lowercase geohash.
func (cell Cell) Hash() Hash { return cell.hash }

// Bounds returns the exact inclusive angular cell bounds.
func (cell Cell) Bounds() geo.BoundingBox { return cell.bounds }

// Center returns the midpoint of the angular cell.
func (cell Cell) Center() geo.Coordinate { return cell.center }

// NeighborSet contains the eight cells adjacent to a geohash.
type NeighborSet struct {
	North     Hash
	NorthEast Hash
	East      Hash
	SouthEast Hash
	South     Hash
	SouthWest Hash
	West      Hash
	NorthWest Hash
}

// All returns neighbors clockwise from north.
func (neighbors NeighborSet) All() []Hash {
	return []Hash{
		neighbors.North,
		neighbors.NorthEast,
		neighbors.East,
		neighbors.SouthEast,
		neighbors.South,
		neighbors.SouthWest,
		neighbors.West,
		neighbors.NorthWest,
	}
}

// Encode returns a standard geohash at precision 1 through 12. It is O(p),
// allocates one p-byte string, and requires EPSG:4326.
func Encode(coordinate geo.Coordinate, precision int) (Hash, error) {
	if err := validatePrecision(precision); err != nil {
		return "", err
	}
	if !coordinate.CRS().Equal(geo.WGS84()) {
		return "", &geo.CRSError{
			SRID:    coordinate.CRS().SRID(),
			Problem: "geohash requires EPSG:4326",
		}
	}

	longitudeMinimum, longitudeMaximum := -180.0, 180.0
	latitudeMinimum, latitudeMaximum := -90.0, 90.0
	result := make([]byte, precision)
	bit, value := 0, byte(0)
	longitudeBit := true
	for index := 0; index < precision; {
		value <<= 1
		if longitudeBit {
			middle := (longitudeMinimum + longitudeMaximum) / 2
			if coordinate.Longitude().Degrees() >= middle {
				value |= 1
				longitudeMinimum = middle
			} else {
				longitudeMaximum = middle
			}
		} else {
			middle := (latitudeMinimum + latitudeMaximum) / 2
			if coordinate.Latitude().Degrees() >= middle {
				value |= 1
				latitudeMinimum = middle
			} else {
				latitudeMaximum = middle
			}
		}
		longitudeBit = !longitudeBit
		bit++
		if bit == 5 {
			result[index] = alphabet[value]
			index++
			bit, value = 0, 0
		}
	}
	return Hash(result), nil
}

// Decode validates hash and returns its exact angular bounds and midpoint.
// It is O(p) and allocates no geometry-sized collections.
func Decode(hash Hash) (Cell, error) {
	if len(hash) < 1 || len(hash) > maxPrecision {
		return Cell{}, encodingError("hash length must be between 1 and 12")
	}
	longitudeMinimum, longitudeMaximum := -180.0, 180.0
	latitudeMinimum, latitudeMaximum := -90.0, 90.0
	longitudeBit := true
	for _, encoded := range []byte(hash) {
		value := strings.IndexByte(alphabet, encoded)
		if value < 0 {
			return Cell{}, encodingError("hash contains a non-canonical character")
		}
		for mask := 16; mask != 0; mask >>= 1 {
			if longitudeBit {
				middle := (longitudeMinimum + longitudeMaximum) / 2
				if value&mask != 0 {
					longitudeMinimum = middle
				} else {
					longitudeMaximum = middle
				}
			} else {
				middle := (latitudeMinimum + latitudeMaximum) / 2
				if value&mask != 0 {
					latitudeMinimum = middle
				} else {
					latitudeMaximum = middle
				}
			}
			longitudeBit = !longitudeBit
		}
	}
	bounds := makeBounds(
		longitudeMinimum,
		latitudeMinimum,
		longitudeMaximum,
		latitudeMaximum,
	)
	center := coordinate(
		(longitudeMinimum+longitudeMaximum)/2,
		(latitudeMinimum+latitudeMaximum)/2,
	)
	return Cell{hash: hash, bounds: bounds, center: center}, nil
}

// Neighbors returns all eight adjacent cells. Longitude wraps at the
// antimeridian; latitude is clamped to the polar row.
func Neighbors(hash Hash) (NeighborSet, error) {
	cell, err := Decode(hash)
	if err != nil {
		return NeighborSet{}, err
	}
	width := cell.bounds.East().Degrees() - cell.bounds.West().Degrees()
	height := cell.bounds.North().Degrees() - cell.bounds.South().Degrees()
	centerLongitude := cell.center.Longitude().Degrees()
	centerLatitude := cell.center.Latitude().Degrees()
	at := func(longitudeOffset, latitudeOffset float64) Hash {
		longitude := wrapLongitude(centerLongitude + longitudeOffset*width)
		latitude := math.Max(
			-90+height/2,
			math.Min(90-height/2, centerLatitude+latitudeOffset*height),
		)
		target := coordinate(longitude, latitude)
		encoded, _ := Encode(target, len(hash))
		return encoded
	}

	return NeighborSet{
		North:     at(0, 1),
		NorthEast: at(1, 1),
		East:      at(1, 0),
		SouthEast: at(1, -1),
		South:     at(0, -1),
		SouthWest: at(-1, -1),
		West:      at(-1, 0),
		NorthWest: at(-1, 1),
	}, nil
}

// Cover returns every angular geohash cell needed to cover bounds. Results are
// sorted, unique, and may include area outside the bounds at cell edges. Work
// and allocation are O(number of returned cells); maxCells is checked before
// allocation. This helper does not replace a database spatial index.
func Cover(
	bounds geo.BoundingBox,
	precision int,
	maxCells int,
) ([]Hash, error) {
	if err := validatePrecision(precision); err != nil {
		return nil, err
	}
	if maxCells <= 0 {
		return nil, &geo.RangeError{
			ValueName: "geohash cover cell limit",
			Value:     float64(maxCells),
			Minimum:   1,
			Maximum:   math.MaxInt,
		}
	}
	if !bounds.CRS().Equal(geo.WGS84()) {
		return nil, &geo.CRSError{
			SRID:    bounds.CRS().SRID(),
			Problem: "geohash requires EPSG:4326",
		}
	}

	columns, rows, width, height := dimensions(precision)
	south := latitudeIndex(bounds.South().Degrees(), height, rows)
	north := latitudeIndex(bounds.North().Degrees(), height, rows)
	segments := [][2]float64{{bounds.West().Degrees(), bounds.East().Degrees()}}
	if bounds.CrossesAntimeridian() {
		segments = [][2]float64{
			{bounds.West().Degrees(), 180},
			{-180, bounds.East().Degrees()},
		}
	}

	type indexRange struct{ west, east int64 }
	ranges := make([]indexRange, len(segments))
	total := int64(0)
	for index, segment := range segments {
		west := longitudeIndex(segment[0], width, columns)
		east := longitudeIndex(segment[1], width, columns)
		ranges[index] = indexRange{west: west, east: east}
		addition := (north - south + 1) * (east - west + 1)
		if addition > int64(maxCells)-total {
			return nil, &geo.RangeError{
				ValueName: "geohash cover cells",
				Value:     float64(total + addition),
				Minimum:   0,
				Maximum:   float64(maxCells),
			}
		}
		total += addition
	}

	hashes := make([]Hash, 0, int(total))
	for latitude := south; latitude <= north; latitude++ {
		for _, cellRange := range ranges {
			for longitude := cellRange.west; longitude <= cellRange.east; longitude++ {
				center := coordinate(
					-180+(float64(longitude)+0.5)*width,
					-90+(float64(latitude)+0.5)*height,
				)
				hash, _ := Encode(center, precision)
				hashes = append(hashes, hash)
			}
		}
	}
	sort.Slice(hashes, func(left, right int) bool { return hashes[left] < hashes[right] })
	return hashes, nil
}

func dimensions(precision int) (columns, rows int64, width, height float64) {
	bits := precision * 5
	longitudeBits := (bits + 1) / 2
	latitudeBits := bits / 2
	columns = int64(1) << longitudeBits
	rows = int64(1) << latitudeBits
	return columns, rows, 360 / float64(columns), 180 / float64(rows)
}

func longitudeIndex(longitude, width float64, columns int64) int64 {
	if longitude == 180 {
		return columns - 1
	}
	index := int64(math.Floor((longitude + 180) / width))
	return max(0, min(columns-1, index))
}

func latitudeIndex(latitude, height float64, rows int64) int64 {
	if latitude == 90 {
		return rows - 1
	}
	index := int64(math.Floor((latitude + 90) / height))
	return max(0, min(rows-1, index))
}

func makeBounds(west, south, east, north float64) geo.BoundingBox {
	westValue, _ := geo.NewLongitude(west)
	southValue, _ := geo.NewLatitude(south)
	eastValue, _ := geo.NewLongitude(east)
	northValue, _ := geo.NewLatitude(north)
	bounds, _ := geo.NewBoundingBox(westValue, southValue, eastValue, northValue, geo.WGS84())
	return bounds
}

func coordinate(longitude, latitude float64) geo.Coordinate {
	lon, _ := geo.NewLongitude(longitude)
	lat, _ := geo.NewLatitude(latitude)
	result, _ := geo.NewCoordinate(lon, lat, geo.WGS84())
	return result
}

func wrapLongitude(longitude float64) float64 {
	for longitude > 180 {
		longitude -= 360
	}
	for longitude < -180 {
		longitude += 360
	}
	return longitude
}

func validatePrecision(precision int) error {
	if precision < 1 || precision > maxPrecision {
		return &geo.RangeError{
			ValueName: "geohash precision",
			Value:     float64(precision),
			Minimum:   1,
			Maximum:   maxPrecision,
		}
	}
	return nil
}

func encodingError(problem string) *geo.EncodingError {
	return &geo.EncodingError{Format: "geohash", Problem: problem}
}

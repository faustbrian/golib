// Package geo provides validated, immutable geospatial values and geometry.
//
// Coordinates are always constructed and serialized in longitude, latitude
// order. Every coordinate and geometry carries an explicit CRS; algorithms do
// not transform between CRSs. Constructors reject non-finite values and apply
// configurable resource limits to aggregate geometry.
package geo

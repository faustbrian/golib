# geo

Reliable, bounded geospatial primitives for Go services.

`geo` makes coordinate order, CRS, numerical model, and resource limits
explicit. It provides immutable values and geometry, spherical and WGS84
geodesy, bounded GeoJSON/WKT/WKB codecs, geohashes, geom conversion, and
optional pgx/PostGIS integration. It does not geocode, route, validate
addresses, transform CRSs, or replace PostGIS.

## Install

```sh
go get github.com/faustbrian/golib/pkg/geo
```

The module requires Go 1.25.12 or newer.

## Coordinate-order quickstart

All constructor and encoded coordinate pairs are **longitude, latitude** (`x,
y`). They are never silently interchanged.

```go
lon, err := geo.NewLongitude(24.9384)
if err != nil { /* handle */ }
lat, err := geo.NewLatitude(60.1699)
if err != nil { /* handle */ }
helsinki, err := geo.NewCoordinate(lon, lat, geo.WGS84())
if err != nil { /* handle */ }
```

Use the WGS84 ellipsoid for accurate earth distances and the named mean-earth
sphere when an explicitly approximate great-circle result is appropriate:

```go
result, err := geodesy.WGS84Ellipsoid().Inverse(helsinki, destination)
if err != nil { /* handle */ }
metres := result.Distance().Meters()
```

Every operation rejects incompatible CRSs; no package performs a silent
transformation.

## Packages

- `geo`: values, limits, typed errors, bounds, geometry, containment, equality.
- `geodesy`: named sphere/ellipsoid models, bearings, destinations, envelopes,
  measurements, and bounded in-memory nearest ranking.
- `geojson`, `wkt`, `wkb`: bounded canonical codecs.
- `geohash`: encode, decode, neighbors, and bounded covers.
- `postgis`: nullable scanner/valuer, pgx codec, and reviewed SQL fragments.
- `adapter/gogeom`: isolated conversion to and from `geom`.
- `geotest`: authoritative vectors and tolerance-aware assertions.

The complete exported API is published by
[pkg.go.dev](https://pkg.go.dev/github.com/faustbrian/golib/pkg/geo). Runnable programs
live in [`examples`](examples), and detailed guidance lives in [`docs`](docs),
including the [direct dependency inventory](docs/dependencies.md).
The [hardening evidence](docs/hardening.md) records numerical tolerances,
interoperability and fuzz corpora, allocation baselines, and residual risks.

## Behavioral contract

- Longitude is `[-180, 180]`; latitude is `[-90, 90]`; bearings are `[0, 360)`.
- NaN and infinity are rejected. Negative zero is canonicalized to positive
  zero. Decimal JSON/text uses Go's shortest round-trippable representation.
- Bounds are inclusive. `west > east` explicitly crosses the antimeridian.
- Polygon boundaries are `Boundary`; holes are excluded from `Interior`.
- Empty Point, LineString, and Polygon values have no v1 representation. Empty
  MultiPoint, MultiLineString, MultiPolygon, and GeometryCollection values are
  supported with an explicit CRS. Degenerate, open, self-intersecting, and
  overlapping rings are rejected.
- Default limits bound points, rings, geometries, nesting, and encoded bytes.
- Errors support `errors.Is` with `geo.ErrRange`, `geo.ErrTopology`,
  `geo.ErrCRS`, `geo.ErrEncoding`, and `geo.ErrUnsupported`.
- Coincident and exact antipodal inverse results report undefined bearings;
  near-antipodal bearings may remain numerically sensitive.

## Choosing process code or PostGIS

Use this module in process for validation, serialization, deterministic
point/geometry operations, small candidate sets, and request-local geodesy.
Use PostGIS for indexed spatial search, joins, large datasets, persistence-side
filtering, or unsupported GIS operations. `geodesy.Nearest` and
`geohash.Cover` explicitly do not replace a spatial index.

## Verification

```sh
go test ./...
go test -race ./...
./scripts/check-coverage.sh
./scripts/fuzz-smoke.sh
```

The CI matrix also verifies formatting, vetting, linting, vulnerabilities,
authoritative numerical vectors, benchmarks, API compatibility, examples, and
live PostgreSQL/PostGIS releases.

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and
[CHANGELOG.md](CHANGELOG.md).

## License

`geo` is available under the [MIT License](LICENSE).

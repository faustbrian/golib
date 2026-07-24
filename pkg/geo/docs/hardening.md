# Numerical and interoperability hardening

This document records the executable evidence and residual limits for the
2026-07-16 hardening audit. It is a verification snapshot, not a promise that
latency is identical on other machines or that dependency advisories cannot
change after the audit date.

## Numerical conformance and tolerance matrix

| Surface | Evidence | Allowed error | Reason |
| --- | --- | --- | --- |
| Scalar ranges, finite values, signed zero | `values_test.go` | Exact | Validation and zero canonicalization are discrete contracts. |
| IUGG mean-radius quarter equator | `TestMeanEarthSphereSolvesQuarterEquator` | 1 micrometre distance, 1e-12 degrees bearing | Closed-form spherical reference using the named 6,371,008.8 m radius. |
| WGS84 inverse and direct | `geotest.WGS84InverseVectors` | 1 micrometre distance, 1e-12 initial and 5e-12 final bearing degrees | Published GeographicLib examples and upstream reference regression vectors. |
| Tiny WGS84 line | GeographicLib `GeodSolve4` | 0.5 millimetres | The published reference value is rounded to 0.001 m. |
| Polar and near-antipodal WGS84 paths | GeographicLib `GeodSolve6` and `GeodSolve9` | 0.5 millimetres | The published reference distances are rounded to 0.001 m. |
| Exact equatorial and pole-to-pole antipodes | GeographicLib WGS84 meridian | 1 micrometre distance; bearings undefined | Multiple shortest-path azimuths exist, so `BearingsDefined` is false. |
| Spherical distance versus PostGIS | `TestPostGISIntegration` | 5 centimetres | PostGIS rounds its mean Earth radius differently from the explicit IUGG radius. |
| WGS84 spheroidal distance versus PostGIS | `TestPostGISIntegration` | 1 millimetre | Independent PostGIS/PROJ spheroid calculation. |
| Polygon interior, exterior, shell, and hole boundaries | OGC vectors and PostGIS predicates | Exact three-state result | The API returns `Outside`, `Inside`, or `Boundary`; it is not a distance approximation. |

GeographicLib documents sub-15-nanometre distance error for terrestrial
ellipsoids. This package does not advertise that upstream bound as its own test
tolerance: the checked reference values have fewer decimal places, so their
explicit 1 micrometre or 0.5 millimetre budgets above are the enforceable
contract.

The mean-radius sphere is intentionally approximate relative to the ellipsoid.
There is no package-wide spherical-versus-ellipsoidal error bound because the
error depends on latitude, direction, and distance. Callers requiring an earth
accuracy claim must select `WGS84Ellipsoid`.

`BearingsDefined` detects coincident points and exact mathematical antipodes.
Near-antipodal azimuths may still be ill-conditioned, and symmetric ellipsoidal
cases can have more than one shortest geodesic. The method is not a general
uniqueness certificate.

Polygon topology and location are planar operations on longitude/latitude
values after antimeridian unwrapping. Edges are straight in that coordinate
plane, not ellipsoidal geodesics. Boundary detection uses exact `float64`
collinearity and intentionally has no hidden epsilon.

## Codec and PostGIS interoperability corpus

[`postgis/testdata/interoperability.json`](../postgis/testdata/interoperability.json)
is loaded by the live integration suite on PostGIS 16 / 3.5 and 18 / 3.6. Its
eleven cases cover:

- Point, LineString, Polygon with a hole, MultiPoint, MultiLineString,
  MultiPolygon, and GeometryCollection;
- empty MultiPoint, MultiLineString, MultiPolygon, and GeometryCollection;
- a line crossing the antimeridian and coordinate extrema;
- GeoJSON, WKT, EWKT, WKB, and EWKB decoding from PostGIS;
- exact WKB and EWKB byte comparison in NDR/little-endian and XDR/big-endian;
- SRID, 2D dimensionality, and empty-state metadata;
- preservation of SRID 3857 without transforming coordinates; and
- rejection of PostGIS `POINT Z` with `geo.ErrUnsupported`.

GeoJSON is only accepted for EPSG:4326. A projected PostGIS geometry preserves
its SRID through EWKB, but GeoJSON encoding rejects it rather than silently
labelling projected ordinates as longitude and latitude. Primitive `EMPTY`
Point, LineString, and Polygon remain unsupported because the root model cannot
represent them; empty aggregate geometries are supported.

## Fuzz corpora and hostile-input boundaries

Every fuzz target has inline seeds plus a checked-in corpus under its package's
`testdata/fuzz` directory:

| Target | Corpus emphasis |
| --- | --- |
| `FuzzGeometryConstructors` | Invalid rings, self-crossing coordinate orders, holes, reversed winding, oversized point arrays, and excessive collection depth. |
| `FuzzValueDecoding` | Scalar, CRS, and coordinate JSON/text with non-finite values, signed zero, malformed separators, and numeric extremes. |
| `geodesy.FuzzModels` | Poles, antipodes, inverse/direct composition, and finite output. |
| `adapter/gogeom.FuzzFromGoGeom` | Malformed flat coordinate storage, NaN, infinity, and point limits. |
| `geojson.FuzzDecode` | Malformed JSON, deep collections, topology, coordinate arity, and encoded-size limits. |
| `wkt.FuzzDecode` | Malformed tokens and numbers, dimensional markers, deep collections, counts, and trailing input. |
| `wkb.FuzzDecode` | Byte order, flags, malformed lengths, integer overflow, truncation, nesting, and SRID. |
| `geohash.FuzzDecode` | Invalid alphabet, length, poles, and neighbor wrapping. |
| `postgis.FuzzValueScan` | Binary and hexadecimal EWKB, prefixes, malformed lengths, and scan ownership. |

Fuzz callbacks cap their own work and pass strict `geo.Limits`. The codecs
check encoded byte limits before recursive parsing, collection depth before
descending, and aggregate counts before allocating child storage. Constructor
tests cover integer-overflow-safe count addition. Fuzzing supplements, rather
than replaces, deterministic hostile length, truncation, overflow, and depth
tests.

## Complexity, allocation, and benchmark baseline

The reproducible command is:

```sh
go test . ./geodesy ./wkb ./adapter/gogeom ./postgis \
  -run '^$' -bench . -benchmem -benchtime=100x -count=3
```

The following medians were measured on an Apple M4 Max (`darwin/arm64`) with
Go 1.25.12 on 2026-07-16. Time values are observational baselines; allocation
budgets are executable regression tests with deliberately higher ceilings.

| Operation | Size | Median time | Bytes/op | Allocs/op | Enforced allocation ceiling |
| --- | ---: | ---: | ---: | ---: | ---: |
| Polygon validation | 1,000 points | 136.7 us | 185,989 | 349 | 400 |
| Spherical inverse | one pair | 156 ns | 0 | 0 | 0 observed |
| WGS84 inverse | one pair | 727 ns | 0 | 0 | 0 observed |
| EWKB marshal | 100,000 points | 765 us | 1,605,680 | 2 | 3 |
| EWKB unmarshal | 100,000 points | 1.40 ms | 8,011,828 | 3 | observational |
| geom to package model | 1,000 points | 22.0 us | 114,864 | 11 | 16 |
| package model to geom | 1,000 points | 13.6 us | 49,312 | 10 | 16 |
| pgx binary encode | one point | 175 ns | 176 | 4 | 8 |
| pgx binary scan | one point | 195 ns | 160 | 3 | 8 |

Limits make work finite, but they are not latency promises. In particular,
polygon validity is delegated to a robust planar topology engine whose
worst-case runtime is not exposed as a stable complexity contract. The default
one-million-point ceiling is a general safety maximum, not an appropriate
request budget; callers must choose smaller point/ring limits from measured
latency and memory budgets.

## Dependency and numerical-risk audit

The detailed module-by-module audit is in
[`dependencies.md`](dependencies.md). On 2026-07-16:

- every selected direct version equalled `go list -m <module>@latest`;
- `govulncheck v1.6.0` reported no reachable vulnerability, using the Go
  vulnerability database snapshot last modified 2026-07-08;
- the compiled dependency closure contained no cgo files;
- `simplefeatures/geom` imports `unsafe` for native-endian detection and
  zero-copy float/byte reinterpretation, although this package calls only its
  planar polygon constructor and validator; and
- the optional simplefeatures GEOS/PROJ cgo packages are present upstream but
  are not imported or linked here.

Advisory status is time-sensitive. Release verification must rerun
`govulncheck`; this snapshot must not be treated as a permanent absence of
future advisories.

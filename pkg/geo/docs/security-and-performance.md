# Security, limits, and performance

## Untrusted input

Never decode untrusted bytes with application-inappropriate limits. A zero
field in `geo.Limits` selects the conservative default:

- 1,000,000 points
- 10,000 rings
- 100,000 geometries
- collection depth 32
- 64 MiB encoded input

Reduce these values for request paths. Limits cover aggregate nested geometry,
not merely each child. Decoders reject truncated values, invalid lengths,
integer overflow, unsupported dimensions/types, malformed numbers, excessive
nesting, and trailing data. Constructors validate topology after applying
resource bounds.

These limits make work finite; they do not promise acceptable request latency.
Polygon validity uses an upstream robust planar topology implementation whose
worst-case CPU complexity is not a stable package contract. Choose substantially
smaller request-specific point and ring limits from measured budgets.

SQL identifiers are validated and quoted; geometry and distance values remain
bound parameters. Do not concatenate `Fragment.Args()` into SQL.

## Complexity and allocation

Public algorithm comments state their complexity. Scalar validation, bounds,
spherical/ellipsoidal point geodesy, and radius envelopes are O(1). Line and
polygon measurements are O(points). `Nearest` is O(n log n) and allocates O(n).
Geohash encode/decode is O(precision); covers allocate O(returned cells) after
checking `maxCells`.

Codec work is O(encoded bytes plus geometry size). The WKB encoder pre-sizes its
buffer and has an allocation regression test: encoding a 100,000-point line is
bounded to three allocations. Benchmarks cover 10, 1,000, and 100,000-point
WKB, 1,000-point polygon validation, geom conversion, spherical versus
ellipsoidal inverse calculations, and pgx binary codecs.

Executable allocation ceilings cover 1,000-point polygon validation,
1,000-point geom conversion, 100,000-point WKB encoding, and pgx binary
encode/scan. The dated measurement matrix is in
[`hardening.md`](hardening.md#complexity-allocation-and-benchmark-baseline).

## Dependency isolation

- GeographicLib supplies audited WGS84 geodesics behind `geodesy.Model`.
- simplefeatures validates polygon topology behind immutable package geometry.
- geom is isolated to an adapter and differential tests.
- pgx is isolated to `postgis`.

These dependencies use redistributable licenses recorded by their modules.
Release review must inspect `go mod graph`, license metadata, `govulncheck`, and
benchmark/fuzz changes before upgrading them.

The compiled closure has no cgo packages. `simplefeatures/geom` does import
`unsafe` for native-endian detection and zero-copy float/byte reinterpretation;
its optional GEOS/PROJ cgo packages are not linked. See
[`dependencies.md`](dependencies.md) for the dated advisory, version, license,
unsafe, cgo, and numerical-risk audit.

## Verification strategy

Statement coverage is exactly 100%, but coverage alone is not the confidence
claim. The suite also contains authoritative vectors, algebraic/property
invariants, geom and PostGIS differential tests, hostile size/depth/overflow
cases, fuzz targets for every decoder, aggregate constructor, geodesic model,
and adapter boundary, the race detector, and representative benchmarks.

Fuzz locally for longer than the CI smoke duration before modifying parsers:

```sh
go test ./wkb -run '^$' -fuzz '^FuzzDecode$' -fuzztime 1m
```

Checked-in corpora live below each package's `testdata/fuzz` directory. Their
coverage and callback bounds are catalogued in
[`hardening.md`](hardening.md#fuzz-corpora-and-hostile-input-boundaries).

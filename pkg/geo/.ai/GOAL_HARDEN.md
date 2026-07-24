# Hardening Goal: Geospatial Primitives

## Objective

Prove numerical correctness, bounded resource use, codec interoperability, and
safe behavior at geographic and floating-point edge cases.

## Required Audits

- Test poles, antimeridian crossings, antipodal and near-antipodal points,
  coincident points, tiny distances, signed zero, NaN, infinity, and extremes.
- Compare geodesic outputs with authoritative datasets and independent engines
  under documented tolerances.
- Fuzz invalid rings, holes, winding, self-intersection, degenerate geometry,
  huge coordinate arrays, deep collections, malformed lengths, and encodings.
- Prove point-in-polygon boundary and hole behavior.
- Differential-test GeoJSON, WKT/EWKT, WKB/EWKB, SRID, dimensionality, empty
  geometry, and byte order against supported PostGIS versions.
- Verify malformed or hostile input cannot cause unbounded recursion,
  allocation, CPU, integer overflow, panic, or silent truncation.
- Audit upstream algorithms for licenses, unsafe code, cgo, numerical caveats,
  advisories, and supported-version changes.
- Verify every approximate result and tolerance is documented honestly.

## Required Deliverables

- Numerical conformance and tolerance matrix.
- Codec and PostGIS interoperability corpus.
- Fuzz corpora for geometry and all encodings.
- Complexity, allocation, and large-geometry benchmark baselines.
- Dependency and numerical-risk audit.
- Updated API, mathematical, security, compatibility, FAQ, and `CHANGELOG.md`.

## Release Blockers

- Coordinate-order ambiguity, silent CRS conversion, invalid accepted values, or
  undocumented precision loss.
- Incorrect antimeridian, pole, polygon-boundary, or SRID behavior.
- Panic, unbounded work, overflow, or memory exhaustion from bounded input.
- Self-round-trip tests used as the only evidence for interoperability.
- Missing meaningful 100% coverage or a failing GitHub Actions gate.

## Completion Criteria

- Numerical, differential, PostGIS, hostile-input, and compatibility suites pass.
- Fuzz, race, vulnerability, and performance gates pass.
- All error bounds and unsupported cases are documented.
- No release blocker remains and `CHANGELOG.md` is current.

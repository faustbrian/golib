# Goal: Reliable Geospatial Primitives

## Objective

Build a production-grade open-source geospatial package for the reusable
coordinate, distance, bounding, geometry, encoding, and PostgreSQL/PostGIS needs
of location-oriented Go services.

The package MUST be mathematically explicit and interoperable. It MUST NOT absorb
postal-code syntax, address validation, carrier rules, provider search, or other
business-domain behavior.

## Core Types

- Validated longitude, latitude, coordinate, altitude, bearing, and distance.
- Explicit coordinate ordering; APIs MUST NOT silently interchange lat/lon and
  lon/lat.
- Point, bounding box, line string, polygon, multi-geometry, and geometry
  collection where justified.
- Explicit SRID and coordinate reference-system metadata.
- Immutable value behavior, stable equality semantics, and documented precision.
- Typed errors for range, topology, CRS, encoding, and unsupported operations.

## Algorithms

- Great-circle and ellipsoidal distance with named, documented models.
- Initial/final bearing and destination point.
- Bounding boxes, radius envelopes, antimeridian-safe containment, and overlap.
- Point-in-polygon with holes and boundary semantics.
- Line/polygon measurements where correctness can be established.
- Geohash encode/decode, neighbors, and covering helpers if justified by actual
  indexing requirements.
- Sorting and nearest-candidate helpers that do not pretend to replace a spatial
  database index.

Complex geodesic and topology algorithms SHOULD use audited upstream
implementations behind package-owned contracts rather than casual rewrites.
Dependencies MUST be benchmarked, fuzzed, licensed, and isolated.

## Interoperability

- GeoJSON geometry and feature support with bounded decoding.
- WKT/EWKT and WKB/EWKB encoding/decoding where required for PostGIS.
- `pgx` codecs or scanner/valuer integration in an optional package.
- PostGIS query helpers limited to safe values and reviewed SQL fragments; no
  generic query builder.
- JSON and text marshaling with stable coordinate order and precision policy.
- Conversion adapters for selected mature Go geometry types without making them
  the mandatory public model.

## Numerical And Resource Policy

- Define behavior for NaN, infinity, signed zero, poles, antimeridian, degenerate
  rings, invalid winding, empty geometry, and precision loss.
- No silent CRS transformation.
- Bound points, rings, nesting, allocation, recursion, and encoded input sizes.
- Algorithms MUST document complexity and whether they allocate.
- Approximate operations MUST be named and state their error characteristics.

## Non-Goals

- No geocoding, reverse geocoding, maps vendor, routing, or places API client.
- No postal-code, address, country, timezone, or carrier-location business rules.
- No replacement for PostGIS or a full GIS engine.
- No unsupported CRS transformation framework in v1.
- No map rendering or frontend components.

## Package Shape

- Root package: core values, geometry, bounds, errors.
- `geodesy`: distance, bearing, destination, and model selection.
- `geojson`, `wkt`, and `wkb`: bounded interoperable codecs.
- `geohash`: optional spatial indexing helpers.
- `postgis`: optional pgx/PostGIS codecs and helpers.
- `geotest`: conformance vectors, generators, tolerances, and assertions.

## Testing And Quality Standard

Meaningful 100% production statement coverage is mandatory. Numerical branches
MUST be validated against authoritative vectors and independent implementations,
not only self-generated round trips.

Required verification includes:

- authoritative geodesic and geometry conformance vectors
- property tests for symmetry, bounds, round trips, containment, and invariants
- fuzzing for every untrusted codec and geometry constructor
- differential tests against selected mature implementations and PostGIS
- hostile geometry, allocation, recursion, and integer-overflow tests
- race tests for any cache or shared state
- benchmarks for representative geometry sizes and database codecs

## Documentation Deliverables

- Complete API reference and coordinate-order quickstart.
- Guides for distances, bearings, bounds, polygons, antimeridian behavior,
  GeoJSON, WKT/WKB, pgx, PostGIS, geohash, precision, and performance.
- Decision guidance for in-process calculations versus PostGIS queries.
- Numerical model, tolerance, CRS, security, compatibility, migration,
  troubleshooting, FAQ, contribution, and maintained `CHANGELOG.md` docs.
- Runnable examples for every user-facing scenario.

## Automation And Release

GitHub Actions MUST run formatting, vetting, linting, tests, exact meaningful
coverage, fuzz smoke tests, race tests, PostGIS integration matrices,
vulnerability scans, numerical conformance, benchmarks, docs, examples, API
compatibility, and release automation.

## Execution Plan

1. Specify values, coordinate order, precision, models, limits, and errors.
2. Implement core coordinates, bounds, geodesy, and conformance vectors.
3. Implement geometry and bounded GeoJSON/WKT/WKB codecs.
4. Implement optional geohash and PostGIS integrations.
5. Complete differential, fuzz, performance, numerical, and security hardening.
6. Publish complete adoption and mathematical documentation.

## Acceptance Criteria

- Coordinate order, CRS, precision, and boundary behavior are never implicit.
- Numerical claims are supported by authoritative and differential evidence.
- GeoJSON, WKT/WKB, pgx, and PostGIS interoperability is proven where claimed.
- Hostile geometry remains bounded and panic-free.
- Meaningful 100% coverage and every GitHub Actions gate pass.
- Documentation is adoption-ready and `CHANGELOG.md` is current.

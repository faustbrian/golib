# Compatibility, migration, troubleshooting, and FAQ

## Compatibility policy

Before v1, minor versions may refine APIs with changelog and migration notes.
At v1, exported APIs follow semantic versioning: incompatible changes require a
new major module path. Numerical model constants, coordinate order, boundary
semantics, canonical encodings, typed error identity, and default resource
limits are compatibility-relevant behavior. The minimum Go version is declared
in `go.mod` and may increase only in a documented minor release.

Before the first release, API compatibility checks compare the current module
with the checked-in `api/baseline.txt`. Regenerate that baseline only for an
intentional, reviewed public API change. After a release, compatibility may also
be checked against an explicitly generated release baseline.

## Migration checklist

1. Identify whether existing pairs are latitude/longitude or
   longitude/latitude; convert explicitly before construction.
2. Attach the real SRID. Do not label projected coordinates as EPSG:4326.
3. Choose WGS84 ellipsoid or a named sphere; record any tolerance change.
4. Replace direct struct mutation with validated constructors and copy-returning
   accessors.
5. Set request-specific limits for every untrusted decoder.
6. Decide whether plain WKT/WKB receives an external CRS or should become
   EWKT/EWKB.
7. Register both PostGIS geometry and geography OIDs when both are queried.
8. Compare old/new production fixtures at poles, boundaries, holes, and the
   antimeridian before rollout.

## Troubleshooting

**Coordinates appear in the wrong country.** Check input order. The first value
must be longitude and the second latitude.

**A geodesy or geohash call returns `ErrCRS`.** The operation requires
EPSG:4326. Transform outside this module with an explicitly selected projection
library, then construct a new WGS84 coordinate.

**A PostGIS pgx query reports invalid geometry.** Register the OID of the actual
wire type on that connection. `GeographyDWithin` requires the geography OID;
`Intersects` requires geometry.

**A polygon is rejected although it is closed.** Closure is only one invariant.
Check repeated/collinear points, self-crossing edges, holes outside the shell,
and overlapping or nested holes.

**A cover or decode returns `ErrRange`.** Inspect `maxCells` and every
`geo.Limits` field. Increase limits only after estimating worst-case allocation
and latency.

**Two computed distances differ.** Confirm both use the same model and
parameters. Spherical and ellipsoidal distances are intentionally different.

**An antipodal result has no bearing.** Coincident and exact antipodal points
do not define a unique shortest-path azimuth, so inspect `BearingsDefined`
before reading the bearing values. Near-antipodal azimuths may also be
ill-conditioned even when a result is returned.

## FAQ

**Does the module transform CRSs?** No.

**Does it support altitude in geometry?** Altitude is a validated scalar, but v1
geometry and interoperable codecs are deliberately 2D.

**Are bounds edges and polygon edges inside?** Bounds containment is inclusive.
Polygon edges return `Boundary`, allowing the caller to choose policy.

**Does geohash prove proximity?** No. It is an indexing hint with cell-shaped
false positives. Confirm with geodesy or PostGIS.

**Should I use `Nearest` for a database table?** No. It ranks a bounded in-memory
slice. Use a PostGIS spatial index for database search.

**Are returned slices mutable aliases?** No. Geometry and fragment accessors
return owned copies; changing them does not change the value.

**Does `BearingsDefined` prove the shortest geodesic is unique?** No. It marks
coincident and exact mathematical antipodes undefined. Special symmetric and
near-antipodal ellipsoidal cases can still have multiple or sensitive azimuths.

**What evidence blocks a release?** The numerical/tolerance matrix, codec and
PostGIS corpus, fuzz corpora, hostile-input tests, allocation budgets,
dependency audit, race detector, vulnerability scan, and exact coverage gate
are catalogued in [`docs/hardening.md`](hardening.md).

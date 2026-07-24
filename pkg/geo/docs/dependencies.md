# Dependency inventory

Direct dependencies are deliberately isolated behind package-owned contracts.
This inventory must be reviewed whenever `go.mod` changes.

The 2026-07-16 audit resolved each selected version as the latest version from
the Go module proxy.

| Module | Selected/latest | Purpose | License | Linked unsafe/cgo | Material caveat |
| --- | --- | --- | --- | --- | --- |
| `github.com/pymaxion/geographiclib-go/v2` | v2.1.2 | WGS84 Karney geodesics behind `geodesy.Model` | MIT | none | Multiple shortest geodesics exist for special antipodal/symmetric cases; the upstream terrestrial distance claim is below 15 nm, but package tolerances are intentionally looser. |
| `github.com/peterstace/simplefeatures` | v0.59.0 | Planar polygon topology validation behind immutable geometry | MIT | `geom` imports `unsafe`; no cgo is linked | Validation is planar, not geodesic. Native-endian initialization and zero-copy WKB helpers use `unsafe` even though this package calls only `NewPolygonXY(...).Validate()`. |
| `github.com/twpayne/go-geom` | v1.6.1 | Optional adapter and independent WKB differential | BSD-2-Clause | none in linked packages | The adapter deliberately accepts only XY layouts with positive SRIDs. |
| `github.com/jackc/pgx/v5` | v5.10.0 | Optional PostGIS wire codec and live integration | MIT | none in linked packages | PostGIS OIDs are installation-specific and must be registered per connection/type map. |

The authoritative license text remains in each dependency module and its
source repository. `go.sum` pins downloaded content; CI runs module-integrity
and vulnerability checks. A dependency upgrade requires tests, fuzzing,
benchmarks, license review, and an entry in `CHANGELOG.md` when user-observable
behavior changes.

`go list -deps -json ./...` reported no linked cgo files. The upstream
simplefeatures module also contains optional GEOS and PROJ packages with cgo,
but those packages are outside this module's compiled dependency closure.
Among direct dependency packages in the closure, only
`simplefeatures/geom` imports `unsafe`.

`govulncheck v1.6.0 -json ./...` reported no reachable finding against
`https://vuln.go.dev` (database timestamp 2026-07-08T17:05:00Z). That result is
a dated reachability scan, not a guarantee that the modules have never had an
advisory or will not receive one. Rerun it for every release and dependency
upgrade. `simplefeatures` retracts v0.45.0 for a known bug; the selected v0.59.0
is not retracted.

See [the hardening matrix](hardening.md) for package-owned tolerances, linked
algorithm boundaries, corpora, and benchmark evidence.

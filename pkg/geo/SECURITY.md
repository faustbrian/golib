# Security policy

Please report vulnerabilities privately through GitHub's security advisory
feature for this repository. Do not open a public issue until a fix and release
are available. Include affected versions, a minimal reproducer, impact, and any
suggested resource limit or mitigation.

Supported versions will be listed here after the first release. Until then,
security fixes target the latest commit on `main`.

The principal risk areas are hostile codec input, aggregate geometry exhaustion,
integer overflow, parser panics, SQL-fragment misuse, numerical edge cases, and
dependency vulnerabilities. Callers must apply request-body and deadline limits;
geometry constructors and codecs additionally expose structural limits for
points, rings, child geometries, and recursion depth. These controls bound work
for accepted input but are not a replacement for request-level CPU and byte
budgets.

The normal module build contains no linked cgo. The direct dependency
`simplefeatures` uses `unsafe` for native-endian detection and zero-copy byte and
floating-point reinterpretation. Its optional GEOS and PROJ cgo adapters are not
imported. Dependency advisory results are time-sensitive and must be refreshed
with `govulncheck ./...`; the latest recorded versions, licenses, caveats, and
scan date are maintained in [the dependency audit](docs/dependencies.md).

Local release evidence includes bounded fuzz smoke tests with durable hostile
corpora, race tests, exact coverage, API compatibility, allocation budgets,
benchmarks, `govulncheck`, and live tests against every supported PostGIS line.
The numerical, interoperability, fuzz, benchmark, and dependency evidence is
indexed in [the hardening matrix](docs/hardening.md).
